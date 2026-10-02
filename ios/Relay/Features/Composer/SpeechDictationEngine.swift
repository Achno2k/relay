import AVFoundation
import Speech

/// The real engine: microphone → `SpeechAnalyzer` with a `SpeechTranscriber`, all on device. The device locale,
/// else en-US; a missing model is downloaded through `AssetInventory` first.
@MainActor
final class SpeechDictationEngine: DictationEngine {
    enum Source {
        case microphone
        /// An audio file instead of the mic, for the on-device pipeline test; ends when the file does.
        case file(URL)
    }

    private let source: Source
    private let audio = AVAudioEngine()
    private var analyzer: SpeechAnalyzer?
    private var input: AsyncStream<AnalyzerInput>.Continuation?
    private var events: AsyncThrowingStream<DictationEvent, any Error>.Continuation?
    private var run: Task<Void, Never>?
    private var tapped = false

    init(source: Source = .microphone) {
        self.source = source
    }

    func start() -> AsyncThrowingStream<DictationEvent, any Error> {
        let (stream, events) = AsyncThrowingStream.makeStream(of: DictationEvent.self)
        self.events = events
        run = Task { [weak self] in
            do {
                try await self?.listen(events)
            } catch {
                self?.stopAudio()
                events.finish(throwing: error)
            }
        }
        return stream
    }

    func finish() {
        stopAudio()
        if let analyzer {
            // The results loop in `listen` ends once the last words are final.
            Task { try? await analyzer.finalizeAndFinishThroughEndOfInput() }
        } else {
            cancel()
        }
    }

    func cancel() {
        run?.cancel()
        stopAudio()
        if let analyzer { Task { await analyzer.cancelAndFinishNow() } }
        analyzer = nil
        events?.finish()
    }

    private func listen(_ events: AsyncThrowingStream<DictationEvent, any Error>.Continuation) async throws {
        if case .microphone = source { try await Self.requestPermissions() }
        guard SpeechTranscriber.isAvailable, let locale = await Self.locale() else { throw DictationError.unavailable }
        let transcriber = SpeechTranscriber(locale: locale, preset: .progressiveTranscription)
        // A request can come back for an installed model too; only a missing one shows "Downloading".
        if await AssetInventory.status(forModules: [transcriber]) != .installed,
           let request = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
            events.yield(.downloading(nil))
            let progress = request.progress
            let watcher = Task {
                while !Task.isCancelled {
                    events.yield(.downloading(progress.fractionCompleted))
                    try? await Task.sleep(for: .milliseconds(250))
                }
            }
            defer { watcher.cancel() }
            try await request.downloadAndInstall()
        }
        try Task.checkCancellation()
        if case .file(let url) = source {
            try await transcribe(AVAudioFile(forReading: url), with: transcriber, events)
            return
        }

        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .spokenAudio, options: [.duckOthers, .defaultToSpeaker])
        try session.setActive(true, options: .notifyOthersOnDeactivation)
        let micFormat = audio.inputNode.outputFormat(forBus: 0)
        guard micFormat.sampleRate > 0, micFormat.channelCount > 0,
              let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [transcriber], considering: micFormat)
        else { throw DictationError.unavailable }

        let analyzer = SpeechAnalyzer(modules: [transcriber])
        self.analyzer = analyzer
        try await analyzer.prepareToAnalyze(in: format)
        try Task.checkCancellation()
        let (inputs, input) = AsyncStream.makeStream(of: AnalyzerInput.self)
        self.input = input
        audio.inputNode.installTap(
            onBus: 0, bufferSize: 4096, format: micFormat,
            block: try Self.tap(from: micFormat, to: format, input: input, events: events)
        )
        tapped = true
        audio.prepare()
        try audio.start()
        try await analyzer.start(inputSequence: inputs)
        events.yield(.listening)

        for try await result in transcriber.results {
            events.yield(.text(String(result.text.characters), isFinal: result.isFinal))
        }
        events.finish()
    }

    private func transcribe(
        _ file: AVAudioFile, with transcriber: SpeechTranscriber,
        _ events: AsyncThrowingStream<DictationEvent, any Error>.Continuation
    ) async throws {
        guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [transcriber], considering: file.processingFormat),
              let converter = BufferConverter(from: file.processingFormat, to: format)
        else { throw DictationError.unavailable }
        let analyzer = SpeechAnalyzer(modules: [transcriber])
        self.analyzer = analyzer
        try await analyzer.prepareToAnalyze(in: format)
        let (inputs, input) = AsyncStream.makeStream(of: AnalyzerInput.self)
        while file.framePosition < file.length {
            guard let buffer = AVAudioPCMBuffer(pcmFormat: file.processingFormat, frameCapacity: 4096) else { break }
            try file.read(into: buffer)
            if let converted = converter.convert(buffer) { input.yield(AnalyzerInput(buffer: converted)) }
        }
        input.finish()
        try await analyzer.start(inputSequence: inputs)
        events.yield(.listening)
        let results = Task {
            for try await result in transcriber.results {
                events.yield(.text(String(result.text.characters), isFinal: result.isFinal))
            }
        }
        try await analyzer.finalizeAndFinishThroughEndOfInput()
        try await results.value
        events.finish()
    }

    private func stopAudio() {
        if audio.isRunning { audio.stop() }
        if tapped {
            audio.inputNode.removeTap(onBus: 0)
            tapped = false
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        }
        input?.finish()
        input = nil
    }

    // Nonisolated: the system calls these closures off the main thread, where a main-actor closure would trap.

    nonisolated private static func requestPermissions() async throws {
        guard await AVAudioApplication.requestRecordPermission() else {
            throw DictationError.denied("Relay can't use the microphone.")
        }
        let status = await withCheckedContinuation { done in
            SFSpeechRecognizer.requestAuthorization { done.resume(returning: $0) }
        }
        guard status == .authorized else { throw DictationError.denied("Speech recognition is off for Relay.") }
    }

    nonisolated private static func locale() async -> Locale? {
        if let locale = await SpeechTranscriber.supportedLocale(equivalentTo: .current) { return locale }
        return await SpeechTranscriber.supportedLocale(equivalentTo: Locale(identifier: "en-US"))
    }

    /// Converts each mic buffer to the analyzer's format and reports its level.
    nonisolated private static func tap(
        from micFormat: AVAudioFormat, to format: AVAudioFormat,
        input: AsyncStream<AnalyzerInput>.Continuation,
        events: AsyncThrowingStream<DictationEvent, any Error>.Continuation
    ) throws -> AVAudioNodeTapBlock {
        guard let converter = BufferConverter(from: micFormat, to: format) else { throw DictationError.unavailable }
        return { buffer, _ in
            events.yield(.level(level(of: buffer)))
            if let converted = converter.convert(buffer) { input.yield(AnalyzerInput(buffer: converted)) }
        }
    }

    /// RMS of the first channel mapped from -50…0 dB to 0…1.
    nonisolated private static func level(of buffer: AVAudioPCMBuffer) -> Float {
        guard let samples = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return 0 }
        let n = Int(buffer.frameLength)
        var sum: Float = 0
        for i in 0..<n { sum += samples[i] * samples[i] }
        let db = 20 * log10(max(sqrt(sum / Float(n)), 1e-6))
        return min(max((db + 50) / 50, 0), 1)
    }
}

/// `AVAudioConverter` from the mic's format to the analyzer's; only used on the audio thread, one buffer at a time.
private final class BufferConverter: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let format: AVAudioFormat

    init?(from: AVAudioFormat, to: AVAudioFormat) {
        guard let converter = AVAudioConverter(from: from, to: to) else { return nil }
        converter.primeMethod = .none
        self.converter = converter
        format = to
    }

    func convert(_ buffer: AVAudioPCMBuffer) -> AVAudioPCMBuffer? {
        if buffer.format == format { return buffer }
        let ratio = format.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up))
        guard let out = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: capacity) else { return nil }
        var consumed = false
        var error: NSError?
        let status = converter.convert(to: out, error: &error) { _, inputStatus in
            if consumed {
                inputStatus.pointee = .noDataNow
                return nil
            }
            consumed = true
            inputStatus.pointee = .haveData
            return buffer
        }
        return status == .error || out.frameLength == 0 ? nil : out
    }
}

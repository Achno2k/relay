import Observation
import SwiftUI

/// What the speech side reports while dictating.
enum DictationEvent: Sendable, Equatable {
    /// The on-device speech model for the locale is downloading; the fraction once it's known.
    case downloading(Double?)
    /// The mic is open.
    case listening
    /// Mic level, 0...1.
    case level(Float)
    /// Words heard. Final text never changes; volatile text is replaced by the next result.
    case text(String, isFinal: Bool)
}

enum DictationError: Error, Equatable {
    /// A permission is off; the message names it.
    case denied(String)
    /// No on-device transcriber for this device or locale, or no microphone.
    case unavailable
}

/// The microphone and recognizer. The composer only sees events, so a fake drives the tests.
@MainActor
protocol DictationEngine: AnyObject {
    /// Asks for permission, readies the model and starts listening. The stream ends after `finish()`,
    /// once the last words are in, or throws `DictationError`.
    func start() -> AsyncThrowingStream<DictationEvent, any Error>
    /// Stops listening; words already spoken still arrive.
    func finish()
    /// Stops listening and drops whatever hasn't arrived.
    func cancel()
}

/// Where dictated words go: after the text that was in the composer, joined with a space when needed.
/// Pure, so it's tested without a microphone.
struct DictationInsertion: Equatable {
    private(set) var base: String
    /// Final results so far.
    private var heard = ""
    /// The newest volatile result, replaced by the next one.
    private var pending = ""
    /// The field as dictation last wrote it.
    private(set) var written: String

    init(base: String) {
        self.base = base
        written = base
    }

    /// The field with `text` folded in, or nil when it no longer holds what dictation last wrote
    /// (the user typed: their edit wins and dictation stops).
    mutating func apply(_ text: String, isFinal: Bool, current: String) -> String? {
        guard current == written else { return nil }
        if isFinal {
            heard = Self.join(heard, text)
            pending = ""
        } else {
            pending = text
        }
        written = Self.join(base, Self.join(heard, pending))
        return written
    }

    /// `b` after `a`, with one space between unless `a` ends in whitespace or `b` starts with punctuation.
    static func join(_ a: String, _ b: String) -> String {
        let b = String(b.trimmingPrefix(while: \.isWhitespace))
        guard !b.isEmpty else { return a }
        guard let last = a.last, !last.isWhitespace, !(b.first.map { ".,!?;:".contains($0) } ?? false) else { return a + b }
        return a + " " + b
    }
}

/// Dictation into the composer: tap mic, the live transcript streams into the field, tap stop. Never sends.
@MainActor
@Observable
final class Dictation {
    enum Phase: Equatable {
        case idle
        /// Asking for permission or loading the model.
        case starting
        case downloading(Double?)
        case listening
    }

    /// Why the last attempt didn't work, shown above the composer.
    struct Notice: Equatable {
        var message: String
        /// A permission is off: offer the Settings link.
        var opensSettings = false
    }

    private(set) var phase: Phase = .idle
    /// Mic level 0...1 while listening, for the indicator.
    private(set) var level: Float = 0
    var notice: Notice?

    var isActive: Bool { phase != .idle }

    @ObservationIgnored private let makeEngine: @MainActor () -> any DictationEngine
    @ObservationIgnored private var engine: (any DictationEngine)?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var insertion = DictationInsertion(base: "")

    init(makeEngine: @escaping @MainActor () -> any DictationEngine = Dictation.defaultEngine) {
        self.makeEngine = makeEngine
    }

    static func defaultEngine() -> any DictationEngine {
        #if DEBUG
        if LaunchOptions.current.mock, !LaunchOptions.current.realDictation { return ScriptedDictationEngine() }
        #endif
        return SpeechDictationEngine()
    }

    /// The mic button: start, or stop listening (keeping what was heard). Stopping while it starts cancels.
    func toggle(_ text: Binding<String>) {
        switch phase {
        case .idle: start(text)
        case .listening: finish()
        case .starting, .downloading: cancel()
        }
    }

    func start(_ text: Binding<String>) {
        cancel()
        notice = nil
        phase = .starting
        insertion = DictationInsertion(base: text.wrappedValue)
        let engine = makeEngine()
        self.engine = engine
        let events = engine.start()
        task = Task { [weak self] in
            do {
                for try await event in events {
                    guard !Task.isCancelled, let self else { return }
                    self.handle(event, text)
                }
            } catch {
                guard !Task.isCancelled else { return }
                self?.notice = Self.notice(for: error)
            }
            guard !Task.isCancelled else { return }
            self?.ended()
        }
    }

    /// Stops listening; words already spoken still land in the field.
    func finish() {
        guard isActive else { return }
        if phase != .listening {
            cancel()
            return
        }
        phase = .idle
        level = 0
        engine?.finish()
    }

    /// Stops and drops anything not yet in the field (send, chat change).
    func cancel() {
        task?.cancel()
        task = nil
        engine?.cancel()
        engine = nil
        phase = .idle
        level = 0
    }

    /// The field changed; typing over the transcript while dictating stops it.
    func noteEdit(_ text: String) {
        if isActive, text != insertion.written { cancel() }
    }

    private func handle(_ event: DictationEvent, _ text: Binding<String>) {
        switch event {
        case .downloading(let fraction):
            if isActive { phase = .downloading(fraction) }
        case .listening:
            if isActive { phase = .listening }
        case .level(let value):
            if phase == .listening { level = value }
        case .text(let words, let isFinal):
            guard let new = insertion.apply(words, isFinal: isFinal, current: text.wrappedValue) else {
                cancel()
                return
            }
            text.wrappedValue = new
        }
    }

    private func ended() {
        task = nil
        engine = nil
        phase = .idle
        level = 0
    }

    private static func notice(for error: any Error) -> Notice {
        switch error as? DictationError {
        case .denied(let message): Notice(message: message, opensSettings: true)
        case .unavailable: Notice(message: "Dictation isn't available on this device.")
        case nil: Notice(message: "Dictation stopped. Try again.")
        }
    }
}

#if DEBUG
/// `-mock`: "hears" a sentence word by word, so the simulator and UI tests run without a microphone.
@MainActor
final class ScriptedDictationEngine: DictationEngine {
    private var task: Task<Void, Never>?
    private var finishing = false

    func start() -> AsyncThrowingStream<DictationEvent, any Error> {
        let (stream, events) = AsyncThrowingStream.makeStream(of: DictationEvent.self)
        let words = "run the tests again and then push the branch".split(separator: " ").map(String.init)
        task = Task { [weak self] in
            events.yield(.listening)
            var heard: [String] = []
            for word in words {
                try? await Task.sleep(for: .milliseconds(400))
                guard !Task.isCancelled, self?.finishing == false else { break }
                heard.append(word)
                events.yield(.level(Float.random(in: 0.2...0.9)))
                events.yield(.text(heard.joined(separator: " "), isFinal: false))
            }
            if !heard.isEmpty, !Task.isCancelled { events.yield(.text(heard.joined(separator: " "), isFinal: true)) }
            events.finish()
        }
        return stream
    }

    func finish() { finishing = true }

    func cancel() { task?.cancel() }
}
#endif

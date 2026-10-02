import Foundation
import Speech
import Testing
@testable import Relay

private final class BundleMarker {}

/// The real on-device pipeline (`AssetInventory` → `SpeechAnalyzer` → `SpeechTranscriber`) on a synthetic
/// recording made with `say`. Needs a device with the transcriber; the simulator usually has none.
@Suite("Dictation on device")
@MainActor
struct DictationDeviceTests {
    @Test(.enabled(if: SpeechTranscriber.isAvailable), .timeLimit(.minutes(5)))
    func transcribesARecording() async throws {
        let url = try #require(Bundle(for: BundleMarker.self).url(forResource: "hello-relay", withExtension: "wav"))
        let locale = await SpeechTranscriber.supportedLocale(equivalentTo: .current)
        let before = await AssetInventory.status(forModules: [SpeechTranscriber(locale: locale ?? Locale(identifier: "en-US"), preset: .progressiveTranscription)])
        print("dictation-device: locale \(locale?.identifier ?? "none, en-US"), asset status before \(before)")

        let engine = SpeechDictationEngine(source: .file(url))
        var downloads: [Double?] = []
        var final = ""
        var volatile = 0
        for try await event in engine.start() {
            switch event {
            case .downloading(let fraction): downloads.append(fraction)
            case .text(let text, let isFinal):
                if isFinal { final = DictationInsertion.join(final, text) } else { volatile += 1 }
            case .listening, .level: break
            }
        }
        print("dictation-device: downloads \(downloads.count), last \(String(describing: downloads.last)), volatile \(volatile), final \"\(final)\"")
        let words = final.lowercased()
        #expect(words.contains("hello"), "heard: \(final)")
        #expect(words.contains("test"), "heard: \(final)")
        if before < .installed { #expect(!downloads.isEmpty, "the missing model wasn't reported as downloading") }
        let after = await AssetInventory.status(forModules: [SpeechTranscriber(locale: locale ?? Locale(identifier: "en-US"), preset: .progressiveTranscription)])
        #expect(after == .installed)
    }
}

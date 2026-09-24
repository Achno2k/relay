import Foundation
import Testing
@testable import RelayCore

/// Feeds screen readers and name sanitisers a pile of adversarial/random input. There's no single
/// expected output here — the property under test is "never crashes, never hangs, never leaks an
/// absolute path or an unsafe filename", per the round-5 hardening brief.
@Suite struct FuzzTests {
    static let pool: [Character] = Array(
        "abcXYZ012 \n\t\r❯›▶☐☒✔☑✓←→│┃║╭╮╰╯┌┐└┘─━═-–—╌┄()[]{}.,:;!?\"'`~@#$%^&*+=|\\/<>_🎉🙂👍️\u{0}\u{1B}\u{7F}\u{200B}\u{FEFF}😀"
    )

    static func randomString(_ rng: inout some RandomNumberGenerator, maxLen: Int = 400) -> String {
        let len = Int.random(in: 0...maxLen, using: &rng)
        return String((0..<len).map { _ in pool.randomElement(using: &rng)! })
    }

    @Test func approvalParserNeverCrashesOnRandomScreens() {
        var rng = SystemRandomNumberGenerator()
        for _ in 0..<500 {
            let screen = Self.randomString(&rng)
            let kind = [nil, "claude", "codex", "pi", "gemini"].randomElement(using: &rng)!
            _ = ApprovalParser.parse(screen, agentId: "w1:p1", cwdName: "proj", kind: kind)
            _ = ApprovalParser.fallback(screen, agentId: "w1:p1")
            _ = ApprovalParser.step(screen)
        }
    }

    @Test func pickerParserNeverCrashesOnRandomScreens() {
        var rng = SystemRandomNumberGenerator()
        for _ in 0..<500 {
            _ = Picker.parse(Self.randomString(&rng))
        }
    }

    @Test func inputBoxNeverCrashesOnRandomScreens() {
        var rng = SystemRandomNumberGenerator()
        for _ in 0..<500 {
            let s = Self.randomString(&rng)
            _ = InputBox.content(s)
            _ = InputBox.lastPromptLine(s)
        }
    }

    @Test func pathScrubberNeverCrashesAndRemovesTheCwdAtAWordBoundary() {
        var rng = SystemRandomNumberGenerator()
        let cwd = "/Users/dev/shop-api"
        let scrubber = PathScrubber(cwd: cwd)
        // Boundary characters only, so `cwd` always ends a path component rather than starting a
        // longer sibling directory name (e.g. `/Users/dev/shop-apiXYZ`, which correctly isn't scrubbed).
        let boundary: [Character] = Array(" \n\t/'\"()[]{}:;,|<>")
        for _ in 0..<500 {
            let before = Self.randomString(&rng, maxLen: 40)
            // Only the character right after `cwd` decides whether it's scrubbed as a bare match
            // (nothing, or a non-identifier char) vs. part of a longer sibling name (an identifier char,
            // correctly left alone). Force the boundary case here; PathScrubberTests covers the other.
            let after = Bool.random(using: &rng) ? "" : String(boundary.randomElement(using: &rng)!) + Self.randomString(&rng, maxLen: 40)
            #expect(!scrubber.scrub(before + cwd + after).contains(cwd))
        }
    }

    /// Attachment names: no path traversal, no separators, never empty, always within the length cap.
    @Test func uploadNameSanitizeIsAlwaysSafe() {
        var rng = SystemRandomNumberGenerator()
        let pool: [Character] = Array("abcXYZ012../\\.._%00\u{0}\u{200B}🎉😀 .-_")
        for _ in 0..<1000 {
            let len = Int.random(in: 0...300, using: &rng)
            let raw = String((0..<len).map { _ in pool.randomElement(using: &rng)! })
            let name = UploadStore.sanitize(raw)
            #expect(!name.isEmpty)
            #expect(!name.contains("/"))
            #expect(!name.contains("\\"))
            #expect(name != "..")
            #expect(name != ".")
            #expect(name.utf8.count <= 93)  // 80-char stem + "." + up to 12-char extension
            // `dir.appendingPathComponent("\(id)-\(name)")` never escapes `dir`: the 16-hex id prefix
            // means this joined component can never be exactly "." or "..".
            let store = UploadStore(root: URL(fileURLWithPath: "/tmp/relay-fuzz-root"))
            let joined = store.root.appendingPathComponent("id0123456789abcd-\(name)").standardizedFileURL
            #expect(joined.path.hasPrefix(store.root.standardizedFileURL.path))
        }
    }

    /// The transcript parser must never crash on garbage JSONL, invalid UTF-8, or partial lines —
    /// it should just skip lines it can't make sense of.
    @Test func transcriptParserNeverCrashesOnGarbageLines() {
        var rng = SystemRandomNumberGenerator()
        for format in [TranscriptFormat.claude, .pi, .codex] {
            var parser = TranscriptParser(format: format, cwd: "/Users/dev/app")
            for _ in 0..<300 {
                let len = Int.random(in: 0...500, using: &rng)
                var bytes = [UInt8](repeating: 0, count: len)
                for i in bytes.indices { bytes[i] = UInt8.random(in: 0...255, using: &rng) }
                _ = parser.consume(line: Data(bytes))
            }
        }
    }
}

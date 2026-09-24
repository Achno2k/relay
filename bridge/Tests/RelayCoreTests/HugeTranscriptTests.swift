import Foundation
import Testing
@testable import RelayCore

/// Round-5 hardening: ">50 MB" transcripts must not crash and must not pull the whole file (plus
/// every parsed message) into memory just to answer one page of `/messages`.
@Suite struct HugeTranscriptTests {
    @Test func readBoundedReturnsTheWholeFileUnderTheCap() throws {
        let url = URL(fileURLWithPath: "/tmp/relay-huge-\(UUID().uuidString.prefix(8)).jsonl")
        defer { try? FileManager.default.removeItem(at: url) }
        try Data("line one\nline two\n".utf8).write(to: url)
        let data = try AgentService.readBounded(url, size: 18, cap: 1024)
        #expect(String(decoding: data, as: UTF8.self) == "line one\nline two\n")
    }

    @Test func readBoundedTailsAndAlignsToTheNextNewlineOverTheCap() throws {
        let url = URL(fileURLWithPath: "/tmp/relay-huge-\(UUID().uuidString.prefix(8)).jsonl")
        defer { try? FileManager.default.removeItem(at: url) }
        // Ten 10-byte lines; a cap of 25 bytes lands mid-line-8, so line 8 is dropped as a fragment
        // and only lines 9 and 10 survive.
        let lines = (1...10).map { String(format: "line-%03d\n", $0) }  // 10 bytes each
        let content = lines.joined()
        try Data(content.utf8).write(to: url)
        let size = UInt64(content.utf8.count)
        let data = try AgentService.readBounded(url, size: size, cap: 25)
        let kept = String(decoding: data, as: UTF8.self)
        #expect(kept == "line-009\nline-010\n")
        #expect(data.count <= 25 + 10)  // never more than one extra line's worth over the cap
    }

    @Test func hugeRealTranscriptDoesNotCrashAndStaysBounded() throws {
        let projects = URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-huge-projects-\(UUID().uuidString.prefix(8))")
        let dir = projects.appendingPathComponent(TranscriptLocator.projectDirName(for: "/Users/dev/shop-api"))
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: projects) }
        let url = dir.appendingPathComponent("huge.jsonl")

        // A >64 MB JSONL file of real Claude assistant lines, so it's not just noise the parser skips.
        // ~4 KB lines and big batched writes: this only needs to prove the read is bounded, not stress
        // the filesystem, and the naive one-write-per-line version made this test minutes slow.
        let line: [String: Any] = [
            "type": "assistant", "uuid": "00000000-0000-0000-0000-000000000000", "isSidechain": false,
            "timestamp": "2026-09-24T10:00:00+00:00",
            "message": ["content": [["type": "text", "text": String(repeating: "x", count: 4000)]]],
        ]
        var lineData = try JSONSerialization.data(withJSONObject: line)
        lineData.append(UInt8(ascii: "\n"))
        let target = AgentService.maxTranscriptReadBytes + (1 << 20)  // a bit over the real cap
        let repeats = Int(target / UInt64(lineData.count)) + 1
        let batch = Data((0..<200).flatMap { _ in lineData })
        FileManager.default.createFile(atPath: url.path, contents: nil)
        let h = try FileHandle(forWritingTo: url)
        var written = 0
        while written < repeats {
            try h.write(contentsOf: batch)
            written += 200
        }
        try h.close()

        let service = AgentService(herdr: HerdrClient(socketPath: "/tmp/relay-unused.sock"),
                                    locator: TranscriptLocator(claudeProjects: projects, codex: CodexRollouts(root: projects.appendingPathComponent("codex"))))
        let ref = TranscriptRef(url: url, format: .claude, cwd: "/Users/dev/shop-api")
        let start = ContinuousClock.now
        let messages = try service.transcriptMessages(ref)
        #expect(!messages.isEmpty)
        // Every message came from the same synthetic uuid, so the parser folded them into one
        // assistant message; either way, the read itself must have been bounded, not the whole file.
        #expect(ContinuousClock.now - start < .seconds(20))
    }
}

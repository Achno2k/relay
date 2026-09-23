import Foundation
import Synchronization
import Testing
@testable import HerdCore

@Suite struct TailerTests {
    @Test func emitsOnlyAppendedGrowthAndWaitsForPartialLines() async throws {
        let url = URL(fileURLWithPath: "/tmp").appendingPathComponent("herd-tail-\(UUID().uuidString.prefix(8)).jsonl")
        defer { try? FileManager.default.removeItem(at: url) }
        let user = #"{"type":"user","uuid":"u1","timestamp":"2026-09-23T13:00:00Z","message":{"content":"hello"}}"#
        try Data((user + "\n").utf8).write(to: url)

        let got = Mutex<[Message]>([])
        let tailer = TranscriptTailer(url: url, format: .claude, cwd: nil) { m in got.withLock { $0.append(m) } }
        tailer.start()
        defer { tailer.stop() }

        let a1 = #"{"type":"assistant","uuid":"a1","timestamp":"2026-09-23T13:00:01Z","message":{"content":[{"type":"text","text":"one"}]}}"#
        let a2 = #"{"type":"assistant","uuid":"a2","timestamp":"2026-09-23T13:00:02Z","message":{"content":[{"type":"text","text":"two"}]}}"#
        let h = try FileHandle(forWritingTo: url)
        try h.seekToEnd()
        try h.write(contentsOf: Data((a1 + "\n" + a2.prefix(20)).utf8))
        try await waitUntil { got.withLock { $0.count } == 1 }
        #expect(got.withLock { $0[0].id } == "a1")
        #expect(got.withLock { $0[0].blocks } == [.text("one")])

        try h.write(contentsOf: Data((a2.dropFirst(20) + "\n").utf8))
        try h.close()
        try await waitUntil { got.withLock { $0.count } == 2 }
        // The same assistant message grew.
        #expect(got.withLock { $0[1].id } == "a1")
        #expect(got.withLock { $0[1].blocks } == [.text("one"), .text("two")])
    }

    @Test func diesWhenFileIsRemoved() async throws {
        let url = URL(fileURLWithPath: "/tmp").appendingPathComponent("herd-tail-\(UUID().uuidString.prefix(8)).jsonl")
        try Data().write(to: url)
        let tailer = TranscriptTailer(url: url, format: .claude, cwd: nil) { _ in }
        tailer.start()
        #expect(!tailer.isDead)
        try FileManager.default.removeItem(at: url)
        try await waitUntil { tailer.isDead }
    }

    func waitUntil(_ cond: @Sendable () -> Bool) async throws {
        for _ in 0..<200 {
            if cond() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("timed out")
    }
}

import Foundation
import Testing
@testable import RelayCore

@Suite struct TranscriptParserTests {
    let cwd = "/Users/dev/shop-api"

    func messages() throws -> [Message] {
        TranscriptParser.parse(try Fixture.data("claude-session.jsonl"), format: .claude, cwd: cwd)
    }

    @Test func keepsOnlyRealUserAndAssistantMessages() throws {
        let ms = try messages()
        #expect(ms.map(\.role) == [.user, .assistant, .user, .assistant])
        #expect(ms.map(\.id) == ["u1", "a1", "u2", "a6"])
    }

    @Test func dropsMetaCommandsRemindersAndSidechains() throws {
        let text = try messages().flatMap(\.blocks).compactMap { b -> String? in
            if case .text(let t) = b { return t }
            return nil
        }.joined(separator: "\n")
        #expect(!text.contains("meta line"))
        #expect(!text.contains("/model"))
        #expect(!text.contains("Set model"))
        #expect(!text.contains("ignore"))
        #expect(!text.contains("sidechain"))
    }

    @Test func mergesAssistantLinesAndAttachesToolResults() throws {
        let a = try messages()[1]
        #expect(a.createdAt == "2026-09-23T13:00:04+00:00")
        #expect(a.blocks == [
            .thinking("Need to find where the token is read first."),
            .toolCall(id: "t1", name: "Grep", summary: "Searched for read_token", input: #"{"path":"src","pattern":"read_token"}"#),
            .toolResult(toolCallId: "t1", isError: false, preview: "src/auth.py:14: def read_token():\nsrc/auth.py:31:     tok = read_token()"),
            .toolCall(id: "t2", name: "Edit", summary: "Edited src/auth.py", input: #"{"file_path":"src/auth.py","new_string":"b","old_string":"a"}"#),
            .toolResult(toolCallId: "t2", isError: false, preview: "The file src/auth.py has been updated."),
            .toolCall(id: "t3", name: "Bash", summary: "Ran uv run pytest -q", input: #"{"command":"uv run pytest -q\necho done","description":"Run tests"}"#),
            .toolResult(toolCallId: "t3", isError: true, preview: "1 failed, 41 passed in tmp"),
            .text("Cached the token in `src/auth.py`. One test failed. See https://docs.python.org/3/library/functools.html for details."),
        ])
    }

    @Test func scrubsUserTextPaths() throws {
        #expect(try messages()[0].blocks == [.text("The auth middleware in src/auth.py re-reads the token file on every request. Cache it.")])
    }

    @Test func incrementalConsumeReportsGrowingMessage() throws {
        let lines = try Fixture.data("claude-session.jsonl").split(separator: UInt8(ascii: "\n"))
        var p = TranscriptParser(format: .claude, cwd: cwd)
        var upserts: [(String, Int)] = []
        for l in lines {
            if let m = p.consume(line: Data(l)) { upserts.append((m.id, m.blocks.count)) }
        }
        #expect(upserts.map(\.0) == ["u1", "a1", "a1", "a1", "a1", "a1", "a1", "a1", "a1", "u2", "a6"])
        #expect(upserts.map(\.1) == [1, 1, 2, 3, 4, 5, 6, 7, 8, 1, 1])
        #expect(p.messages == (try messages()))
    }

    @Test func orphanToolResultIsDropped() {
        let line = #"{"type":"user","isSidechain":false,"uuid":"x","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"hi"}]}}"#
        var p = TranscriptParser(format: .claude, cwd: nil)
        #expect(p.consume(line: Data(line.utf8)) == nil)
        #expect(p.messages.isEmpty)
    }

    @Test func ignoresGarbageAndPartialLines() {
        var p = TranscriptParser(format: .claude, cwd: nil)
        #expect(p.consume(line: Data("{\"type\":\"user\",\"mess".utf8)) == nil)
        #expect(p.consume(line: Data("not json".utf8)) == nil)
    }

    @Test func truncatesLongPreviews() {
        let long = String(repeating: "x", count: 1000)
        let lines = [
            #"{"type":"assistant","uuid":"a","message":{"content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls"}}]}}"#,
            #"{"type":"user","uuid":"r","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"\#(long)"}]}}"#,
        ].joined(separator: "\n")
        let ms = TranscriptParser.parse(Data(lines.utf8), format: .claude, cwd: nil)
        guard case .toolResult(_, _, let preview) = ms[0].blocks[1] else {
            Issue.record("expected toolResult")
            return
        }
        #expect(preview.count == ToolSummary.previewLimit + 1)
        #expect(preview.hasSuffix("…"))
    }

    @Test func parsesPi() throws {
        let ms = TranscriptParser.parse(try Fixture.data("pi-session.jsonl"), format: .pi, cwd: "/Users/dev/website")
        #expect(ms.map(\.role) == [.user, .assistant])
        #expect(ms[0].blocks == [.text("List the pages")])
        #expect(ms[1].id == "p2")
        #expect(ms[1].blocks == [
            .thinking("Look at the pages dir."),
            .toolCall(id: "c1", name: "bash", summary: "Ran ls pages", input: #"{"command":"ls pages"}"#),
            .toolResult(toolCallId: "c1", isError: false, preview: "index.tsx\nabout.tsx"),
            .text("Two pages: index and about."),
        ])
    }

    @Test func encodesLikeTheContract() throws {
        let m = Message(id: "m", role: .assistant, createdAt: "2026-09-23T13:00:04+00:00", blocks: [
            .toolResult(toolCallId: "t1", isError: false, preview: "ok"),
        ])
        let enc = JSONEncoder()
        enc.outputFormatting = .sortedKeys
        let s = String(decoding: try enc.encode(m), as: UTF8.self)
        #expect(s == #"{"blocks":[{"isError":false,"preview":"ok","toolCallId":"t1","type":"toolResult"}],"createdAt":"2026-09-23T13:00:04+00:00","id":"m","role":"assistant"}"#)
    }

    @Test func decodesContractFixture() throws {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("docs/fixtures/messages.json")
        let page = try JSONDecoder().decode(MessagePage.self, from: Data(contentsOf: url))
        #expect(page.messages.count == 4)
    }
}

import Foundation
import Testing
@testable import HerdCore

@Suite struct CodexTranscriptTests {
    let cwd = "/Users/dev/project"

    func messages() throws -> [Message] {
        TranscriptParser.parse(try Fixture.data("codex-session.jsonl"), format: .codex, cwd: cwd,
                               uploads: UploadStore(root: URL(fileURLWithPath: "/nonexistent")))
    }

    @Test func userAndAssistantTurns() throws {
        let ms = try messages()
        #expect(ms.map(\.role) == [.user, .assistant, .user])
        #expect(ms.map(\.id) == ["u1", "rs1", "u2"])
        #expect(ms[0].blocks == [.text("Fix the failing test in src/app.py")])
        #expect(ms[0].createdAt == "2026-09-24T14:00:02+00:00")
        #expect(ms[2].blocks == [.text("Thanks")])
    }

    @Test func assistantItemsMergeIntoOneMessage() throws {
        let blocks = try messages()[1].blocks
        #expect(blocks[0] == .thinking("**Finding the failing test**"))
        #expect(blocks[1] == .text("Running the tests first."))
        #expect(blocks[2] == .toolCall(id: "exec-1", name: "Shell", summary: "Ran pytest -q", input: #"{"command":"pytest -q\necho done","cwd":"."}"#))
        #expect(blocks[3] == .toolResult(toolCallId: "exec-1", isError: true, preview: "1 failed in tests/test_app.py"))
        #expect(blocks[4] == .toolCall(id: "exec-2", name: "Edit", summary: "Edited src/app.py", input: #"{"files":["src/app.py"]}"#))
        #expect(blocks[5] == .toolResult(toolCallId: "exec-2", isError: false, preview: "src/app.py\n@@ -1 +1 @@\n-return 1\n+return 2"))
        #expect(blocks[6] == .toolCall(id: "exec-3", name: "docs.search", summary: "Called docs.search", input: #"{"q":"pytest"}"#))
        #expect(blocks[7] == .toolResult(toolCallId: "exec-3", isError: false, preview: "3 results"))
        #expect(blocks[8] == .toolCall(id: "exec-4", name: "WebSearch", summary: "Searched the web for pytest fixtures", input: #"{"query":"pytest fixtures"}"#))
        // A declined command is an error result.
        #expect(blocks[11] == .toolResult(toolCallId: "exec-5", isError: true, preview: ""))
        #expect(blocks.last == .text("Fixed `src/app.py`."))
        #expect(blocks.count == 13)  // the empty reasoning item adds nothing
    }

    @Test func rawResponseItemsAreIgnored() throws {
        let json = String(decoding: try JSONEncoder().encode(try messages()), as: UTF8.self)
        #expect(!json.contains("environment_context"))
        #expect(!json.contains("tools.exec_command"))
        #expect(!json.contains("/Users/dev"))
    }

    @Test func incrementalGrowth() throws {
        var p = TranscriptParser(format: .codex, cwd: cwd, uploads: UploadStore(root: URL(fileURLWithPath: "/nonexistent")))
        var counts: [(String, Int)] = []
        for line in try Fixture.data("codex-session.jsonl").split(separator: UInt8(ascii: "\n")) {
            if let m = p.consume(line: Data(line)) { counts.append((m.id, m.blocks.count)) }
        }
        #expect(counts.map(\.0) == ["u1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "u2"])
        #expect(counts.map(\.1) == [1, 1, 2, 4, 6, 8, 10, 12, 13, 1])
    }

    @Test func locatorFindsBySessionIdOrNewestInCwd() throws {
        let root = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("herd-codex-\(UUID().uuidString.prefix(8))")
        defer { try? FileManager.default.removeItem(at: root) }
        let day = root.appendingPathComponent("2026/09/24")
        try FileManager.default.createDirectory(at: day, withIntermediateDirectories: true)
        let a = day.appendingPathComponent("rollout-2026-09-24T14-00-00-01a0d390-0000-7a41-979a-000000000002.jsonl")
        try Fixture.data("codex-session.jsonl").write(to: a)
        let b = day.appendingPathComponent("rollout-2026-09-24T15-00-00-01a0d390-0000-7a41-979a-000000000009.jsonl")
        try Data(#"{"timestamp":"2026-09-24T15:00:00Z","type":"session_meta","payload":{"id":"x","cwd":"/Users/dev/other"}}"#.utf8).write(to: b)

        let rollouts = CodexRollouts(root: root)
        #expect(rollouts.find(sessionId: "01a0d390-0000-7a41-979a-000000000002") == a)
        #expect(rollouts.find(sessionId: "nope") == nil)
        #expect(rollouts.newest(cwd: "/Users/dev/project") == a)
        #expect(rollouts.newest(cwd: "/Users/dev/other") == b)
        #expect(rollouts.newest(cwd: "/Users/dev/none") == nil)

        let locator = TranscriptLocator(claudeProjects: root, codex: rollouts)
        func agent(session: String?) -> HerdrAgent {
            HerdrAgent(paneId: "w1:p1", workspaceId: "w1", tabId: "w1:t1", name: nil, agent: "codex", displayAgent: nil,
                       agentStatus: .idle, title: nil, terminalTitleStripped: nil, cwd: "/Users/dev/project", foregroundCwd: nil,
                       agentSession: session.map { HerdrAgentSession(source: "herdr:codex", agent: "codex", kind: "id", value: $0) },
                       stateChangeSeq: nil, revision: nil)
        }
        #expect(locator.locate(agent(session: "01a0d390-0000-7a41-979a-000000000002")) == TranscriptRef(url: a, format: .codex, cwd: "/Users/dev/project"))
        // No session id from herdr yet: newest rollout started in the agent's folder.
        #expect(locator.locate(agent(session: nil))?.url == a)
    }
}

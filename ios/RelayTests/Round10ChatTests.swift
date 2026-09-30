import Foundation
import RelayKit
import Testing
@testable import Relay

private func claudeAgent(status: AgentStatus = .idle) -> Agent {
    Agent(
        id: "w1:p1", name: nil, kind: "claude", title: "Test agent", workspaceId: "w1", workspaceName: "shop-api",
        cwdName: "shop-api", status: status, hasTranscript: true, updatedAt: .now
    )
}

/// B3/B8: the chat is busy ("Thinking…") from the tap on send, before herdr reports `working`.
@MainActor
@Suite("Round 10: awaiting a reply")
struct AwaitingReplyTests {
    private func store(_ backend: ProgrammableBackend, agent: Agent) -> AppStore {
        let store = AppStore(backend: backend, hostLabel: "test")
        store.apply(.agentUpdated(agent))
        return store
    }

    @Test func sendAwaitsUntilWorking() async throws {
        var agent = claudeAgent()
        let store = store(ProgrammableBackend(), agent: agent)
        store.send("hi", to: agent.id)
        #expect(store.awaitingReply[agent.id] != nil)

        agent.status = .working
        store.apply(.agentUpdated(agent))
        #expect(store.awaitingReply[agent.id] == nil)
    }

    @Test func blockedEndsTheWait() {
        var agent = claudeAgent()
        let store = store(ProgrammableBackend(), agent: agent)
        store.send("hi", to: agent.id)
        agent.status = .blocked
        store.apply(.agentUpdated(agent))
        #expect(store.awaitingReply[agent.id] == nil)
    }

    /// A turn so short that herdr never reported `working`: the reply landing ends the wait.
    @Test func replyEndsTheWaitButTheEchoedPromptDoesNot() {
        let agent = claudeAgent()
        let store = store(ProgrammableBackend(), agent: agent)
        store.send("hi", to: agent.id)
        let echo = Message(id: "u1", role: .user, createdAt: .now, blocks: [.text("hi")])
        store.apply(.messageUpserted(agentId: agent.id, message: echo))
        #expect(store.awaitingReply[agent.id] != nil)

        let reply = Message(id: "a1", role: .assistant, createdAt: .now, blocks: [.text("hello")])
        store.apply(.messageUpserted(agentId: agent.id, message: reply))
        #expect(store.awaitingReply[agent.id] == nil)
    }

    @Test func olderAssistantMessageDoesNotEndTheWait() {
        let agent = claudeAgent()
        let store = store(ProgrammableBackend(), agent: agent)
        store.send("hi", to: agent.id)
        let old = Message(id: "a0", role: .assistant, createdAt: .now.addingTimeInterval(-60), blocks: [.text("earlier")])
        store.apply(.messageUpserted(agentId: agent.id, message: old))
        #expect(store.awaitingReply[agent.id] != nil)
    }

    @Test func stopEndsTheWait() {
        let agent = claudeAgent()
        let store = store(ProgrammableBackend(), agent: agent)
        store.send("hi", to: agent.id)
        store.interrupt(agent.id)
        #expect(store.awaitingReply[agent.id] == nil)
    }
}

/// B7: the plan from plan mode, on the wire and in the chat.
@Suite("Round 10: plans and edits")
struct PlanItemTests {
    private static let transcript = """
    {"hasMore":false,"messages":[
     {"id":"e1","role":"user","createdAt":"2026-09-30T10:00:00+00:00","blocks":[{"type":"text","text":"Plan first."}]},
     {"id":"e2","role":"assistant","createdAt":"2026-09-30T10:00:05+00:00","blocks":[
      {"type":"toolCall","id":"t1","name":"Read","summary":"Read src/auth.py","path":"src/auth.py"},
      {"type":"toolResult","toolCallId":"t1","isError":false},
      {"type":"toolCall","id":"t2","name":"Write","summary":"Wrote quiet-orange-kettle.md","edit":{"kind":"write","content":"# Plan\\n","truncated":false}},
      {"type":"toolResult","toolCallId":"t2","isError":false},
      {"type":"toolCall","id":"t3","name":"ExitPlanMode","summary":"ExitPlanMode","input":"{}","plan":"# Cache the auth token\\n\\n1. Wrap it.\\n"},
      {"type":"toolResult","toolCallId":"t3","isError":false,"preview":"User has approved your plan."},
      {"type":"toolCall","id":"t4","name":"MultiEdit","summary":"Edited src/server.py","path":"src/server.py","edit":{"kind":"edit","changes":[{"old":"import os","new":"import os\\nimport signal"},{"old":"A = 5","new":"A = 10","replaceAll":true}]}},
      {"type":"toolCall","id":"t5","name":"Edit","summary":"Edited 2 files","edit":{"kind":"diff","diff":"--- a/x\\n+++ b/x\\n","truncated":true}},
      {"type":"toolCall","id":"t6","name":"Edit","summary":"Edited y","edit":{"kind":"patch"}},
      {"type":"text","text":"Done."}
     ]}
    ]}
    """

    private func page() throws -> MessagePage {
        try RelayJSON.decoder().decode(MessagePage.self, from: Data(Self.transcript.utf8))
    }

    private func calls(_ page: MessagePage) -> [ToolCall] {
        page.messages.flatMap(\.blocks).compactMap { if case .toolCall(let c) = $0 { c } else { nil } }
    }

    @Test func toolCallFieldsDecode() throws {
        let calls = calls(try page())
        #expect(calls[0].path == "src/auth.py")
        #expect(calls[0].edit == nil)
        #expect(calls[1].edit?.kind == .write)
        #expect(calls[1].edit?.content == "# Plan\n")
        #expect(calls[2].plan == "# Cache the auth token\n\n1. Wrap it.\n")
        #expect(calls[3].edit?.changes == [
            ToolEditChange(old: "import os", new: "import os\nimport signal"),
            ToolEditChange(old: "A = 5", new: "A = 10", replaceAll: true),
        ])
        #expect(calls[4].edit?.kind == .diff)
        #expect(calls[4].edit?.truncated == true)
        #expect(calls[5].edit?.kind == .unknown, "a newer kind must not break decoding")
    }

    @Test func approvalPlanDecodes() throws {
        let json = """
        {"agentId":"w1:p2","question":"Claude has written up a plan and is ready to execute. Would you like to proceed?",
         "options":[{"label":"Yes, and auto-accept edits","keys":["1"]},{"label":"No, keep planning","keys":["3"]}],
         "plan":"# Cache the auth token\\n"}
        """
        let approval = try RelayJSON.decoder().decode(Approval.self, from: Data(json.utf8))
        #expect(approval.plan == "# Cache the auth token\n")
        let plain = try RelayJSON.decoder().decode(Approval.self, from: Data(#"{"agentId":"a","question":"q","options":[]}"#.utf8))
        #expect(plain.plan == nil)
    }

    /// The plan follows the tool group it's in, and never splits it: ExitPlanMode's result still lands.
    @Test func planFollowsItsToolGroup() throws {
        let items = ChatItem.build(from: try page().messages)
        #expect(items.map(\.kind) == ["user", "tools", "plan", "text"])
        guard case .tools(_, let steps, _) = items[1], case .plan(let id, let markdown) = items[2] else {
            Issue.record("unexpected items")
            return
        }
        #expect(steps.map(\.id) == ["t1", "t2", "t3", "t4", "t5", "t6"])
        #expect(steps.first { $0.id == "t3" }?.finished == true)
        #expect(steps[0].path == "src/auth.py")
        #expect(steps[3].edit?.changes.count == 2)
        #expect(id == "plan-t3")
        #expect(markdown.hasPrefix("# Cache the auth token"))
    }

    @Test func emptyPlanAddsNothing() {
        let call = ToolCall(id: "t1", name: "ExitPlanMode", summary: "ExitPlanMode", plan: "  \n")
        let message = Message(id: "m", role: .assistant, createdAt: .now, blocks: [.toolCall(call)])
        #expect(ChatItem.build(from: [message]).map(\.kind) == ["tools"])
    }
}

private extension ChatItem {
    var kind: String {
        switch self {
        case .user: "user"
        case .text: "text"
        case .thinking: "thinking"
        case .tools: "tools"
        case .stopped: "stopped"
        case .live: "live"
        case .plan: "plan"
        }
    }
}

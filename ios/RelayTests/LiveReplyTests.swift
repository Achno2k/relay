import Foundation
import RelayKit
import Testing
@testable import Relay

@Suite("Live reply")
@MainActor
struct LiveReplyTests {
    private func agent(status: AgentStatus) -> Agent {
        Agent(
            id: "w1:p1", name: nil, kind: "claude", title: "chat", workspaceId: "w1", workspaceName: "shop-api",
            cwdName: "shop-api", status: status, hasTranscript: true, updatedAt: .now
        )
    }

    private func store(status: AgentStatus = .working) -> AppStore {
        let backend = RecordingBackend(agent: agent(status: status), approval: Approval(agentId: "w1:p1", question: "", options: []))
        return AppStore(backend: backend, hostLabel: "test")
    }

    @Test func replyLiveTextIsStoredPerAgent() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "The answer starts", seq: 1))
        #expect(store.liveReplyText["w1:p1"] == "The answer starts")
    }

    @Test func replyLiveGrowsThenClearsOnNull() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "The answer", seq: 1))
        store.apply(.replyLive(agentId: "w1:p1", text: "The answer starts here", seq: 2))
        #expect(store.liveReplyText["w1:p1"] == "The answer starts here")
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 3))
        #expect(store.liveReplyText["w1:p1"] == nil)
    }

    @Test func staleSeqIsIgnored() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "Second", seq: 2))
        store.apply(.replyLive(agentId: "w1:p1", text: "First, arrived late", seq: 1))
        #expect(store.liveReplyText["w1:p1"] == "Second")
    }

    @Test func differentAgentsDoNotInterfere() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "Agent one", seq: 1))
        store.apply(.replyLive(agentId: "w1:p2", text: "Agent two", seq: 1))
        #expect(store.liveReplyText["w1:p1"] == "Agent one")
        #expect(store.liveReplyText["w1:p2"] == "Agent two")
    }

    @Test func agentClosedClearsLiveText() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "In progress", seq: 1))
        store.apply(.agentClosed(agentId: "w1:p1"))
        #expect(store.liveReplyText["w1:p1"] == nil)
        // A late frame from before the close doesn't resurrect it: the seq counter restarts at 0.
        store.apply(.replyLive(agentId: "w1:p1", text: "In progress", seq: 1))
        #expect(store.liveReplyText["w1:p1"] == "In progress")
    }

    @Test func relayStateIgnoresReplyLive() throws {
        var state = RelayState()
        state.agents = try FixtureFiles.decode([Agent].self, "agents.json")
        state.setPage(try FixtureFiles.decode(MessagePage.self, "messages.json"), agentId: "w1:p1")
        let before = state
        state.apply(.replyLive(agentId: "w1:p1", text: "preview", seq: 1))
        #expect(state == before)
    }
}

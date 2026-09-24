import Foundation
import HerdKit
import Testing
@testable import Herd

@Suite("Controls")
struct ControlsTests {
    @Test func decodesAgentControlFields() throws {
        let agents = try FixtureFiles.decode([Agent].self, "agents.json")
        let opus = try #require(agents.first { $0.id == "w1:p1" })
        #expect(opus.model == "claude-opus-5-5")
        #expect(opus.modelLabel == "Opus 5.5")
        #expect(opus.permissionMode == "auto")
        #expect(opus.effort == "medium")
        #expect(opus.sessionId != nil)
        let codex = try #require(agents.first { $0.kind == "codex" })
        #expect(codex.model == nil && codex.permissionMode == nil && codex.sessionId == nil)
    }

    @Test func decodesCatalog() throws {
        let catalog = try FixtureFiles.decode(ControlsCatalog.self, "controls.json")
        #expect(catalog.models.map(\.id) == ["opus", "sonnet", "haiku", "fable"])
        #expect(catalog.modes.map(\.id) == ["default", "acceptEdits", "plan", "auto", "bypassPermissions"])
        #expect(catalog.efforts.first?.label == "Low")
    }

    @Test func encodesOneKeyPerRequest() throws {
        func json(_ r: ControlRequest) throws -> String { String(decoding: try JSONEncoder().encode(r), as: UTF8.self) }
        #expect(try json(.model("sonnet")) == #"{"model":"sonnet"}"#)
        #expect(try json(.permissionMode("plan")) == #"{"permissionMode":"plan"}"#)
        #expect(try json(.effort("high")) == #"{"effort":"high"}"#)
        #expect(try json(.command(.clear)) == #"{"command":"clear"}"#)
        #expect(ControlRequest.command(.compact).timeout > 90)
    }

    @Test func pillLabelsFollowPendingChanges() throws {
        let catalog = try FixtureFiles.decode(ControlsCatalog.self, "controls.json")
        let agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let idle = AgentControls(agent: agent, catalog: catalog, pending: nil)
        #expect(idle.subtitle == "Opus 5.5 · Default")
        #expect(idle.modelAlias == "opus")
        #expect(idle.busyCaption == nil)
        let switching = AgentControls(agent: agent, catalog: catalog, pending: .model("sonnet"))
        #expect(switching.subtitle == "Sonnet 5 · Default")
        #expect(AgentControls(agent: agent, catalog: catalog, pending: .permissionMode("plan")).subtitle == "Opus 5.5 · Plan")
        var noLabel = agent
        noLabel.modelLabel = nil
        noLabel.model = "claude-haiku-4-5-20251001"
        #expect(AgentControls(agent: noLabel, catalog: catalog, pending: nil).modelLabel == "Haiku 4.5")
    }

    @Test func newSessionDropsCachedChat() throws {
        var state = HerdState()
        var agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w1:p1" })
        state.agents = [agent]
        state.setPage(try FixtureFiles.decode(MessagePage.self, "messages.json"), agentId: agent.id)

        agent.permissionMode = "plan"
        state.apply(.agentUpdated(agent))
        #expect(state.messages[agent.id]?.count == 4, "same session keeps the chat")

        agent.sessionId = "after-clear"
        state.apply(.agentUpdated(agent))
        #expect(state.messages[agent.id] == nil, "a new session drops the chat so it's refetched")

        agent.sessionId = nil
        state.setPage(MessagePage(messages: [], hasMore: false), agentId: agent.id)
        state.apply(.agentUpdated(agent))
        #expect(state.messages[agent.id] != nil, "an unknown session id isn't a change")
    }
}

@MainActor
@Suite("AppStore controls")
struct StoreControlsTests {
    private func idleStore() async throws -> (AppStore, RecordingBackend, Agent) {
        let agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let backend = RecordingBackend(agent: agent, approval: Approval(agentId: agent.id, question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id
        await store.refresh()
        return (store, backend, agent)
    }

    @Test func successAppliesServerAgent() async throws {
        let (store, backend, agent) = try await idleStore()
        #expect(store.controls?.models.count == 4)
        store.control(.model("sonnet"), for: agent.id)
        #expect(store.pendingControls[agent.id] == .model("sonnet"))
        #expect(AgentControls(agent: agent, catalog: store.controls, pending: store.pendingControls[agent.id]).modelLabel == "Sonnet 5")
        try await waitUntil { store.pendingControls[agent.id] == nil }
        #expect(await backend.calls == [.control(.model("sonnet"))])
        #expect(store.state.agent(agent.id)?.model == "claude-sonnet-5")
        #expect(store.errorMessage == nil)
    }

    @Test func failureRevertsAndExplains() async throws {
        let (store, backend, agent) = try await idleStore()
        await backend.failControls(with: .http(status: 409, code: "agent_busy", message: "The agent is working."))
        store.control(.permissionMode("plan"), for: agent.id)
        try await waitUntil { store.pendingControls[agent.id] == nil }
        #expect(store.state.agent(agent.id)?.permissionMode == "default")
        #expect(store.errorMessage == "Couldn't switch to Plan: The agent is working.")
    }

    @Test func clearRefetchesTheChat() async throws {
        let (store, backend, agent) = try await idleStore()
        #expect(store.isLoaded(agent.id))
        store.control(.command(.clear), for: agent.id)
        try await waitUntil { store.pendingControls[agent.id] == nil }
        #expect(store.state.agent(agent.id)?.sessionId == "new-session")
        try await waitUntil { store.isLoaded(agent.id) }
        #expect(await backend.calls == [.control(.command(.clear))])
    }

    private func waitUntil(_ condition: @MainActor () -> Bool) async throws {
        for _ in 0..<60 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        Issue.record("timed out")
    }
}

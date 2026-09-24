import Foundation
import HerdKit
import Testing
@testable import Herd

@Suite("Per-agent controls")
struct PerAgentControlsTests {
    private let codexInfo = AgentControlsInfo(
        models: [ControlOption(id: "gpt-5.1-codex", label: "GPT-5.1 Codex"), ControlOption(id: "gpt-5.1", label: "GPT-5.1")],
        efforts: ["minimal", "low", "medium", "high"].map { ControlOption(id: $0, label: $0.capitalized) },
        modes: [],
        supports: .init(model: true, effort: true, mode: false, compact: true, clear: false)
    )

    private func agent(kind: String, model: String?, label: String? = nil, effort: String? = "medium", mode: String? = nil) -> Agent {
        Agent(id: "w9:p1", name: nil, kind: kind, title: "t", workspaceId: "w9", workspaceName: "w", cwdName: "w",
              status: .idle, hasTranscript: true, updatedAt: .now, model: model, modelLabel: label,
              permissionMode: mode, effort: effort, sessionId: "s1")
    }

    @Test func decodesWithMissingLists() throws {
        let json = #"{"models":[{"id":"openai/gpt-5.1","label":"GPT-5.1"}],"efforts":null,"supports":{"model":true,"effort":false,"mode":false,"compact":false,"clear":false}}"#
        let info = try HerdJSON.decoder().decode(AgentControlsInfo.self, from: Data(json.utf8))
        #expect(info.models.count == 1 && info.efforts.isEmpty && info.modes.isEmpty)
        #expect(info.supports.model && !info.supports.effort && info.supports.any)
        let claude = AgentControlsInfo(claudeCatalog: try FixtureFiles.decode(ControlsCatalog.self, "controls.json"))
        #expect(claude.supports == .init(model: true, effort: true, mode: true, compact: true, clear: true))
    }

    @Test func matchesExactIdsBeforeAliases() throws {
        let pi = [ControlOption(id: "openai/gpt-5.1", label: "GPT-5.1"), ControlOption(id: "openai/gpt-5.1-mini", label: "GPT-5.1 Mini")]
        #expect(ControlDisplay.modelId(agent(kind: "pi", model: "openai/gpt-5.1-mini"), models: pi) == "openai/gpt-5.1-mini",
                "substring matching alone would pick gpt-5.1")
        let claude = try FixtureFiles.decode(ControlsCatalog.self, "controls.json").models
        #expect(ControlDisplay.modelId(agent(kind: "claude", model: "claude-sonnet-5"), models: claude) == "sonnet")
    }

    @Test func codexShowsModelAndEffortOnly() {
        let controls = AgentControls(agent: agent(kind: "codex", model: "gpt-5.1-codex", label: "GPT-5.1 Codex", effort: "high"),
                                     info: codexInfo, pending: nil)
        #expect(controls.isAvailable)
        #expect(controls.subtitle == "GPT-5.1 Codex · High", "no mode, so effort sits next to the model")
        #expect(!controls.supports.mode && !controls.supports.clear && controls.supports.compact)
        let switching = AgentControls(agent: controls.agent, info: codexInfo, pending: .model("gpt-5.1"))
        #expect(switching.subtitle == "GPT-5.1 · High")
    }

    @Test func noInfoMeansNoControlsButTheModelStillShows() {
        let controls = AgentControls(agent: agent(kind: "pi", model: "openai/gpt-5.1", label: nil), info: nil, pending: nil)
        #expect(!controls.isAvailable)
        #expect(controls.modelLabel == "openai/gpt-5.1")
    }
}

@MainActor
@Suite("AppStore per-agent controls")
struct StoreRound3Tests {
    private func store(_ info: AgentControlsInfo?) async throws -> (AppStore, RecordingBackend, Agent) {
        let agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let backend = RecordingBackend(agent: agent, approval: Approval(agentId: "", question: "", options: []))
        await backend.setPerAgentControls(info)
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id
        await store.refresh()
        return (store, backend, agent)
    }

    @Test func cachesUntilKindOrSessionChanges() async throws {
        let info = AgentControlsInfo(models: [ControlOption(id: "a", label: "A")], efforts: [], modes: [],
                                     supports: .init(model: true, effort: false, mode: false, compact: false, clear: false))
        let (store, backend, agent) = try await store(info)
        #expect(store.agentControls[agent.id] == info)
        let fetches = await backend.agentControlsFetches
        await store.loadAgentControls(agent.id)
        #expect(await backend.agentControlsFetches == fetches, "same kind and session: cached")

        var next = agent
        next.sessionId = "after-clear"
        store.apply(.agentUpdated(next))
        await store.loadAgentControls(agent.id)
        #expect(await backend.agentControlsFetches > fetches, "a new session refetches")
    }

    @Test func olderBridgeFallsBackToClaudeList() async throws {
        let (store, _, agent) = try await store(nil)
        let info = try #require(store.agentControls[agent.id])
        #expect(info.models.map(\.id) == ["opus", "sonnet", "haiku", "fable"])
        #expect(store.controlsState(for: agent).isAvailable)
    }
}

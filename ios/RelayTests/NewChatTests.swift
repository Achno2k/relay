import Foundation
import RelayKit
import Testing
@testable import Relay

@Suite("New chat controls")
struct NewChatTests {
    @Test func decodesKindControls() throws {
        let json = #"""
        {"models":[{"id":"gpt-6-luna","label":"GPT-6-Luna"},{"id":"gpt-5.6-terra","label":"GPT-5.6-Terra"}],
         "efforts":[],"modes":[],"supports":{"model":true,"effort":true,"mode":true,"compact":true,"clear":true},
         "defaultModel":"gpt-5.6-sol","defaultEffort":"high",
         "effortsByModel":{"gpt-6-luna":[{"id":"low","label":"Low"},{"id":"xhigh","label":"Extra high"}]}}
        """#
        let info = try RelayJSON.decoder().decode(AgentControlsInfo.self, from: Data(json.utf8))
        #expect(info.defaultModel == "gpt-5.6-sol", "codex's default may be outside its list")
        #expect(info.efforts(for: "gpt-6-luna").map(\.id) == ["low", "xhigh"])
        #expect(info.efforts(for: "gpt-5.6-sol").isEmpty, "an unlisted default has no efforts to offer")
        let claude = try FixtureFiles.decode(AgentControlsInfo.self, "agent-controls-claude.json")
        #expect(claude.defaultModel == nil && claude.efforts(for: "sonnet") == claude.efforts, "shared efforts")
    }

    @Test func createBodyCarriesOnlyWhatWasPicked() throws {
        func json(_ r: CreateAgentRequest) throws -> [String: String] {
            try JSONDecoder().decode([String: String].self, from: try RelayJSON.encoder().encode(r))
        }
        #expect(try json(CreateAgentRequest(workspaceId: "w1", kind: "pi", model: "openai/gpt-5.1", effort: "low"))
                == ["workspaceId": "w1", "kind": "pi", "model": "openai/gpt-5.1", "effort": "low"])
        #expect(try json(CreateAgentRequest(workspaceId: "w1", kind: "claude")) == ["workspaceId": "w1", "kind": "claude"],
                "defaults are left out, never sent as values")
    }
}

@MainActor
@Suite("AppStore new chat")
struct StoreNewChatTests {
    @Test func createsWithPicksAndFocusesTheComposer() async throws {
        let agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let backend = RecordingBackend(agent: agent, approval: Approval(agentId: "", question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        await store.refresh()

        let info = try #require(await store.kindControls("codex"))
        #expect(info.efforts(for: "m2").map(\.id) == ["max"])
        _ = await store.kindControls("codex")
        #expect(await backend.kindControlsFetches == 1, "cached per kind")

        #expect(await store.createAgent(workspaceId: "w2", kind: "codex", model: "m2", effort: "max"))
        let sent = try #require(await backend.created.last)
        #expect(sent.model == "m2" && sent.effort == "max" && sent.prompt == nil)
        #expect(store.selectedAgentId == agent.id)
        #expect(store.focusComposerFor == agent.id, "the new chat opens ready to type")
    }
}

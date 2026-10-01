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

    @Test func decodesKinds() throws {
        let kinds = try FixtureFiles.decode([KindStatus].self, "kinds.json")
        #expect(kinds.map(\.kind) == ["claude", "codex", "pi"])
        #expect(kinds.map(\.canStart) == [true, false, false])
        #expect(kinds[0].signInHint == nil, "no hint when it can start")
        #expect(kinds[1].signInHint == "Run `codex login` on this machine, then try again.")
        #expect(kinds[2].installed == false && kinds[2].signInHint?.contains("isn't installed") == true)
    }

    @Test func only409sForKindsAreNotReady() {
        let hint = "Run `codex login` on this machine, then try again."
        #expect(RelayError.http(status: 409, code: "not_signed_in", message: hint).kindNotReady == hint)
        #expect(RelayError.http(status: 409, code: "not_installed", message: nil).kindNotReady != nil, "a fallback when the message is missing")
        #expect(RelayError.http(status: 409, code: "conflict", message: "x").kindNotReady == nil)
        #expect(RelayError.http(status: 400, code: "not_signed_in", message: "x").kindNotReady == nil)
        #expect(RelayError.unauthorized.kindNotReady == nil)
    }

    @Test func picksAKindThatCanStart() {
        let statuses = [
            KindStatus(kind: "claude", installed: true, signedIn: false, signInHint: "h"),
            KindStatus(kind: "codex", installed: true, signedIn: true),
            KindStatus(kind: "pi", installed: false, signedIn: false, signInHint: "h"),
        ]
        #expect(NewChatSheet.pickKind("claude", statuses: statuses) == "codex", "signed out moves to the first that can start")
        #expect(NewChatSheet.pickKind("pi", statuses: statuses) == "codex")
        #expect(NewChatSheet.pickKind("codex", statuses: statuses) == "codex")
        #expect(NewChatSheet.pickKind("claude", statuses: nil) == "claude", "unknown (older bridge, offline) keeps the pick")
        let none = statuses.map { KindStatus(kind: $0.kind, installed: true, signedIn: false, signInHint: "h") }
        #expect(NewChatSheet.pickKind("codex", statuses: none) == "codex", "nothing can start: keep it so its hint shows")
        #expect(NewChatSheet.pickKind("claude", statuses: [KindStatus(kind: "codex", installed: true, signedIn: true)]) == "claude",
                "a kind the bridge didn't list counts as startable")
    }

    @Test func hintsShowCommandsAsCode() {
        let text = NewChatSheet.hintText("Run `codex login` on this machine, then try again.")
        #expect(String(text.characters) == "Run codex login on this machine, then try again.")
        #expect(text.runs.contains { $0.inlinePresentationIntent == .code })
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

        #expect(await store.createAgent(workspaceId: "w2", kind: "codex", model: "m2", effort: "max") == .created)
        let sent = try #require(await backend.created.last)
        #expect(sent.model == "m2" && sent.effort == "max" && sent.prompt == nil)
        #expect(store.selectedAgentId == agent.id)
        #expect(store.focusComposerFor == agent.id, "the new chat opens ready to type")
    }
}

@MainActor
@Suite("AppStore agent kinds")
struct StoreKindsTests {
    private func store() async throws -> (AppStore, RecordingBackend, String) {
        let agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let backend = RecordingBackend(agent: agent, approval: Approval(agentId: "", question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        await store.refresh()
        return (store, backend, try #require(store.machines.first?.id))
    }

    @Test func fetchesKindsFreshEachTime() async throws {
        let (store, backend, machine) = try await store()
        let fixture = try FixtureFiles.decode([KindStatus].self, "kinds.json")
        await backend.setKindStatuses(fixture)
        #expect(await store.kinds(machineId: machine) == fixture)
        _ = await store.kinds(machineId: machine)
        #expect(await backend.kindsFetches == 2, "never cached by the app; the bridge re-derives it")
        #expect(await store.kinds(machineId: "nope") == nil)
    }

    @Test func olderBridgeWithoutKindsIsUnknown() async throws {
        let (store, _, machine) = try await store()
        #expect(await store.kinds(machineId: machine) == nil)
        #expect(store.errorMessage == nil, "a missing /kinds isn't an error")
    }

    @Test func refusedCreateIsNotReadyNotABanner() async throws {
        let (store, backend, _) = try await store()
        let hint = "Run `codex login` on this machine, then try again."
        await backend.setCreateError(.http(status: 409, code: "not_signed_in", message: hint))
        let before = store.selectedAgentId
        #expect(await store.createAgent(workspaceId: "w2", kind: "codex", model: nil, effort: nil) == .kindNotReady(hint))
        #expect(store.errorMessage == nil, "shown as the sheet's alert, not the banner")
        #expect(store.selectedAgentId == before, "no chat opened")

        await backend.setCreateError(.http(status: 409, code: "not_installed", message: "`pi` isn't installed on this machine."))
        #expect(await store.createAgent(workspaceId: "w2", kind: "pi", model: nil, effort: nil) == .kindNotReady("`pi` isn't installed on this machine."))

        await backend.setCreateError(.http(status: 500, code: "internal", message: "boom"))
        #expect(await store.createAgent(workspaceId: "w2", kind: "claude", model: nil, effort: nil) == .failed)
        #expect(store.errorMessage == "boom", "other failures still use the banner")
    }
}

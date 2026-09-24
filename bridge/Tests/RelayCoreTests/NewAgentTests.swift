import Foundation
import Synchronization
import Testing
@testable import RelayCore

@Suite(.serialized) struct NewAgentTests {
    static func catalogs(dir: URL) throws -> ModelCatalogs {
        try Data(#"{"model":"sonnet","effortLevel":"high"}"#.utf8).write(to: dir.appendingPathComponent("claude.json"))
        try Data("model = \"gpt-5.6-terra\"\nmodel_reasoning_effort = \"xhigh\"\n\n[projects.\"/x\"]\nmodel = \"ignored\"\n".utf8)
            .write(to: dir.appendingPathComponent("config.toml"))
        try Data(#"{"defaultProvider":"openai-codex","defaultModel":"gpt-5.6-sol","defaultThinkingLevel":"high"}"#.utf8)
            .write(to: dir.appendingPathComponent("pi.json"))
        let piList = try Fixture.data("pi-list-models.txt")
        let codexJSON = try Fixture.data("codex-models.json")
        return ModelCatalogs(run: { args in args.first == "pi" ? piList : codexJSON },
                             piSettingsURL: dir.appendingPathComponent("pi.json"),
                             piModelsStoreURL: Fixture.url("pi-models-store.json"),
                             codexSessions: dir.appendingPathComponent("sessions"),
                             claudeSettingsURL: dir.appendingPathComponent("claude.json"),
                             codexConfigURL: dir.appendingPathComponent("config.toml"))
    }

    func withService(_ body: (AgentService, FakeHerdr) async throws -> Void) async throws {
        let dir = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("relay-new-\(UUID().uuidString.prefix(8))")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let fake = try FakeHerdr { method, params in
            let pane = { (id: String, cwd: String) -> [String: Any] in
                ["pane_id": id, "workspace_id": "w14", "tab_id": "w14:t1", "terminal_id": "t", "focused": false, "agent_status": "unknown", "revision": 1, "cwd": cwd]
            }
            switch method {
            case "pane.list":
                return ["type": "pane_list", "panes": [pane("w14:p1", "/Users/dev"), pane("w14:p2", "/Users/dev/e2e")]]
            case "tab.create":
                return ["type": "tab_created", "tab": ["tab_id": "w14:t9", "workspace_id": "w14"], "root_pane": pane("w14:p9", params["cwd"] as? String ?? "")]
            case "agent.start":
                return ["type": "agent_started", "argv": [] as [String], "agent": World.agent("w14:p9", name: params["name"] as? String, status: "idle", cwd: "/Users/dev/e2e", session: nil)]
            case "agent.get":
                var a = World.agent("w14:p9", name: "n", status: "idle", cwd: "/Users/dev/e2e", session: nil)
                a.removeValue(forKey: "agent")  // not classified yet
                return ["type": "agent_info", "agent": a]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.read":
                return ["type": "pane_read", "read": ["pane_id": "w14:p9", "workspace_id": "w14", "tab_id": "w14:t9", "source": "detection", "format": "text", "text": "", "revision": 1, "truncated": false]]
            default: return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }
        let service = AgentService(herdr: HerdrClient(socketPath: fake.socketPath),
                                   locator: TranscriptLocator(claudeProjects: dir, codex: CodexRollouts(root: dir.appendingPathComponent("sessions"))),
                                   catalogs: try Self.catalogs(dir: dir))
        try await body(service, fake)
    }

    @Test func launchFlags() {
        #expect(ClaudeDriver().launchArgs(model: "opus", effort: "low") == ["--model", "opus", "--effort", "low"])
        #expect(CodexDriver().launchArgs(model: "gpt-5.5", effort: "xhigh") == ["-m", "gpt-5.5", "-c", #"model_reasoning_effort="xhigh""#])
        #expect(PiDriver().launchArgs(model: "openai-codex/gpt-5.5", effort: "low") == ["--model", "openai-codex/gpt-5.5:low"])
        #expect(PiDriver().launchArgs(model: nil, effort: "high") == ["--thinking", "high"])
        #expect(ClaudeDriver().agentModel(for: "opus") == "claude-opus-5-5")
    }

    @Test func codexConfigTopLevelOnly() {
        let c = ModelCatalogs.parseCodexConfig("model = \"gpt-5.5\"\nmodel_reasoning_effort = 'low'\n[profiles.x]\nmodel = \"other\"\n")
        #expect(c.model == "gpt-5.5")
        #expect(c.effort == "low")
    }

    @Test func kindControlsWithDefaults() async throws {
        try await withService { service, _ in
            let claude = try service.kindControls("claude")
            #expect(claude.defaultModel == "sonnet")
            #expect(claude.defaultEffort == "high")
            #expect(claude.effortsByModel == nil)
            let codex = try service.kindControls("codex")
            #expect(codex.defaultModel == "gpt-5.6-terra")
            #expect(codex.defaultEffort == "xhigh")
            #expect(codex.effortsByModel?["gpt-5.5"]?.map(\.id) == ["low", "medium", "high", "xhigh"])
            #expect(codex.efforts.map(\.id) == ["low", "medium", "high", "xhigh", "max", "ultra"])
            let pi = try service.kindControls("pi")
            #expect(pi.defaultModel == "openai-codex/gpt-5.6-sol")
            #expect(pi.models.first?.id == "openai-codex/gpt-5.6-sol")
            #expect(pi.effortsByModel?["opencode-go/plain-model"]?.map(\.id) == ["off"])
            #expect(throws: APIError.self) { try service.kindControls("gemini") }
        }
    }

    @Test func createPassesFlagsAndReflectsChoice() async throws {
        try await withService { service, fake in
            let a = try await service.create(workspaceId: "w14", kind: "claude", name: "c1", prompt: nil,
                                             model: "opus", effort: "low", cwdFromPane: "w14:p2")
            #expect(a.model == "claude-opus-5-5")
            #expect(a.modelLabel == "Opus 5.5")
            #expect(a.effort == "low")
            #expect(a.kind == "claude")
            #expect(fake.params(of: "agent.start")?.contains(#""args":["--model","opus","--effort","low"]"#) == true)
            #expect(fake.params(of: "tab.create")?.contains(#""cwd":"/Users/dev/e2e""#) == true)
        }
    }

    @Test func justCreatedAgentStaysPendingUntilHerdrDetectsIt() async throws {
        try await withService { service, fake in
            _ = try await service.create(workspaceId: "w14", kind: "claude", name: "c3", prompt: nil)
            // herdr still reports no kind a moment later (the fake never classifies it).
            let a = try await service.agent(id: "w14:p9")
            #expect(a.kind == "claude")
            #expect(a.transcriptState == .pending)
            let page = try await service.messages(id: "w14:p9", before: nil, limit: 50)
            #expect(page.messages.isEmpty)
            #expect(fake.params(of: "agent.read")?.contains(#""source":"recent""#) != true)
        }
    }

    @Test func createWithoutChoicesPassesNoArgs() async throws {
        try await withService { service, fake in
            _ = try await service.create(workspaceId: "w14", kind: "codex", name: "c2", prompt: nil)
            #expect(fake.params(of: "agent.start")?.contains("args") == false)
            #expect(fake.params(of: "tab.create")?.contains(#""cwd":"/Users/dev""#) == true)  // first pane
        }
    }

    @Test func createValidatesBeforeOpeningATab() async throws {
        try await withService { service, fake in
            for (kind, model, effort) in [("pi", "anthropic/claude-fable-5", "off"), ("claude", "gpt-9", nil), ("codex", "gpt-5.5", "ultra"), ("gemini", "x", nil)] {
                await #expect(throws: APIError.self) {
                    _ = try await service.create(workspaceId: "w14", kind: kind, name: nil, prompt: nil, model: model, effort: effort)
                }
            }
            #expect(!fake.methods().contains("tab.create"))
            await #expect(throws: APIError.self) {
                _ = try await service.create(workspaceId: "w14", kind: "pi", name: nil, prompt: nil, cwdFromPane: "w9:p9")
            }
        }
    }

    @Test func codexFallbackIgnoresOlderRollouts() throws {
        let root = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("relay-codex-\(UUID().uuidString.prefix(8))")
        defer { try? FileManager.default.removeItem(at: root) }
        let day = root.appendingPathComponent("2026/09/25")
        try FileManager.default.createDirectory(at: day, withIntermediateDirectories: true)
        let old = day.appendingPathComponent("rollout-2026-09-25T10-00-00-aaaa.jsonl")
        try Data(#"{"type":"session_meta","payload":{"cwd":"/Users/dev/e2e"}}"#.utf8).write(to: old)
        try FileManager.default.setAttributes([.creationDate: Date().addingTimeInterval(-3600)], ofItemAtPath: old.path)
        let rollouts = CodexRollouts(root: root)
        #expect(rollouts.newest(cwd: "/Users/dev/e2e") == old)
        // Another agent's older rollout in the same folder isn't borrowed by a new agent.
        #expect(rollouts.newest(cwd: "/Users/dev/e2e", notBefore: Date().addingTimeInterval(-60)) == nil)
    }

    final class Counter: Sendable {
        let state = Mutex((active: 0, max: 0))
        func enter() { state.withLock { $0.active += 1; $0.max = max($0.max, $0.active) } }
        func leave() { state.withLock { $0.active -= 1 } }
    }

    @Test func settingsLockRunsOneAtATime() async throws {
        let lock = SerialLock()
        let counter = Counter()
        try await withThrowingTaskGroup(of: Void.self) { group in
            for _ in 0..<5 {
                group.addTask {
                    try await lock.run {
                        counter.enter()
                        try await Task.sleep(for: .milliseconds(20))
                        counter.leave()
                    }
                }
            }
            try await group.waitForAll()
        }
        #expect(counter.state.withLock { $0.max } == 1)
    }
}

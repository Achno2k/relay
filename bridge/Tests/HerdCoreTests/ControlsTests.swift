import Foundation
import Hummingbird
import HummingbirdTesting
import Synchronization
import Testing
@testable import HerdCore

/// A fake Claude Code behind a fake herdr: slash commands print `Set … to …`, Shift+Tab cycles the
/// footer mode, `/model` and `/effort` rewrite a settings file like the real one does.
@Suite(.serialized) struct ControlsTests {
    final class Claude: Sendable {
        struct State {
            var kind = "claude"
            var status = "done"
            var mode = "auto"
            var cycle = ["auto", "manual", "acceptEdits", "plan"]
            var session = "s1"
            var history: [String] = []
            var shiftTabs = 0
            /// Ask "Switch model?" before switching, like Claude does on a long cached conversation.
            var confirmSwitch = false
            var dialog: [String]?
            var pendingModel: String?
        }
        let state = Mutex(State())
        let settings: URL

        init() {
            settings = URL(fileURLWithPath: "/tmp").appendingPathComponent("herd-ctl-settings-\(UUID().uuidString.prefix(8)).json")
            FileManager.default.createFile(atPath: settings.path, contents: Data("{\"model\":\"opus\"}\n".utf8))
        }

        var screen: String {
            state.withLock { s in
                let footer = switch s.mode {
                case "auto": "⏵⏵ auto mode on (shift+tab to cycle)"
                case "manual": "⏸ manual mode on"
                case "acceptEdits": "⏵⏵ accept edits on (shift+tab to cycle)"
                default: "⏸ plan mode on (shift+tab to cycle)"
                }
                let rule = String(repeating: "─", count: 40)
                if let dialog = s.dialog { return (s.history.suffix(10) + dialog).joined(separator: "\n") }
                return (s.history.suffix(10) + [rule, "❯", rule, "  0/1.0M", "  \(footer) · ← 1 agent"]).joined(separator: "\n")
            }
        }

        func run(_ command: String) {
            state.withLock { s in
                s.history.append("❯ \(command)")
                let parts = command.split(separator: " ").map(String.init)
                switch parts[0] {
                case "/model" where s.confirmSwitch:
                    let label = ClaudeControls.catalog.models.first { $0.id == parts[1] }!.label
                    s.pendingModel = label
                    s.dialog = [String(repeating: "▔", count: 40), "   Switch model?", "   Your next response will be slower",
                                "   ❯ 1. Yes, switch to \(label)", "     2. No, go back"]
                case "/model":
                    let label = ClaudeControls.catalog.models.first { $0.id == parts[1] }!.label
                    s.history.append("  ⎿  Set model to \(label) and saved as your default for new sessions")
                    try? Data("{\"model\":\"\(parts[1])\"}\n".utf8).write(to: settings)
                case "/effort":
                    s.history.append("  ⎿  Set effort level to \(parts[1]) (saved as your default for new sessions): …")
                    try? Data("{\"model\":\"opus\",\"effortLevel\":\"\(parts[1])\"}\n".utf8).write(to: settings)
                case "/compact":
                    s.history.append("  ⎿  Compacted (ctrl+o to see full summary)")
                case "/clear":
                    s.session = "s\(Int(s.session.dropFirst())! + 1)"
                    s.history = ["▝▜██████▀  Sonnet 5 with medium effort · Claude Max"]
                default:
                    break
                }
            }
        }
    }

    func withService(_ claude: Claude, _ body: (AgentService, FakeHerdr) async throws -> Void) async throws {
        let fake = try FakeHerdr { method, params in
            switch method {
            case "agent.get":
                let s = claude.state.withLock { $0 }
                var a = World.agent("w14:p2", name: "e2e", status: s.status, cwd: "/Users/dev/e2e",
                                    session: ["source": "herdr:\(s.kind)", "agent": s.kind, "kind": "id", "value": s.session])
                a["agent"] = s.kind
                return ["type": "agent_info", "agent": a]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.read":
                return ["type": "pane_read", "read": ["pane_id": "w14:p2", "workspace_id": "w14", "tab_id": "w14:t1", "source": "detection", "format": "text", "text": claude.screen, "revision": 1, "truncated": false]]
            case "agent.prompt":
                claude.run(params["text"] as? String ?? "")
                return ["type": "agent_prompted", "agent": [:] as [String: Any]]
            case "agent.send_keys":
                if (params["keys"] as? [String]) == ["1"] {
                    claude.state.withLock { s in
                        guard s.dialog != nil, let label = s.pendingModel else { return }
                        s.dialog = nil
                        s.history.append("  ⎿  Set model to \(label) and saved as your default for new sessions")
                    }
                }
                if (params["keys"] as? [String]) == ["2"] {
                    claude.state.withLock { s in
                        guard s.dialog != nil else { return }
                        s.dialog = nil
                        s.history.append("  ⎿  Kept model as Opus 5.5")
                    }
                }
                if (params["keys"] as? [String]) == ["esc"] {
                    claude.state.withLock { $0.dialog = nil }
                }
                if (params["keys"] as? [String]) == ["shift+tab"] {
                    claude.state.withLock { s in
                        s.shiftTabs += 1
                        s.mode = s.cycle[(s.cycle.firstIndex(of: s.mode)! + 1) % s.cycle.count]
                    }
                }
                return ["type": "ok"]
            default:
                return FakeError(code: "unknown_method", message: method)
            }
        }
        defer {
            fake.stop()
            try? FileManager.default.removeItem(at: claude.settings)
        }
        let service = AgentService(herdr: HerdrClient(socketPath: fake.socketPath),
                                   locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))),
                                   settings: SettingsGuard(url: claude.settings))
        try await body(service, fake)
    }

    @Test func modelSwitchKeepsSavedDefault() async throws {
        let claude = Claude()
        try await withService(claude) { service, fake in
            let before = try Data(contentsOf: claude.settings)
            let a = try await service.control(id: "w14:p2", .model("sonnet"))
            #expect(a.model == "claude-sonnet-5")
            #expect(a.modelLabel == "Sonnet 5")
            #expect(fake.params(of: "agent.prompt") == #"{"target":"w14:p2","text":"/model sonnet"}"#)
            #expect(try Data(contentsOf: claude.settings) == before)
        }
    }

    @Test func answersSwitchModelConfirmation() async throws {
        let claude = Claude()
        claude.state.withLock { $0.confirmSwitch = true }
        try await withService(claude) { service, fake in
            let a = try await service.control(id: "w14:p2", .model("sonnet"))
            #expect(a.model == "claude-sonnet-5")
            #expect(fake.params(of: "agent.send_keys") == #"{"keys":["1"],"target":"w14:p2"}"#)
            #expect(claude.state.withLock { $0.dialog } == nil)
        }
    }

    @Test func realSwitchDialogIsDetected() throws {
        let screen = try Fixture.text("switch-model-dialog.txt")
        let d = try #require(AgentService.dialog(screen))
        #expect(d.question == "Switch model?")
        #expect(d.options.first == .init(label: "Yes, switch to Sonnet 5", keys: ["1"]))
        // A numbered list in the output during a redraw (no input box) is not a dialog.
        #expect(AgentService.dialog(try Fixture.text("redraw-with-list.txt")) == nil)
        // The normal input box is not a dialog.
        #expect(AgentService.dialog(try Fixture.text("input-empty.txt")) == nil)
    }

    @Test func keptModelIsARefusal() {
        let screen = "❯ /model sonnet\n  ⎿  Kept model as Opus 5.5\n───\n❯\n───"
        #expect(AgentService.kept(screen, after: "/model sonnet") == "Opus 5.5")
        #expect(AgentService.kept("<local-command-stdout>Kept model as `Opus 5.5`</local-command-stdout>", after: "/model sonnet") == "Opus 5.5")
        // An old "Kept" line before this command doesn't count.
        #expect(AgentService.kept("  ⎿  Kept model as Opus 5.5\n❯ /model sonnet\n  ⎿  Set model to Sonnet 5", after: "/model sonnet") == nil)
    }

    @Test func effort() async throws {
        let claude = Claude()
        try await withService(claude) { service, _ in
            let a = try await service.control(id: "w14:p2", .effort("high"))
            #expect(a.effort == "high")
            #expect(try String(contentsOf: claude.settings, encoding: .utf8) == "{\"model\":\"opus\"}\n")
        }
    }

    @Test func modeCyclesWithShiftTab() async throws {
        let claude = Claude()
        try await withService(claude) { service, _ in
            let a = try await service.control(id: "w14:p2", .permissionMode("plan"))
            #expect(a.permissionMode == "plan")
            #expect(claude.state.withLock { $0.shiftTabs } == 3)
            let d = try await service.control(id: "w14:p2", .permissionMode("default"))
            #expect(d.permissionMode == "default")
            // Already there: no key presses.
            let presses = claude.state.withLock { $0.shiftTabs }
            _ = try await service.control(id: "w14:p2", .permissionMode("default"))
            #expect(claude.state.withLock { $0.shiftTabs } == presses)
        }
    }

    @Test func modeOutsideTheCycleIsUnsupported() async throws {
        let claude = Claude()
        try await withService(claude) { service, _ in
            await #expect(throws: APIError.self) { try await service.control(id: "w14:p2", .permissionMode("bypassPermissions")) }
            do {
                _ = try await service.control(id: "w14:p2", .permissionMode("bypassPermissions"))
            } catch let e as APIError {
                #expect(e.code == "unsupported")
                #expect(e.status == .badRequest)
            }
            // One full cycle each time, ending where it started.
            #expect(claude.state.withLock { $0.mode } == "auto")
            #expect(claude.state.withLock { $0.shiftTabs } == 8)
        }
    }

    @Test func clearFollowsTheNewSession() async throws {
        let claude = Claude()
        try await withService(claude) { service, _ in
            let a = try await service.control(id: "w14:p2", .clear)
            #expect(a.sessionId == "s2")
            // The fresh session's banner fills model and effort before any reply.
            #expect(a.model == "claude-sonnet-5")
            #expect(a.effort == "medium")
        }
    }

    @Test func compact() async throws {
        let claude = Claude()
        try await withService(claude) { service, fake in
            _ = try await service.control(id: "w14:p2", .compact)
            #expect(fake.params(of: "agent.prompt") == #"{"target":"w14:p2","text":"/compact"}"#)
        }
    }

    @Test(arguments: [("working", "agent_busy"), ("blocked", "agent_blocked")])
    func refusesWhileBusy(status: String, code: String) async throws {
        let claude = Claude()
        claude.state.withLock { $0.status = status }
        try await withService(claude) { service, fake in
            do {
                _ = try await service.control(id: "w14:p2", .model("opus"))
                Issue.record("expected \(code)")
            } catch let e as APIError {
                #expect(e.code == code)
                #expect(e.status == .conflict)
            }
            #expect(!fake.methods().contains("agent.prompt"))
        }
    }

    @Test func onlyClaude() async throws {
        let claude = Claude()
        claude.state.withLock { $0.kind = "gemini" }
        try await withService(claude) { service, _ in
            do {
                _ = try await service.control(id: "w14:p2", .compact)
                Issue.record("expected unsupported")
            } catch let e as APIError {
                #expect(e.code == "unsupported")
            }
            #expect(try await service.agent(id: "w14:p2").model == nil)
        }
    }

    @Test func routes() async throws {
        let claude = Claude()
        try await withService(claude) { service, _ in
            let router = HerdRoutes.router(service: service, hub: EventHub(), token: "t")
            let auth: HTTPFields = [.authorization: "Bearer t"]
            try await Application(router: router).test(.router) { client in
                try await client.execute(uri: "/controls", method: .get, headers: auth) { r throws in
                    #expect(try JSONDecoder().decode(ControlsCatalog.self, from: Data(buffer: r.body)) == ClaudeControls.catalog)
                }
                for bad in [#"{}"#, #"{"model":"opus","effort":"high"}"#, #"{"command":"nuke"}"#, #"{"model":"gpt"}"#] {
                    try await client.execute(uri: "/agents/w14%3Ap2/control", method: .post, headers: auth, body: ByteBuffer(string: bad)) { r throws in
                        #expect(r.status == .badRequest, "\(bad)")
                    }
                }
                try await client.execute(uri: "/agents/w14%3Ap2/control", method: .post, headers: auth, body: ByteBuffer(string: #"{"permissionMode":"plan"}"#)) { r throws in
                    #expect(r.status == .accepted)
                    #expect(try JSONDecoder().decode(Agent.self, from: Data(buffer: r.body)).permissionMode == "plan")
                }
            }
        }
    }
}

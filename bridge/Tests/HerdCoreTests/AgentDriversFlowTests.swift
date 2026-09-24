import Foundation
import Synchronization
import Testing
@testable import HerdCore

/// Fake pi and codex TUIs behind a fake herdr, to drive the real drivers end to end.
@Suite(.serialized) struct AgentDriversFlowTests {
    final class TUI: Sendable {
        struct State {
            var kind: String
            var session = "s1"
            var status = "idle"
            // pi
            var piModel = "openai-codex/gpt-5.6-sol"
            var piThinking = "high"
            var piRejects: [String] = []
            // codex
            var model = "gpt-5.6-terra"
            var effort = "high"
            var picker: (title: String, labels: [String], cursor: Int)?
            var pickedModel: String?
            var savedDefault = false
            var lines: [String] = []
        }
        let state: Mutex<State>
        static let codexModels: [(slug: String, name: String, efforts: [String])] = [
            ("gpt-6-luna", "GPT-6-Luna", ["low", "medium", "high"]),
            ("gpt-5.6-terra", "GPT-5.6-Terra", ["low", "medium", "high", "xhigh", "max", "ultra"]),
            ("gpt-5.5", "GPT-5.5", ["low", "medium", "high", "xhigh"]),
        ]

        init(kind: String) { state = Mutex(State(kind: kind)) }

        var screen: String {
            state.withLock { s in
                if s.kind == "pi" {
                    let (p, m) = (s.piModel.split(separator: "/").first!, s.piModel.split(separator: "/").last!)
                    return (s.lines.suffix(6) + ["────", "────", "/Users/dev/p (main)", "$0.000 (sub) 0.0%/272k (auto)     (\(p)) \(m) • \(s.piThinking)"]).joined(separator: "\n")
                }
                var out = s.lines.suffix(6)
                if let p = s.picker {
                    out.append("  \(p.title)")
                    for (i, l) in p.labels.enumerated() { out.append("\(i == p.cursor ? "›" : " ") \(i + 1). \(l)  description") }
                    out.append("  enter default · s session · esc back")
                } else {
                    let name = Self.codexModels.first { $0.slug == s.model }?.name ?? s.model
                    out += ["› Ask Codex to do anything", "  \(name) \(s.effort) · /Users/dev/p  ⚠ 3 warnings"]
                }
                return out.joined(separator: "\n")
            }
        }

        static func effortLabel(_ e: String) -> String { Effort.labels[e] ?? e }

        func prompt(_ text: String) {
            state.withLock { s in
                let parts = text.split(separator: " ").map(String.init)
                switch (s.kind, parts[0]) {
                case ("pi", "/model"): s.piModel = parts[1]; s.lines.append(" Model: \(parts[1])")
                case ("pi", "/thinking") where s.piRejects.contains(parts[1]):
                    s.lines.append(#" Error: Unknown thinking level "\#(parts[1])". Available levels: minimal, low, medium, high, xhigh, max."#)
                case ("pi", "/thinking"): s.piThinking = parts[1]; s.lines.append(" Thinking level: \(parts[1])")
                case ("pi", "/new"): s.session = "s\(Int(s.session.dropFirst())! + 1)"
                case ("codex", "/model"):
                    let cur = Self.codexModels.firstIndex { $0.slug == s.model } ?? 0
                    s.picker = ("Select Model and Effort", Self.codexModels.map(\.name), cur)
                case ("codex", "/permissions"):
                    s.picker = ("Update Model Permissions", ["Ask for approval (current)", "Approve for me", "Full Access"], 0)
                default: break
                }
            }
        }

        func key(_ k: String) {
            state.withLock { s in
                guard var p = s.picker else { return }
                switch k {
                case "down": p.cursor = (p.cursor + 1) % p.labels.count; s.picker = p
                case "up": p.cursor = (p.cursor + p.labels.count - 1) % p.labels.count; s.picker = p
                case "esc": s.picker = nil
                case "enter", "s":
                    let label = Picker.cleanLabel(p.labels[p.cursor])
                    if p.title == "Select Model and Effort" {
                        let m = Self.codexModels.first { $0.name == label }!
                        s.pickedModel = m.slug
                        let direct = m.efforts.filter { !["max", "ultra"].contains($0) }.map(Self.effortLabel)
                        let more = m.efforts.contains("max") ? ["More reasoning…"] : []
                        s.picker = ("Select Reasoning Level for \(m.name)", direct + more, 1)
                    } else if p.title.hasPrefix("Select Reasoning Level") || p.title == "Advanced Reasoning" {
                        if label.hasPrefix("More reasoning") {
                            let m = Self.codexModels.first { $0.slug == s.pickedModel }!
                            s.picker = ("Advanced Reasoning", m.efforts.filter { ["max", "ultra"].contains($0) }.map(Self.effortLabel), 0)
                            return
                        }
                        let effort = Effort.labels.first { $0.value == label }!.key
                        s.model = s.pickedModel!
                        s.effort = effort
                        s.picker = nil
                        if k == "enter" { s.savedDefault = true; s.lines.append("• Model changed to \(s.model) \(effort)") }
                        else { s.lines.append("• Model changed to \(s.model) \(effort) for this session only") }
                    } else if p.title == "Update Model Permissions", k == "enter" {
                        s.picker = nil
                        s.lines.append("• Permission selection requested: \(label.replacingOccurrences(of: " (current)", with: ""))")
                    }
                default: break
                }
            }
        }
    }

    func withService(_ tui: TUI, _ body: (AgentService) async throws -> Void) async throws {
        let fake = try FakeHerdr { method, params in
            switch method {
            case "agent.get":
                let s = tui.state.withLock { $0 }
                var a = World.agent("w14:p9", name: "t", status: s.status, cwd: "/Users/dev/p",
                                    session: ["source": "herdr:\(s.kind)", "agent": s.kind, "kind": s.kind == "pi" ? "path" : "id", "value": s.session])
                a["agent"] = s.kind
                return ["type": "agent_info", "agent": a]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.read":
                return ["type": "pane_read", "read": ["pane_id": "w14:p9", "workspace_id": "w14", "tab_id": "w14:t9", "source": "detection", "format": "text", "text": tui.screen, "revision": 1, "truncated": false]]
            case "agent.prompt":
                tui.prompt(params["text"] as? String ?? "")
                return ["type": "agent_prompted", "agent": [:] as [String: Any]]
            case "agent.send_keys":
                for k in params["keys"] as? [String] ?? [] { tui.key(k) }
                return ["type": "ok"]
            default:
                return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }
        let piList = try Fixture.data("pi-list-models.txt")
        let codexJSON = try Fixture.data("codex-models.json")
        let catalogs = ModelCatalogs(run: { args in args.first == "pi" ? piList : codexJSON },
                                     piSettingsURL: URL(fileURLWithPath: "/nonexistent"),
                                     piModelsStoreURL: Fixture.url("pi-models-store.json"),
                                     codexSessions: URL(fileURLWithPath: "/nonexistent"))
        try await body(AgentService(herdr: HerdrClient(socketPath: fake.socketPath),
                                    locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent")),
                                    catalogs: catalogs))
    }

    func expectError(_ code: String, _ op: () async throws -> Void) async {
        do {
            try await op()
            Issue.record("expected \(code)")
        } catch let e as APIError {
            #expect(e.code == code, "\(e.message)")
        } catch {
            Issue.record("unexpected \(error)")
        }
    }

    @Test func piModelAndThinking() async throws {
        let tui = TUI(kind: "pi")
        try await withService(tui) { service in
            let before = try await service.agent(id: "w14:p9")
            #expect(before.model == "openai-codex/gpt-5.6-sol")
            #expect(before.modelLabel == "gpt-5.6-sol")
            #expect(before.effort == "high")
            let caps = try await service.controls(id: "w14:p9")
            #expect(caps.supports == .init(model: true, effort: true, mode: false, compact: true, clear: true))
            #expect(caps.efforts.map(\.id) == PiDriver.efforts)
            let a = try await service.control(id: "w14:p9", .model("anthropic/claude-sonnet-5"))
            #expect(a.model == "anthropic/claude-sonnet-5")
            let b = try await service.control(id: "w14:p9", .effort("minimal"))
            #expect(b.effort == "minimal")
            let c = try await service.control(id: "w14:p9", .clear)
            #expect(c.sessionId == "s2")
            await expectError("bad_request") { _ = try await service.control(id: "w14:p9", .model("nope/x")) }
            await expectError("unsupported") { _ = try await service.control(id: "w14:p9", .permissionMode("plan")) }
        }
    }

    @Test func piEffortsFollowTheModel() async throws {
        let tui = TUI(kind: "pi")
        tui.state.withLock { $0.piModel = "anthropic/claude-fable-5" }
        try await withService(tui) { service in
            let caps = try await service.controls(id: "w14:p9")
            #expect(caps.efforts.map(\.id) == ["minimal", "low", "medium", "high", "xhigh", "max"])
            // "off" isn't offered, and asking anyway is a clear 400 before anything is typed.
            await expectError("bad_request") { _ = try await service.control(id: "w14:p9", .effort("off")) }
        }
    }

    @Test func piRejectionIsUnsupportedNotTimeout() async throws {
        let tui = TUI(kind: "pi")
        tui.state.withLock { s in
            s.piModel = "unlisted/model"  // not in models-store: bridge offers everything
            s.piRejects = ["off"]
        }
        try await withService(tui) { service in
            let clock = ContinuousClock()
            let start = clock.now
            await expectError("unsupported") { _ = try await service.control(id: "w14:p9", .effort("off")) }
            #expect(clock.now - start < .seconds(3))
            // Learned: now "off" isn't offered for that model.
            let learned = try await service.controls(id: "w14:p9")
            #expect(!learned.efforts.map(\.id).contains("off"))
        }
    }

    @Test func piModelWithoutThinkingOnlyOffersOff() async throws {
        let tui = TUI(kind: "pi")
        tui.state.withLock { $0.piModel = "opencode-go/plain-model" }
        try await withService(tui) { service in
            let caps = try await service.controls(id: "w14:p9")
            #expect(caps.efforts.map(\.id) == ["off"])
            #expect(!caps.supports.effort)
        }
    }

    @Test func codexModelPickerSessionOnly() async throws {
        let tui = TUI(kind: "codex")
        try await withService(tui) { service in
            let before = try await service.agent(id: "w14:p9")
            #expect(before.model == "gpt-5.6-terra")
            #expect(before.modelLabel == "GPT-5.6-Terra")
            let caps = try await service.controls(id: "w14:p9")
            #expect(caps.models.map(\.id) == ["gpt-6-luna", "gpt-5.6-terra", "gpt-5.5"])  // hidden model left out
            #expect(caps.efforts.map(\.id) == ["low", "medium", "high", "xhigh", "max", "ultra"])
            #expect(caps.modes.map(\.id) == ["ask", "approveForMe", "fullAccess"])

            let a = try await service.control(id: "w14:p9", .model("gpt-5.5"))
            #expect(a.model == "gpt-5.5")
            #expect(a.effort == "high")  // kept: gpt-5.5 supports it
            let b = try await service.control(id: "w14:p9", .model("gpt-5.6-terra"))
            #expect(b.model == "gpt-5.6-terra")
            let c = try await service.control(id: "w14:p9", .effort("ultra"))  // behind "More reasoning…"
            #expect(c.effort == "ultra")
            let d = try await service.control(id: "w14:p9", .model("gpt-6-luna"))
            #expect(d.effort == "medium")  // ultra unsupported there: its default
            #expect(!tui.state.withLock { $0.savedDefault })
            #expect(tui.state.withLock { $0.picker } == nil)
        }
    }

    @Test func codexModes() async throws {
        let tui = TUI(kind: "codex")
        try await withService(tui) { service in
            let a = try await service.control(id: "w14:p9", .permissionMode("fullAccess"))
            #expect(a.permissionMode == "fullAccess")
            let b = try await service.control(id: "w14:p9", .permissionMode("ask"))
            #expect(b.permissionMode == "ask")
        }
    }

    @Test func codexRefusals() async throws {
        let tui = TUI(kind: "codex")
        tui.state.withLock { $0.model = "gpt-5.6-sol" }  // configured, but not in the catalogue
        try await withService(tui) { service in
            let caps = try await service.controls(id: "w14:p9")
            #expect(caps.models.first == ControlChoice("gpt-5.6-sol", "gpt-5.6-sol"))
            #expect(!caps.supports.effort)
            await expectError("unsupported") { _ = try await service.control(id: "w14:p9", .effort("low")) }
            await expectError("unsupported") { _ = try await service.control(id: "w14:p9", .model("gpt-5.6-sol")) }
            await expectError("bad_request") { _ = try await service.control(id: "w14:p9", .model("gpt-hidden")) }
            #expect(tui.state.withLock { $0.picker } == nil)
        }
    }

    @Test func codexNoPickerLeftOpenOnFailure() async throws {
        let tui = TUI(kind: "codex")
        try await withService(tui) { service in
            // gpt-5.5 has no Max; asking for it after picking the model fails inside the picker.
            await expectError("unsupported") {
                try await CodexDriver().pickModel(
                    try await service.herdr.agent("w14:p9"),
                    .init(slug: "gpt-5.5", displayName: "GPT-5.5", visible: true, efforts: ["low"], defaultEffort: "low"),
                    effort: "max", service: service)
            }
            #expect(tui.state.withLock { $0.picker } == nil)
            #expect(!tui.state.withLock { $0.savedDefault })
        }
    }
}

import Foundation
import Testing
@testable import HerdCore

@Suite struct ClaudeControlsTests {
    @Test(arguments: [
        ("claude-opus-5-5", "Opus 5.5"),
        ("claude-sonnet-5", "Sonnet 5"),
        ("claude-haiku-4-5-20251001", "Haiku 4.5"),
        ("claude-fable-5-1", "Fable 5.1"),
        ("claude-opus-5-5[1m]", "Opus 5.5"),
    ])
    func labels(id: String, label: String) {
        #expect(ClaudeControls.label(forModel: id) == label)
    }

    @Test func labelsAndIdsRoundTrip() {
        for m in ClaudeControls.catalog.models {
            let id = ClaudeControls.id(forLabel: m.label)
            #expect(id?.contains(m.id) == true)
            #expect(id.flatMap(ClaudeControls.label(forModel:)) == m.label)
        }
        #expect(ClaudeControls.id(forLabel: "Opus 5.5 (1M context)") == "claude-opus-5-5")
        #expect(ClaudeControls.label(forModel: "<synthetic>") == nil)
        #expect(ClaudeControls.id(forLabel: "Default (recommended)") == nil)
    }

    @Test func scanTakesTheLatestValues() throws {
        let s = ClaudeControls.scan(try Fixture.data("controls-session.jsonl"))
        #expect(s == ControlState(model: "claude-sonnet-5", effort: "high", permissionMode: "plan"))
    }

    @Test func scanIgnoresSidechainsAndPartialLines() {
        let lines = #"{"type":"assistant","isSidechain":true,"effort":"low","message":{"model":"claude-haiku-4-5"}}"# + "\n" + #"{"type":"assist"#
        #expect(ClaudeControls.scan(Data(lines.utf8)) == ControlState())
        #expect(ClaudeControls.scan(Data(#"{"type":"permission-mode","permissionMode":"manual"}"#.utf8)).permissionMode == "default")
    }

    @Test(arguments: [
        ("⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent", "auto"),
        ("⏸ manual mode on · ← 1 agent", "default"),
        ("⏵⏵ accept edits on (shift+tab to cycle)", "acceptEdits"),
        ("⏸ plan mode on (shift+tab to cycle)", "plan"),
        ("⏵⏵ bypass permissions on (shift+tab to cycle)", "bypassPermissions"),
    ])
    func footer(line: String, mode: String) {
        #expect(ClaudeControls.footerMode("────\n❯\n────\n  0/1.0M\n  \(line)\n") == mode)
    }

    @Test func footerAndBanner() throws {
        let screen = try Fixture.text("footer-plan.txt")
        #expect(ClaudeControls.footerMode(screen) == "plan")
        #expect(ClaudeControls.banner(screen) == ControlState(model: "claude-sonnet-5", effort: "medium"))
        #expect(ClaudeControls.footerMode("no footer here") == nil)
        #expect(ClaudeControls.screenEffort("   ◐ medium · /effort\n───") == "medium")
    }

    @Test func mergeFillsGaps() {
        let new = ControlState(model: "claude-sonnet-5")
        #expect(new.merged(over: ControlState(model: "claude-opus-5-5", effort: "low", permissionMode: "auto"))
            == ControlState(model: "claude-sonnet-5", effort: "low", permissionMode: "auto"))
    }

    @Test func compactSummaryIsNotAUserBubble() throws {
        let ms = TranscriptParser.parse(try Fixture.data("controls-session.jsonl"), format: .claude, cwd: nil)
        #expect(ms.map(\.id) == ["u1", "a1"])
    }

    @Test func lastPromptLine() {
        #expect(InputBox.lastPromptLine("❯ /model sonnet\n  ⎿  Set model\n───\n❯\n───") == "")
        #expect(InputBox.lastPromptLine("───\n❯ /effort high\n───\n  /effort  Set effort level") == "/effort high")
    }

    @Test func catalogMatchesContractFixture() throws {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("docs/fixtures/controls.json")
        #expect(try JSONDecoder().decode(ControlsCatalog.self, from: Data(contentsOf: url)) == ClaudeControls.catalog)
    }

    @Test func settingsGuardRestoresExactBytes() throws {
        let url = URL(fileURLWithPath: "/tmp").appendingPathComponent("herd-settings-\(UUID().uuidString.prefix(8)).json")
        defer { try? FileManager.default.removeItem(at: url) }
        let original = Data("{\n  \"model\": \"opus\"\n}\n".utf8)
        FileManager.default.createFile(atPath: url.path, contents: original, attributes: [.posixPermissions: 0o600])
        let guardian = SettingsGuard(url: url)
        let snap = guardian.snapshot()
        try Data("{\"model\":\"sonnet\"}".utf8).write(to: url)
        #expect(guardian.restore(snap))
        #expect(try Data(contentsOf: url) == original)
        #expect((try FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] as? NSNumber)?.intValue == 0o600)
        #expect(!guardian.restore(snap))
    }
}

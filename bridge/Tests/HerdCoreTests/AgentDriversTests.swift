import Foundation
import Testing
@testable import HerdCore

@Suite struct AgentDriversTests {
    // MARK: pi

    @Test func piFooter() throws {
        #expect(PiDriver.footer(try Fixture.text("pi-footer.txt")) == ControlState(model: "openai-codex/gpt-5.6-terra", effort: "low"))
        #expect(PiDriver.footer("   (anthropic) claude-sonnet-5") == ControlState(model: "anthropic/claude-sonnet-5", effort: nil))
        #expect(PiDriver.footer("nothing here") == nil)
    }

    @Test func piSessionFile() throws {
        #expect(PiDriver.scan(try Fixture.data("pi-controls-session.jsonl")) == ControlState(model: "anthropic/claude-sonnet-5", effort: "low"))
    }

    @Test func piListAndOrdering() throws {
        let models = ModelCatalogs.parsePiList(try Fixture.text("pi-list-models.txt"))
        #expect(models.count == 6)
        #expect(models.last == .init(provider: "opencode-go", id: "plain-model", thinking: false))
        let settings = ModelCatalogs.PiSettings(defaultModel: "openai-codex/gpt-5.6-sol", enabledModels: ["claude-sonnet-5", "opencode-go/*"])
        let ordered = PiDriver.ordered(models, settings: settings, current: "openai-codex/gpt-5.6-sol")
        #expect(ordered.map(\.id) == [
            "openai-codex/gpt-5.6-sol", "anthropic/claude-sonnet-5", "opencode-go/gpt-5.5", "opencode-go/plain-model",
            "anthropic/claude-haiku-4-5", "openai-codex/gpt-5.5",
        ])
        // Same model id under two providers gets the provider in its label.
        #expect(ordered.first { $0.id == "opencode-go/gpt-5.5" }?.label == "gpt-5.5 (opencode-go)")
        #expect(ordered.first { $0.id == "anthropic/claude-sonnet-5" }?.label == "claude-sonnet-5")
        // A current model pi doesn't list still shows up, first.
        #expect(PiDriver.ordered(models, settings: settings, current: "local/custom").first == ControlChoice("local/custom", "custom"))
    }

    @Test func piSettingsFile() throws {
        let url = URL(fileURLWithPath: "/tmp/herd-pi-settings-\(UUID().uuidString.prefix(8)).json")
        defer { try? FileManager.default.removeItem(at: url) }
        try Data(#"{"defaultProvider":"openai-codex","defaultModel":"gpt-5.6-sol","defaultThinkingLevel":"high","enabledModels":["sonnet"]}"#.utf8).write(to: url)
        let s = ModelCatalogs(run: { _ in nil }, piSettingsURL: url).piSettings()
        #expect(s == .init(defaultModel: "openai-codex/gpt-5.6-sol", defaultThinking: "high", enabledModels: ["sonnet"]))
    }

    // MARK: codex

    @Test func codexFooter() throws {
        let f = try #require(CodexDriver.footer(try Fixture.text("codex-footer.txt")))
        #expect(f.model == "GPT-5.6-Terra")
        #expect(f.effort == "high")
        #expect(CodexDriver.footer("  gpt-5.6-sol high · /x  ⚠ 1 warning")?.model == "gpt-5.6-sol")
    }

    @Test func codexCatalogAndSlugs() throws {
        let cat = ModelCatalogs.parseCodexCatalog(try Fixture.data("codex-models.json"))
        #expect(cat.map(\.slug) == ["gpt-hidden", "gpt-6-luna", "gpt-5.6-terra", "gpt-5.5"])  // by priority
        #expect(cat.first { $0.slug == "gpt-hidden" }?.visible == false)
        #expect(CodexDriver.slug(for: "GPT-5.6-Terra", in: cat) == "gpt-5.6-terra")
        #expect(CodexDriver.slug(for: "gpt-5.6-sol", in: cat) == "gpt-5.6-sol")
    }

    @Test func codexTurnContext() throws {
        let ctx = try #require(CodexDriver.lastTurnContext(try Fixture.data("codex-rollout.jsonl")))
        #expect(ctx.model == "gpt-5.6-terra")
        #expect(ctx.effort == "high")
        #expect(ctx.mode == "approveForMe")
        #expect(ctx.at == Timestamps.parse("2026-09-24T13:20:05.000Z"))
        let full = #"{"timestamp":"2026-09-24T13:21:00Z","type":"turn_context","payload":{"model":"m","effort":"low","approvals_reviewer":"user","sandbox_policy":{"type":"danger-full-access"}}}"#
        #expect(CodexDriver.lastTurnContext(Data(full.utf8))?.mode == "fullAccess")
    }

    @Test func pickerParsing() throws {
        let p = try #require(Picker.parse(try Fixture.text("codex-model-picker.txt")))
        #expect(p.title == "Select Model and Effort")
        #expect(p.labels == ["GPT-6-Luna", "GPT-5.6-Terra", "GPT-5.6-Luna", "GPT-5.5"])
        #expect(p.cursor == 1)
        let sub = try #require(Picker.parse("  Advanced Reasoning\n  ⚠ Consumes usage limits faster\n› 1. Max  For difficult problems · higher usage\n  enter default · s session"))
        #expect(sub.labels == ["Max"])
        #expect(AgentService.titleMatches(sub, ""))
        #expect(!AgentService.titleMatches(sub, "Reasoning Level"))
        // The input line isn't a picker.
        #expect(Picker.parse("› Ask Codex to do anything\n  gpt-5.5 high · /x") == nil)
    }

    @Test func effortLabels() {
        #expect(Effort.choices(["xhigh", "ultra", "off"]).map(\.label) == ["Extra high", "Ultra", "Off"])
    }

    @Test func contractFixturesDecode() throws {
        let docs = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("docs/fixtures")
        for kind in ["claude", "pi", "codex"] {
            let c = try JSONDecoder().decode(AgentControls.self, from: Data(contentsOf: docs.appendingPathComponent("agent-controls-\(kind).json")))
            #expect(!c.models.isEmpty)
        }
        let agents = try JSONDecoder().decode([Agent].self, from: Data(contentsOf: docs.appendingPathComponent("agents-multi.json")))
        #expect(agents.map(\.kind) == ["pi", "codex"])
    }
}

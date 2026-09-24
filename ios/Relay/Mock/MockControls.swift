#if DEBUG
import Foundation
import HerdKit

/// Synthetic per-kind controls for the mock backend (what `GET /agents/:id/controls` returns).
enum MockControls {
    /// Claude and codex come from `docs/fixtures/agent-controls-*.json`. pi uses a synthetic long list here
    /// (its fixture has 6 models) so the search sheet gets exercised.
    static func info(for kind: String, fixtures: Fixtures) -> AgentControlsInfo {
        switch kind {
        case "claude":
            return fixtures.agentControls["claude"] ?? AgentControlsInfo(claudeCatalog: fixtures.controls)
        case "codex":
            return fixtures.agentControls["codex"] ?? none
        case "pi":
            return AgentControlsInfo(
                models: piModels,
                efforts: fixtures.agentControls["pi"]?.efforts ?? [],
                modes: [],
                supports: .init(model: true, effort: true, mode: false, compact: true, clear: true)
            )
        default:
            return none
        }
    }

    private static let none = AgentControlsInfo(
        models: [], efforts: [], modes: [], supports: .init(model: false, effort: false, mode: false, compact: false, clear: false)
    )

    /// Long on purpose: pi lists every provider's models, with the user's scoped ones first.
    static let piModels: [ControlOption] = {
        let scoped = [
            ("anthropic/claude-sonnet-4-5", "Claude Sonnet 4.5"),
            ("openai/gpt-5.1", "GPT-5.1"),
            ("google/gemini-2.5-pro", "Gemini 2.5 Pro"),
        ]
        let rest = [
            ("anthropic/claude-opus-4-1", "Claude Opus 4.1"), ("anthropic/claude-haiku-4-5", "Claude Haiku 4.5"),
            ("openai/gpt-5.1-mini", "GPT-5.1 Mini"), ("openai/gpt-4.1", "GPT-4.1"), ("openai/o4-mini", "o4-mini"),
            ("google/gemini-2.5-flash", "Gemini 2.5 Flash"), ("google/gemini-2.5-flash-lite", "Gemini 2.5 Flash Lite"),
            ("xai/grok-4", "Grok 4"), ("xai/grok-4-fast", "Grok 4 Fast"), ("mistral/mistral-large", "Mistral Large"),
            ("mistral/codestral", "Codestral"), ("deepseek/deepseek-v3.2", "DeepSeek V3.2"), ("deepseek/deepseek-r1", "DeepSeek R1"),
            ("groq/llama-3.3-70b", "Llama 3.3 70B"), ("groq/qwen3-32b", "Qwen3 32B"), ("openrouter/kimi-k2", "Kimi K2"),
            ("openrouter/glm-4.6", "GLM 4.6"), ("cerebras/gpt-oss-120b", "gpt-oss 120B"), ("ollama/qwen2.5-coder", "Qwen2.5 Coder"),
            ("ollama/llama3.2", "Llama 3.2"),
        ]
        return (scoped + rest).map { ControlOption(id: $0.0, label: $0.1) }
    }()

    /// The codex agent in `agents.json` predates controls: give it the fields `agents-multi.json` shows.
    /// A pi agent is added to the website folder.
    static func extraAgents(_ agents: [Agent]) -> [Agent] {
        var agents = agents.map { agent -> Agent in
            guard agent.kind == "codex", agent.model == nil else { return agent }
            var a = agent
            a.model = "gpt-5.6-terra"
            a.modelLabel = "GPT-5.6-Terra"
            a.effort = "high"
            a.permissionMode = "approveForMe"
            return a
        }
        if let website = agents.first(where: { $0.workspaceId == "w2" }) {
            agents.append(Agent(
                id: "w2:p4", name: "docs", kind: "pi", title: "Tidy the docs folder",
                workspaceId: website.workspaceId, workspaceName: website.workspaceName, cwdName: website.cwdName,
                status: .idle, hasTranscript: true, updatedAt: Date().addingTimeInterval(-25 * 60),
                model: "anthropic/claude-sonnet-4-5", modelLabel: "Claude Sonnet 4.5", effort: "medium",
                sessionId: "pi-session-1"
            ))
        }
        return agents
    }
}
#endif

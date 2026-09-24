import RelayKit
import SwiftUI

/// Labels and symbols for model / mode / effort, taking in-flight changes into account.
enum ControlDisplay {
    /// The option in `models` that `agent.model` is. pi and codex report the exact id; Claude reports a
    /// full id ("claude-sonnet-5") that contains its alias ("sonnet").
    static func modelId(_ agent: Agent, models: [ControlOption]) -> String? {
        guard let model = agent.model else { return nil }
        if let exact = models.first(where: { $0.id == model }) { return exact.id }
        let lower = model.lowercased()
        return models.first { lower.contains($0.id.lowercased()) }?.id
    }

    static func label(_ id: String, in options: [ControlOption]?) -> String {
        options?.first { $0.id == id }?.label ?? id.prefix(1).uppercased() + id.dropFirst()
    }

    static func modeSymbol(_ mode: String?) -> String {
        switch mode {
        case "plan": "checklist"
        case "acceptEdits": "pencil.line"
        case "auto": "bolt.fill"
        case "approveForMe": "checkmark.shield"
        case "bypassPermissions", "fullAccess": "exclamationmark.shield"
        default: "hand.raised"
        }
    }


    /// "Couldn't switch to Sonnet 5" etc., in front of the bridge's error message.
    static func failurePrefix(_ request: ControlRequest, info: AgentControlsInfo?) -> String {
        switch request {
        case .model(let id): "Couldn't switch to \(label(id, in: info?.models))"
        case .permissionMode(let id): "Couldn't switch to \(label(id, in: info?.modes))"
        case .effort(let id): "Couldn't set effort to \(label(id, in: info?.efforts))"
        case .command(.compact): "Couldn't compact"
        case .command(.clear): "Couldn't clear"
        }
    }
}

/// What the chat header and menus show for one agent: server values overlaid with a pending change,
/// limited to what this agent's kind supports.
struct AgentControls {
    let agent: Agent
    /// From `GET /agents/:id/controls`; nil until loaded (or when the bridge offers nothing).
    let info: AgentControlsInfo?
    let pending: ControlRequest?

    var supports: AgentControlsInfo.Supports {
        info?.supports ?? .init(model: false, effort: false, mode: false, compact: false, clear: false)
    }

    var isAvailable: Bool { supports.any }
    var isBusy: Bool { agent.status == .working || agent.status == .blocked }

    var modelId: String? {
        if case .model(let id) = pending { return id }
        return ControlDisplay.modelId(agent, models: info?.models ?? [])
    }

    var modelLabel: String? {
        if case .model(let id) = pending { return ControlDisplay.label(id, in: info?.models) }
        return agent.modelLabel ?? modelId.map { ControlDisplay.label($0, in: info?.models) } ?? agent.model
    }

    var mode: String? {
        if case .permissionMode(let id) = pending { return id }
        return agent.permissionMode
    }

    var modeLabel: String? { mode.map { ControlDisplay.label($0, in: info?.modes) } }


    var effort: String? {
        if case .effort(let id) = pending { return id }
        return agent.effort
    }

    var effortLabel: String? { effort.map { ControlDisplay.label($0, in: info?.efforts) } }

    /// "Opus 5.5 · Auto" under the title; kinds without modes show effort instead ("gpt-5-codex · High").
    var subtitle: String? {
        let second = supports.mode || mode != nil ? modeLabel : effortLabel
        let parts = [modelLabel, second].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// Why the controls are disabled, shown as the menu section caption.
    var busyCaption: String? {
        switch agent.status {
        case .working: "Agent is working"
        case .blocked: "Answer the agent's question first"
        default: pending == nil ? nil : "Applying…"
        }
    }
}

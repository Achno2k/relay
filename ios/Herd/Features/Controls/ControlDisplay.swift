import HerdKit
import SwiftUI

/// Labels and symbols for model / mode / effort, taking in-flight changes into account.
enum ControlDisplay {
    /// Alias in `catalog.models` that `agent.model` (a full id like "claude-sonnet-5") belongs to.
    static func modelAlias(_ agent: Agent, catalog: ControlsCatalog?) -> String? {
        guard let model = agent.model?.lowercased() else { return nil }
        return catalog?.models.first { model.contains($0.id.lowercased()) }?.id
    }

    static func label(_ id: String, in options: [ControlOption]?) -> String {
        options?.first { $0.id == id }?.label ?? id.prefix(1).uppercased() + id.dropFirst()
    }

    static func modeSymbol(_ mode: String?) -> String {
        switch mode {
        case "plan": "checklist"
        case "acceptEdits": "pencil.line"
        case "auto": "bolt.fill"
        case "bypassPermissions": "exclamationmark.shield"
        default: "hand.raised"
        }
    }

    static func modeTint(_ mode: String?) -> Color {
        mode == "bypassPermissions" ? .orange : .blue
    }

    /// "Couldn't switch to Sonnet 5" etc., in front of the bridge's error message.
    static func failurePrefix(_ request: ControlRequest, catalog: ControlsCatalog?) -> String {
        switch request {
        case .model(let id): "Couldn't switch to \(label(id, in: catalog?.models))"
        case .permissionMode(let id): "Couldn't switch to \(label(id, in: catalog?.modes))"
        case .effort(let id): "Couldn't set effort to \(label(id, in: catalog?.efforts))"
        case .command(.compact): "Couldn't compact"
        case .command(.clear): "Couldn't clear"
        }
    }
}

/// What the chat header and menus show for one agent: server values overlaid with a pending change.
struct AgentControls {
    let agent: Agent
    let catalog: ControlsCatalog?
    let pending: ControlRequest?

    var isAvailable: Bool { agent.kind == "claude" && catalog != nil }
    var isBusy: Bool { agent.status == .working || agent.status == .blocked }

    var modelAlias: String? {
        if case .model(let id) = pending { return id }
        return ControlDisplay.modelAlias(agent, catalog: catalog)
    }

    var modelLabel: String? {
        if case .model(let id) = pending { return ControlDisplay.label(id, in: catalog?.models) }
        return agent.modelLabel ?? modelAlias.map { ControlDisplay.label($0, in: catalog?.models) }
    }

    var mode: String? {
        if case .permissionMode(let id) = pending { return id }
        return agent.permissionMode
    }

    var modeLabel: String? { mode.map { ControlDisplay.label($0, in: catalog?.modes) } }

    var effort: String? {
        if case .effort(let id) = pending { return id }
        return agent.effort
    }

    var effortLabel: String? { effort.map { ControlDisplay.label($0, in: catalog?.efforts) } }

    /// "Opus 5.5 · Auto" under the title.
    var subtitle: String? {
        let parts = [modelLabel, modeLabel].compactMap { $0 }
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

import HerdKit
import SwiftUI

/// Model ▸ / Mode ▸ / Effort ▸, then Compact and Clear. Used in the title menu.
struct ControlMenuItems: View {
    let store: AppStore
    let controls: AgentControls
    let onClear: () -> Void

    private var agentId: String { controls.agent.id }

    var body: some View {
        let catalog = controls.catalog
        Section {
            if locked {
                LockedRow(title: "Model", symbol: "cpu", value: controls.modelLabel)
                LockedRow(title: "Mode", symbol: ControlDisplay.modeSymbol(controls.mode), value: controls.modeLabel)
                LockedRow(title: "Effort", symbol: "gauge.with.dots.needle.50percent", value: controls.effortLabel)
            } else {
            if let models = catalog?.models, !models.isEmpty {
                Picker(selection: binding(controls.modelAlias) { .model($0) }) {
                    ForEach(models) { Text($0.label).tag($0.id) }
                } label: {
                    Label("Model", systemImage: "cpu")
                    Text(rowSubtitle(controls.modelLabel, pendingIf: { if case .model = $0 { true } else { false } }))
                }
                .pickerStyle(.menu)
            }
            ModeMenuItems(store: store, controls: controls)
            if let efforts = catalog?.efforts, !efforts.isEmpty {
                Picker(selection: binding(controls.effort) { .effort($0) }) {
                    ForEach(efforts) { Text($0.label).tag($0.id) }
                } label: {
                    Label("Effort", systemImage: "gauge.with.dots.needle.50percent")
                    Text(rowSubtitle(controls.effortLabel, pendingIf: { if case .effort = $0 { true } else { false } }))
                }
                .pickerStyle(.menu)
            }
            }
        } header: {
            if let caption = controls.busyCaption { Text(caption) }
        }

        Section {
            Button {
                store.control(.command(.compact), for: agentId)
            } label: {
                Label(controls.pending == .command(.compact) ? "Compacting…" : "Compact context",
                      systemImage: "arrow.down.right.and.arrow.up.left")
            }
            Button(role: .destructive, action: onClear) {
                Label(controls.pending == .command(.clear) ? "Clearing…" : "Clear conversation", systemImage: "trash")
            }
        }
        .disabled(locked)
    }

    /// `.disabled` greys a menu's buttons but not its pickers, so while busy the pickers become locked rows.
    private var locked: Bool { controls.isBusy || controls.pending != nil }

    /// Menu rows can't animate, so a pending change reads "Switching to …" the next time it opens.
    private func rowSubtitle(_ current: String?, pendingIf matches: (ControlRequest) -> Bool) -> String {
        if let pending = controls.pending, matches(pending) { return "Switching to \(current ?? "…")" }
        return current ?? "Unknown"
    }

    private func binding(_ current: String?, _ make: @escaping (String) -> ControlRequest) -> Binding<String> {
        Binding(
            get: { current ?? "" },
            set: { new in
                guard new != current else { return }
                store.control(make(new), for: agentId)
            }
        )
    }
}

/// A disabled menu row showing the current value.
private struct LockedRow: View {
    let title: String
    let symbol: String
    let value: String?

    var body: some View {
        Button {} label: {
            Label(title, systemImage: symbol)
            Text(value ?? "Unknown")
        }
        .disabled(true)
    }
}

/// The Mode picker, shared by the title menu and the composer's mode chip.
struct ModeMenuItems: View {
    let store: AppStore
    let controls: AgentControls
    /// The chip lists the modes directly; the title menu nests them under "Mode ▸".
    var inline = false

    var body: some View {
        if let modes = controls.catalog?.modes, !modes.isEmpty {
            let picker = Picker(selection: Binding(
                get: { controls.mode ?? "" },
                set: { new in
                    guard new != controls.mode else { return }
                    store.control(.permissionMode(new), for: controls.agent.id)
                }
            )) {
                ForEach(modes) { Label($0.label, systemImage: ControlDisplay.modeSymbol($0.id)).tag($0.id) }
            } label: {
                Label("Mode", systemImage: ControlDisplay.modeSymbol(controls.mode))
                Text(subtitle)
            }
            if inline {
                picker.pickerStyle(.inline)
            } else {
                picker.pickerStyle(.menu)
            }
        }
    }

    private var subtitle: String {
        if case .permissionMode = controls.pending { return "Switching to \(controls.modeLabel ?? "…")" }
        return controls.modeLabel ?? "Unknown"
    }
}

/// Composer chip for a non-default mode ("Plan"); tapping opens the Mode menu. Like ChatGPT's tool chips.
struct ModeChip: View {
    let store: AppStore
    let controls: AgentControls

    var body: some View {
        let tint = ControlDisplay.modeTint(controls.mode)
        Menu {
            if let caption = controls.busyCaption {
                Section(caption) {
                    LockedRow(title: "Mode", symbol: ControlDisplay.modeSymbol(controls.mode), value: controls.modeLabel)
                }
            } else {
                ModeMenuItems(store: store, controls: controls, inline: true)
            }
        } label: {
            HStack(spacing: 5) {
                if case .permissionMode = controls.pending {
                    ProgressView().controlSize(.mini)
                } else {
                    Image(systemName: ControlDisplay.modeSymbol(controls.mode))
                        .font(.caption.weight(.semibold))
                }
                Text(controls.modeLabel ?? "")
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)
            }
            .foregroundStyle(tint)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(tint.opacity(0.14), in: .capsule)
            .contentShape(.capsule)
        }
        .accessibilityIdentifier("modeChip")
        .accessibilityLabel("Mode: \(controls.modeLabel ?? "")")
    }
}

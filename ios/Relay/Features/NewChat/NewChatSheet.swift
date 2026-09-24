import RelayKit
import SwiftUI

/// Folder, agent kind, then that kind's Model and Effort from the bridge. The first message is typed in the
/// chat that opens, not here.
struct NewChatSheet: View {
    let store: AppStore
    let initialWorkspaceId: String?
    @Environment(\.dismiss) private var dismiss

    @State private var workspaceId: String?
    @State private var kind = "claude"
    /// "" = the kind's saved default (shown as "Default (…)"), so nothing is overridden.
    @State private var model = ""
    @State private var effort = ""
    @State private var controls: AgentControlsInfo?
    @State private var loading = false
    @State private var creating = false

    private static let kinds = ["claude", "codex", "pi"]
    /// Longer model lists (pi) open as a list instead of a menu.
    private static let menuLimit = 8

    private var effortChoices: [ControlOption] {
        controls?.efforts(for: model.isEmpty ? controls?.defaultModel : model) ?? []
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Folder") {
                    ForEach(store.state.workspaces) { workspace in
                        Button {
                            workspaceId = workspace.id
                        } label: {
                            HStack {
                                Image(workspace.id == workspaceId ? "folder-open" : "folder-closed")
                                    .resizable()
                                    .renderingMode(.template)
                                    .scaledToFit()
                                    .frame(width: 20, height: 20)
                                    .foregroundStyle(.secondary)
                                    .frame(width: 24)
                                Text(workspace.name)
                                    .foregroundStyle(.primary)
                                Spacer()
                                Text(workspace.agentCount == 1 ? "1 agent" : "\(workspace.agentCount) agents")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                                if workspace.id == workspaceId {
                                    Image(systemName: "checkmark")
                                        .font(.body.weight(.semibold))
                                        .foregroundStyle(.tint)
                                }
                            }
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                }

                Section("Agent") {
                    Picker("Agent", selection: $kind) {
                        ForEach(Self.kinds, id: \.self) { Text(KindIcon.name($0).capitalized).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                }

                Section {
                    if loading && controls == nil {
                        HStack {
                            ProgressView()
                            Text("Loading \(KindIcon.name(kind)) models…").foregroundStyle(.secondary)
                        }
                    } else if let controls, !controls.models.isEmpty {
                        modelPicker(controls)
                        if !effortChoices.isEmpty {
                            Picker("Effort", selection: $effort) {
                                Text(defaultLabel(controls.defaultEffort, in: effortChoices)).tag("")
                                ForEach(effortChoices) { Text($0.label).tag($0.id) }
                            }
                            .pickerStyle(.menu)
                            .accessibilityIdentifier("newChatEffort")
                        }
                    }
                } footer: {
                    if !loading && controls == nil {
                        Text("Couldn't load \(KindIcon.name(kind))'s models. It starts with its saved defaults.")
                    } else {
                        Text("Opens a new tab in the folder and starts \(KindIcon.name(kind)) there. Nothing is saved as its default.")
                    }
                }
            }
            .navigationTitle("New chat")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(role: .confirm) {
                        create()
                    } label: {
                        if creating { ProgressView() } else { Label("Create", systemImage: "checkmark") }
                    }
                    .disabled(workspaceId == nil || creating)
                }
            }
        }
        .onAppear {
            workspaceId = initialWorkspaceId ?? store.selectedAgent?.workspaceId ?? store.state.workspaces.first?.id
        }
        .task(id: kind) { await loadControls() }
        .onChange(of: model) {
            // pi and codex efforts depend on the model; drop a choice the new model doesn't offer.
            if !effort.isEmpty, !effortChoices.contains(where: { $0.id == effort }) { effort = "" }
        }
        .sensoryFeedback(.success, trigger: creating) { old, new in old && !new }
    }

    @ViewBuilder
    private func modelPicker(_ controls: AgentControlsInfo) -> some View {
        let picker = Picker("Model", selection: $model) {
            Text(defaultLabel(controls.defaultModel, in: controls.models)).tag("")
            ForEach(controls.models) { Text($0.label).tag($0.id) }
        }
        .accessibilityIdentifier("newChatModel")
        if controls.models.count > Self.menuLimit {
            picker.pickerStyle(.navigationLink)
        } else {
            picker.pickerStyle(.menu)
        }
    }

    /// "Default (Sonnet 5)": the saved default, which may be outside the list (codex).
    private func defaultLabel(_ id: String?, in options: [ControlOption]) -> String {
        guard let id else { return "Default" }
        return "Default (\(options.first { $0.id == id }?.label ?? id))"
    }

    private func loadControls() async {
        model = ""
        effort = ""
        controls = nil
        loading = true
        controls = await store.kindControls(kind)
        loading = false
    }

    private func create() {
        guard let workspaceId else { return }
        creating = true
        Task {
            let ok = await store.createAgent(
                workspaceId: workspaceId, kind: kind,
                model: model.isEmpty ? nil : model, effort: effort.isEmpty ? nil : effort
            )
            creating = false
            if ok { dismiss() }
        }
    }
}

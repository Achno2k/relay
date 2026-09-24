import RelayKit
import SwiftUI

struct NewChatSheet: View {
    let store: AppStore
    let initialWorkspaceId: String?
    @Environment(\.dismiss) private var dismiss

    @State private var workspaceId: String?
    @State private var kind = "claude"
    @State private var prompt = ""
    @State private var creating = false
    @FocusState private var promptFocused: Bool

    private static let kinds = ["claude", "codex", "pi"]

    var body: some View {
        NavigationStack {
            Form {
                Section("Workspace") {
                    ForEach(store.state.workspaces) { workspace in
                        Button {
                            workspaceId = workspace.id
                        } label: {
                            HStack {
                                Image(systemName: "folder")
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
                        ForEach(Self.kinds, id: \.self) { Text($0.capitalized).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                }

                Section {
                    TextField("What should it work on?", text: $prompt, axis: .vertical)
                        .lineLimit(3...8)
                        .focused($promptFocused)
                } header: {
                    Text("First message")
                } footer: {
                    Text("Optional. Opens a new tab in the workspace and starts \(kind.capitalized) there.")
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
        .sensoryFeedback(.success, trigger: creating) { old, new in old && !new }
    }

    private func create() {
        guard let workspaceId else { return }
        creating = true
        Task {
            let ok = await store.createAgent(workspaceId: workspaceId, kind: kind, prompt: prompt)
            creating = false
            if ok { dismiss() }
        }
    }
}

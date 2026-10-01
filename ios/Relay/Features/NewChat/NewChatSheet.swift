import RelayKit
import SwiftUI

/// Machine (in All, with several paired), folder, agent kind, then that kind's Model and Effort from that
/// machine's bridge. The first message is typed in the chat that opens, not here.
struct NewChatSheet: View {
    let store: AppStore
    /// A key; picks its machine too.
    let initialWorkspaceId: String?
    @Environment(\.dismiss) private var dismiss

    /// The machine the last chat was started on; the default when nothing else decides.
    @AppStorage("newChatMachine", store: AppDefaults.standard) private var lastMachineId = ""
    @State private var machineId: String?
    @State private var workspaceId: String?
    @State private var kind = "claude"
    /// "" = the kind's saved default (shown as "Default (…)"), so nothing is overridden.
    @State private var model = ""
    @State private var effort = ""
    @State private var controls: AgentControlsInfo?
    @State private var loading = false
    @State private var creating = false
    /// The machine's `GET /kinds`; nil until it answers, or when it can't say (older bridge, offline).
    @State private var kindStatuses: [KindStatus]?
    /// A `409 not_signed_in`/`not_installed` from Create (the bridge knew better than `/kinds`).
    @State private var notReadyMessage: String?

    private nonisolated static let kinds = ["claude", "codex", "pi"]
    /// Longer model lists (pi) open as a list instead of a menu.
    private static let menuLimit = 8

    private var effortChoices: [ControlOption] {
        controls?.efforts(for: model.isEmpty ? controls?.defaultModel : model) ?? []
    }

    /// Only in All: with one machine picked (or paired) the chat goes there.
    private var choosesMachine: Bool { store.machineFilter == nil && store.hasSeveralMachines }

    private var machine: MachineEntry? { machineId.flatMap { id in store.machines.first { $0.id == id } } }

    /// The chosen machine's folders.
    private var workspaces: [Workspace] { workspaces(on: machineId) }

    private func workspaces(on machineId: String?) -> [Workspace] {
        guard let machineId else { return store.state.workspaces }
        return store.state.workspaces.filter { store.machineId(of: $0.id) == machineId }
    }

    private var machineIsDown: Bool { machine.map { $0.status == .offline || $0.status == .needsRePair } ?? false }

    var body: some View {
        NavigationStack {
            Form {
                if choosesMachine {
                    Section {
                        Picker("Machine", selection: $machineId) {
                            ForEach(store.machines) { machine in
                                Text(machine.status == .online || machine.status == .connecting
                                     ? machine.displayName : "\(machine.displayName) (\(machine.status.spoken))")
                                    .tag(Optional(machine.id))
                            }
                        }
                        .pickerStyle(.menu)
                        .accessibilityIdentifier("newChatMachine")
                    } footer: {
                        if let machine, machineIsDown {
                            Text("\(machine.displayName) is \(machine.status.spoken). Pick another machine or try again once it's back.")
                        }
                    }
                }
                Section("Folder") {
                    if workspaces.isEmpty {
                        Text("No folders on this machine yet.")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(workspaces) { workspace in
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
                    ForEach(Self.kinds, id: \.self) { kindRow($0) }
                }

                // A kind that can't start has no models to pick; its hint says what to do.
                if kindCanStart {
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
                    .disabled(workspaceId == nil || creating || machineIsDown || !kindCanStart)
                }
            }
        }
        .onAppear {
            let machine = defaultMachine()
            let folders = workspaces(on: machine)
            let preferred = initialWorkspaceId ?? store.selectedAgent?.workspaceId
            machineId = machine
            workspaceId = folders.contains { $0.id == preferred } ? preferred : folders.first?.id
        }
        .onChange(of: machineId) { _, _ in
            if !workspaces.contains(where: { $0.id == workspaceId }) { workspaceId = workspaces.first?.id }
        }
        .task(id: machineId) { await loadKinds() }
        .task(id: "\(kind)|\(machineId ?? "")|\(kindCanStart)") { await loadControls() }
        .alert(
            "Can't start \(KindIcon.name(kind))",
            isPresented: Binding(get: { notReadyMessage != nil }, set: { if !$0 { notReadyMessage = nil } })
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(Self.hintText(notReadyMessage ?? ""))
        }
        .onChange(of: model) {
            // pi and codex efforts depend on the model; drop a choice the new model doesn't offer.
            if !effort.isEmpty, !effortChoices.contains(where: { $0.id == effort }) { effort = "" }
        }
        .sensoryFeedback(.success, trigger: creating) { old, new in old && !new }
    }

    private func status(of kind: String) -> KindStatus? { kindStatuses?.first { $0.kind == kind } }

    /// Unknown (no `/kinds` answer yet, or an older bridge) counts as startable; the bridge has the last word.
    private var kindCanStart: Bool { status(of: kind)?.canStart ?? true }

    /// One agent kind. One that can't start on this machine is greyed out with the bridge's hint under it.
    private func kindRow(_ kind: String) -> some View {
        let status = status(of: kind)
        let enabled = status?.canStart ?? true
        return Button {
            self.kind = kind
        } label: {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(KindIcon.name(kind).capitalized)
                        .foregroundStyle(enabled ? .primary : .secondary)
                    if !enabled, let hint = status?.signInHint {
                        Text(Self.hintText(hint))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if enabled && kind == self.kind {
                    Image(systemName: "checkmark")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.tint)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("newChatKind-\(kind)")
        .accessibilityAddTraits(enabled && kind == self.kind ? .isSelected : [])
    }

    /// The hints quote commands in backticks; show them as code.
    nonisolated static func hintText(_ hint: String) -> AttributedString {
        (try? AttributedString(markdown: hint)) ?? AttributedString(hint)
    }

    /// The kind to have selected once `/kinds` answers: the current one if it can start (or nothing is
    /// known), else the first that can. None can: keep it, so its hint shows and Create stays off.
    nonisolated static func pickKind(_ current: String, statuses: [KindStatus]?) -> String {
        guard let statuses, statuses.first(where: { $0.kind == current })?.canStart == false else { return current }
        return kinds.first { k in statuses.first { $0.kind == k }?.canStart ?? true } ?? current
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

    private func defaultMachine() -> String? {
        Self.defaultMachine(
            project: initialWorkspaceId.flatMap(store.machineId(of:)), filter: store.machineFilter,
            lastUsed: choosesMachine ? lastMachineId : nil, openChat: store.selectedAgentId.flatMap(store.machineId(of:)),
            machines: store.machines
        )
    }

    /// The project's machine (its `+`), else the one picked in the menu, else the last used, else the open
    /// chat's, else the first online, else the first. Only machines still paired count.
    static func defaultMachine(
        project: String?, filter: String?, lastUsed: String?, openChat: String?, machines: [MachineEntry]
    ) -> String? {
        let paired = Set(machines.map(\.id))
        let candidates = [project, filter, lastUsed, openChat, machines.first { $0.status == .online }?.id, machines.first?.id]
        return candidates.lazy.compactMap(\.self).first { paired.contains($0) }
    }

    private func loadKinds() async {
        kindStatuses = nil
        guard let machineId else { return }
        let statuses = await store.kinds(machineId: machineId)
        guard !Task.isCancelled else { return }
        kindStatuses = statuses
        kind = Self.pickKind(kind, statuses: statuses)
    }

    private func loadControls() async {
        model = ""
        effort = ""
        controls = nil
        guard let machineId, kindCanStart else { return }
        loading = true
        controls = await store.kindControls(kind, machineId: machineId)
        loading = false
    }

    private func create() {
        guard let workspaceId else { return }
        creating = true
        Task {
            let outcome = await store.createAgent(
                workspaceId: workspaceId, kind: kind,
                model: model.isEmpty ? nil : model, effort: effort.isEmpty ? nil : effort
            )
            creating = false
            switch outcome {
            case .created:
                if let machineId { lastMachineId = machineId }
                dismiss()
            case .kindNotReady(let message):
                notReadyMessage = message
                // `/kinds` was stale; refetch so the row greys out with its hint.
                if let machineId, let statuses = await store.kinds(machineId: machineId) { kindStatuses = statuses }
            case .failed:
                break
            }
        }
    }
}

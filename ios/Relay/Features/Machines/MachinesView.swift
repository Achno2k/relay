import RelayKit
import SwiftUI

/// Every paired machine with its status, OS and bridge version. A machine opens into rename (local only),
/// re-pair and remove; + adds one through the same pairing flow as first launch. From the machine menu and
/// the sidebar's ••• menu.
struct MachinesView: View {
    @Bindable var store: AppStore
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var adding = false
    @State private var rePairing = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(store.machines) { machine in
                        NavigationLink(value: machine.id) {
                            MachineRow(machine: machine)
                        }
                        .accessibilityElement(children: .combine)
                        .accessibilityLabel("Machine: \(machine.displayName), \(machine.status.spoken)")
                        .accessibilityValue(MachineRow.details(machine))
                        .accessibilityIdentifier("machineRow-\(machine.id)")
                    }
                } footer: {
                    Text("Each machine runs its own bridge, and the phone talks to each one directly. Names you give machines here stay on this phone.")
                }
            }
            .navigationTitle("Machines")
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: String.self) { id in
                MachineDetailView(store: store, machineId: id) {
                    model.rePairMachineId = id
                    rePairing = true
                }
            }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                        .accessibilityIdentifier("machinesDone")
                }
                ToolbarItem(placement: .topBarLeading) {
                    Button { adding = true } label: {
                        Label("Add machine", systemImage: "plus")
                    }
                    .accessibilityIdentifier("machinesAdd")
                }
            }
        }
        .sheet(isPresented: $adding) {
            PairingView()
        }
        .sheet(isPresented: $rePairing, onDismiss: { model.rePairMachineId = nil }) {
            PairingView()
        }
    }
}

/// Kind icon, name, then "Online · macOS 26.4 · relay 0.9.0".
private struct MachineRow: View {
    let machine: MachineEntry

    static func details(_ machine: MachineEntry) -> String {
        [machine.os, machine.bridgeVersion.map { "relay \($0)" }].compactMap(\.self).joined(separator: " · ")
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: machine.kind == .laptop ? "laptopcomputer" : "desktopcomputer")
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 32)
            VStack(alignment: .leading, spacing: 2) {
                Text(machine.displayName)
                    .font(.body)
                HStack(spacing: 6) {
                    MachineDot(status: machine.status)
                    Text(([machine.status.title] + [Self.details(machine)].filter { !$0.isEmpty }).joined(separator: " · "))
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44)
    }
}

/// One machine: its local name, what the bridge last reported, re-pair and remove.
private struct MachineDetailView: View {
    @Bindable var store: AppStore
    let machineId: String
    let onRePair: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var label = ""
    @State private var confirmingRemove = false
    @FocusState private var nameFocused: Bool

    private var machine: MachineEntry? { store.machines.first { $0.id == machineId } }

    var body: some View {
        Form {
            if let machine {
                if machine.status == .needsRePair { rePairNotice(machine) }
                Section {
                    TextField(machine.machine?.name ?? machine.host, text: $label)
                        .focused($nameFocused)
                        .submitLabel(.done)
                        .onSubmit(commitName)
                        .textInputAutocapitalization(.words)
                        .accessibilityLabel("Name")
                        .accessibilityIdentifier("machineNameField")
                } header: {
                    Text("Name")
                } footer: {
                    Text("Only on this phone. Leave it empty to use the machine's own name.")
                }
                Section("Status") {
                    LabeledContent("Connection") {
                        HStack(spacing: 6) {
                            MachineDot(status: machine.status)
                            Text(machine.status.title)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    if machine.status == .online, machine.herdrAvailable == false {
                        LabeledContent("herdr", value: "Not running")
                    }
                    if let os = machine.os { LabeledContent("System", value: os) }
                    if let model = machine.machine?.model { LabeledContent("Model", value: model) }
                    if let version = machine.bridgeVersion { LabeledContent("Bridge", value: "relay \(version)") }
                    LabeledContent("Address", value: machine.host)
                }
                if machine.status != .needsRePair {
                    Section {
                        rePairButton
                    } footer: {
                        Text("After the machine's address or token changes. Its chats and settings stay.")
                    }
                }
                Section {
                    Button("Remove Machine", role: .destructive) { confirmingRemove = true }
                        .accessibilityIdentifier("machineRemove")
                } footer: {
                    Text("Forgets the pairing and this phone's state for its chats. Nothing changes on the machine.")
                }
            }
        }
        .navigationTitle(machine?.displayName ?? "Machine")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { label = machine?.label ?? "" }
        .onChange(of: nameFocused) { _, focused in if !focused { commitName() } }
        .onDisappear(perform: commitName)
        .onChange(of: machine == nil) { _, gone in if gone { dismiss() } }
        .confirmationDialog(
            "Remove \(machine?.displayName ?? "this machine")?", isPresented: $confirmingRemove, titleVisibility: .visible
        ) {
            // The machine vanishing pops back to the list (onChange above); a second dismiss would close the sheet.
            Button("Remove", role: .destructive) { store.remove(machineId) }
                .accessibilityIdentifier("machineRemoveConfirm")
        } message: {
            Text("Its chats leave the sidebar. Pair it again any time with relay pair.")
        }
    }

    private var rePairButton: some View {
        Button("Re-pair…", action: onRePair)
            .accessibilityIdentifier("machineRePair")
    }

    /// A refused token puts re-pairing first.
    private func rePairNotice(_ machine: MachineEntry) -> some View {
        Section {
            Label("\(machine.displayName) refused this phone's token, or its address now reaches another machine.",
                  systemImage: "key.slash")
                .foregroundStyle(.secondary)
            rePairButton
        }
    }

    private func commitName() {
        guard let machine, label != (machine.label ?? "") else { return }
        store.rename(machineId, label: label)
    }
}

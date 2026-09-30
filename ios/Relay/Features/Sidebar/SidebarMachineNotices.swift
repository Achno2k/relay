import RelayKit
import SwiftUI

/// A machine that's down and has nothing listed (never loaded, or nothing cached) would otherwise just be
/// missing from the sidebar. One card row per such machine says so; tapping it opens the Machines screen,
/// where a refused token can be re-paired. Machines with chats listed show "offline" on those rows instead.
struct SidebarMachineNotices: View {
    let store: AppStore
    let onMachines: () -> Void

    static func down(in store: AppStore) -> [MachineEntry] {
        let scope = store.filteredMachine.map { [$0] } ?? store.machines
        return scope.filter { machine in
            (machine.status == .offline || machine.status == .needsRePair)
                && !store.state.agents.contains { store.machineId(of: $0.id) == machine.id }
        }
    }

    var body: some View {
        let down = Self.down(in: store)
        if !down.isEmpty {
            Section {
                ForEach(down) { machine in
                    Button(action: onMachines) {
                        HStack(spacing: 14) {
                            Image(systemName: machine.status == .needsRePair ? "key.slash" : "wifi.slash")
                                .font(.body)
                                .foregroundStyle(.secondary)
                                .frame(width: 18)
                            VStack(alignment: .leading, spacing: 0) {
                                Text(machine.displayName)
                                    .font(.body)
                                Text(machine.status == .needsRePair ? "Needs re-pairing" : "Offline. Its chats show here once it's back.")
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            Spacer(minLength: 0)
                            Image(systemName: "chevron.right")
                                .font(.footnote.weight(.bold))
                                .foregroundStyle(.tertiary)
                        }
                        .padding(.vertical, 8)
                        .frame(minHeight: 60)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .sidebarCardRow(leading: 18, separator: machine.id == down.last?.id ? nil : 32)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Machine: \(machine.displayName), \(machine.status.spoken)")
                    .accessibilityHint("Opens Machines")
                    .accessibilityAddTraits(.isButton)
                    .accessibilityIdentifier("machineNotice-\(machine.id)")
                }
            } header: {
                SidebarGapHeader(height: 16)
            }
        }
    }
}

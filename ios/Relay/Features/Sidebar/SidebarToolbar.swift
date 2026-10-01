import RelayKit
import SwiftUI

/// The sidebar's pinned top bar (design A · Quiet list). Row 1, 52 pt: the "Relay" large title on the left
/// and a glass pill with Filter and ••• on the right. Row 2: the machine menu as the title's plain subtitle.
/// The title sits 20 pt in, the pill 16 pt from the edge.
struct SidebarToolbar: View {
    @Bindable var store: AppStore
    let onUsage: () -> Void
    let onAddMachine: () -> Void
    let onMachines: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Text("Relay")
                    .font(.largeTitle.bold())
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                    .accessibilityAddTraits(.isHeader)
                    .frame(maxWidth: .infinity, alignment: .leading)
                HStack(spacing: 0) {
                    SidebarFilterMenu(selection: $store.filter, needsInput: store.sidebar.needsInputCount > 0)
                    moreMenu
                }
                .glassEffect(.regular.interactive(), in: .capsule)
            }
            .frame(minHeight: 52)
            // The design's 28 pt subtitle 4 pt under the bar, drawn as a 44 pt target.
            MachineMenu(store: store, onAddMachine: onAddMachine, onManageMachines: onMachines)
                .padding(.top, -4)
        }
        .padding(.leading, 20)
        .padding(.trailing, 16)
        // Like the system bars: the toolbar stops growing at AX1 so the title, the machine name and the
        // Filter/••• pill keep their room; long-press shows the large content viewer beyond that.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    private var moreMenu: some View {
        Menu {
            Section {
                Button(action: onUsage) {
                    Label("Usage", systemImage: "gauge")
                }
                .accessibilityIdentifier("sidebarUsage")
            }
            Section {
                // Unpairing is per machine now: Remove, on the Machines screen.
                Button(action: onMachines) {
                    Label("Machines", systemImage: "desktopcomputer")
                }
                .accessibilityIdentifier("sidebarMachines")
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.body.weight(.semibold))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .tint(.primary)
        .accessibilityShowsLargeContentViewer {
            Label("More", systemImage: "ellipsis")
        }
        .accessibilityLabel("More")
        .accessibilityIdentifier("sidebarMore")
    }
}

/// "● All machines ⌄" or "● Test VM ⌄", plain text under the title: which machine's chats the sidebar shows.
/// The menu picks All or one machine (with its status), and leads to adding and managing machines. Status is
/// monochrome: the dot is filled when online, hollow while connecting, grey when offline, and orange when the
/// token was refused.
private struct MachineMenu: View {
    @Bindable var store: AppStore
    let onAddMachine: () -> Void
    let onManageMachines: () -> Void

    private var name: String { store.filteredMachine?.displayName ?? "All machines" }

    /// One machine's status, or for All the one that needs attention most.
    private var status: MachineStatus {
        if let machine = store.filteredMachine { return machine.status }
        let all = store.machines.map(\.status)
        for worst in [MachineStatus.needsRePair, .offline, .connecting] where all.contains(worst) { return worst }
        return .online
    }

    /// "online", or for All the machines that aren't: "Test VM offline".
    private var spokenStatus: String {
        if let machine = store.filteredMachine { return machine.status.spoken }
        let down = store.machines.filter { $0.status != .online }
        if down.isEmpty { return store.hasSeveralMachines ? "all online" : "online" }
        return down.map { "\($0.displayName) \($0.status.spoken)" }.joined(separator: ", ")
    }

    var body: some View {
        Menu {
            Picker("Machine", selection: $store.machineFilter) {
                Label("All machines", systemImage: "square.stack")
                    .tag(String?.none)
                    .accessibilityIdentifier("machineMenuAll")
                ForEach(store.machines) { machine in
                    // Menu pickers drop a subtitle, so a machine that isn't online says so in its title.
                    Label(
                        machine.status == .online ? machine.displayName : "\(machine.displayName) · \(machine.status.title)",
                        systemImage: machine.status.symbol
                    )
                    .tag(Optional(machine.id))
                    .accessibilityLabel("Machine: \(machine.displayName), \(machine.status.spoken)")
                    .accessibilityIdentifier("machineMenuItem-\(machine.id)")
                }
            }
            .pickerStyle(.inline)
            Section {
                Button(action: onAddMachine) {
                    Label("Add machine…", systemImage: "plus")
                }
                .accessibilityIdentifier("machineMenuAdd")
                Button(action: onManageMachines) {
                    Label("Manage machines…", systemImage: "desktopcomputer")
                }
                .accessibilityIdentifier("machineMenuManage")
            }
        } label: {
            // 7 pt dot, 15 pt name in the secondary label colour, a small chevron; 44 pt tall to tap.
            HStack(spacing: 6) {
                MachineDot(status: status)
                Text(name)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.secondary)
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .tint(.primary)
        .accessibilityShowsLargeContentViewer {
            Label(name, systemImage: "desktopcomputer")
        }
        .accessibilityLabel("Machine: \(name)")
        .accessibilityValue(spokenStatus)
        .accessibilityIdentifier("sidebarMachineMenu")
    }
}

/// The 7 pt status dot beside a machine's name.
struct MachineDot: View {
    let status: MachineStatus

    var body: some View {
        // UIColor.label: a menu label dims `.primary` to grey.
        let color: Color = switch status {
        case .online, .connecting: Color(.label)
        case .offline: Color(.tertiaryLabel)
        case .needsRePair: .orange
        }
        Circle()
            .strokeBorder(color, lineWidth: status == .connecting ? 1.5 : 0)
            .background(Circle().fill(status == .connecting ? .clear : color))
            .frame(width: 7, height: 7)
            .accessibilityHidden(true)
    }
}

import Foundation
import RelayKit

/// What the sidebar shows about machines (round 9). Names only appear once there's more than one machine
/// to tell apart, and only in All: with one machine picked, every row is on it.
extension AppStore {
    /// More than one machine is paired.
    var hasSeveralMachines: Bool { machines.count > 1 }

    /// Project headers and rows carry their machine's name.
    var showsMachineTags: Bool { machineFilter == nil && hasSeveralMachines }

    /// The machine name to tag an agent or workspace key with, or nil when tags are off.
    func machineTag(_ key: String) -> String? {
        showsMachineTags ? machine(of: key)?.displayName : nil
    }

    /// The machine picked in the menu, nil for All.
    var filteredMachine: MachineEntry? { machineFilter.flatMap { id in machines.first { $0.id == id } } }

    /// Row identifiers keep the bare pane id with one machine (the older UI tests), the key with more.
    func accessibilityKey(_ key: String) -> String { hasSeveralMachines ? key : MachineKey.raw(key) }
}

extension MachineStatus {
    /// Menu and row copy.
    var title: String {
        switch self {
        case .connecting: "Connecting…"
        case .online: "Online"
        case .offline: "Offline"
        case .needsRePair: "Re-pair needed"
        }
    }

    /// VoiceOver: "Machine: Test VM, offline".
    var spoken: String {
        switch self {
        case .connecting: "connecting"
        case .online: "online"
        case .offline: "offline"
        case .needsRePair: "re-pair needed"
        }
    }

    /// For menu items, which only draw SF Symbols: filled when online, hollow while connecting, struck
    /// through when offline, a warning when the token was refused.
    var symbol: String {
        switch self {
        case .connecting: "circle"
        case .online: "circle.fill"
        case .offline: "circle.slash"
        case .needsRePair: "exclamationmark.circle"
        }
    }
}

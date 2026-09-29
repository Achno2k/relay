import Foundation
import RelayKit
import Testing
@testable import Relay

/// Round 9 UI logic: which machine a new chat starts on, and what the sidebar says about machines.
@MainActor
@Suite("Sidebar and new chat across machines")
struct SidebarMachinesTests {
    private func entry(_ id: String, _ status: MachineStatus = .online) -> MachineEntry {
        MachineEntry(id: id, displayName: id, label: nil, machine: nil, bridgeVersion: nil, herdrAvailable: nil, host: id, status: status)
    }

    @Test func newChatStartsOnTheProjectsMachineFirst() {
        let machines = [entry("mac"), entry("vm")]
        #expect(NewChatSheet.defaultMachine(project: "vm", filter: "mac", lastUsed: "mac", openChat: "mac", machines: machines) == "vm")
        #expect(NewChatSheet.defaultMachine(project: nil, filter: "vm", lastUsed: "mac", openChat: "mac", machines: machines) == "vm")
        #expect(NewChatSheet.defaultMachine(project: nil, filter: nil, lastUsed: "vm", openChat: "mac", machines: machines) == "vm")
        #expect(NewChatSheet.defaultMachine(project: nil, filter: nil, lastUsed: nil, openChat: "vm", machines: machines) == "vm")
    }

    @Test func newChatSkipsAMachineThatIsNoLongerPaired() {
        let machines = [entry("mac", .offline), entry("vm")]
        #expect(NewChatSheet.defaultMachine(project: nil, filter: nil, lastUsed: "gone", openChat: nil, machines: machines) == "vm")
        #expect(NewChatSheet.defaultMachine(project: nil, filter: nil, lastUsed: nil, openChat: nil, machines: [entry("mac", .offline)]) == "mac")
        #expect(NewChatSheet.defaultMachine(project: nil, filter: nil, lastUsed: nil, openChat: nil, machines: []) == nil)
    }

    @Test func statusCopyForVoiceOver() {
        #expect(MachineStatus.online.spoken == "online")
        #expect(MachineStatus.offline.spoken == "offline")
        #expect(MachineStatus.needsRePair.spoken == "re-pair needed")
        #expect(MachineStatus.connecting.spoken == "connecting")
    }

    // MARK: With a store of two fake bridges (FakeBridge is in MachinesTests.swift)

    private func store(labels: [String: String] = ["mac": "Mac", "vm": "VM"]) async -> AppStore {
        AppDefaults.resetTestState()
        let connections = ["mac", "vm"].map { id in
            let bridge = FakeBridge(
                id: id, agents: [MachinesTests.agent("w1:p1", ws: "w1", title: "\(id) chat")],
                workspaces: [Workspace(id: "w1", name: "proj", agentCount: 1)]
            )
            return MachineConnection(record: MachineRecord(id: id, url: URL(string: "http://\(id):7878")!, label: labels[id]), raw: bridge)
        }
        let store = AppStore(connections: connections, pairingStore: nil, makeBackend: { _ in FakeBridge(id: "x", agents: [], workspaces: []) })
        await store.refresh()
        return store
    }

    @Test func sameProjectIdOnTwoMachinesIsTwoSections() async {
        let store = await store()
        let sections = store.sidebar.grouped().sections
        #expect(sections.map(\.id) == ["mac/w1", "vm/w1"])
        #expect(sections.map { $0.rows.map(\.displayTitle) } == [["mac chat"], ["vm chat"]])
    }

    @Test func machineTagsOnlyInAllWithSeveralMachines() async {
        let store = await store()
        #expect(store.showsMachineTags)
        #expect(store.machineTag("vm/w1") == "VM")
        #expect(store.machineTag("mac/w1:p1") == "Mac")

        store.machineFilter = "vm"
        #expect(!store.showsMachineTags)
        #expect(store.machineTag("vm/w1") == nil)
        #expect(store.filteredMachine?.displayName == "VM")
        #expect(store.sidebar.grouped().sections.map(\.id) == ["vm/w1"])
    }

    @Test func rowIdentifiersUseKeysOnlyWithSeveralMachines() async {
        let two = await store()
        #expect(two.accessibilityKey("vm/w1:p1") == "vm/w1:p1")
        two.remove("vm")
        #expect(!two.hasSeveralMachines)
        #expect(two.accessibilityKey("mac/w1:p1") == "w1:p1")
        #expect(!two.showsMachineTags)
    }
}

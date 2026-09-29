import Foundation
import RelayKit
import Testing
@testable import Relay

/// One fake bridge. Raw ids only, like a real one.
actor FakeBridge: Backend {
    enum Failure { case none, offline, unauthorized }

    var id: String
    var agentList: [Agent]
    var workspaceList: [Workspace]
    var failure: Failure = .none
    private(set) var prompts: [(agentId: String, text: String)] = []
    private(set) var messageFetches: [String] = []
    private(set) var created: [CreateAgentRequest] = []
    private var continuation: AsyncStream<ConnectionEvent>.Continuation?

    init(id: String, agents: [Agent], workspaces: [Workspace]) {
        self.id = id
        self.agentList = agents
        self.workspaceList = workspaces
    }

    /// How long `agents()` takes.
    var delay: Duration = .zero
    func setDelay(_ d: Duration) { delay = d }
    func setFailure(_ f: Failure) { failure = f }
    func setId(_ id: String) { self.id = id }
    func send(_ event: ConnectionEvent) { continuation?.yield(event) }

    private func check() throws {
        switch failure {
        case .none: break
        case .offline: throw RelayError.unreachable(timedOut: false)
        case .unauthorized: throw RelayError.unauthorized
        }
    }

    func workspaces() async throws -> [Workspace] { try check(); return workspaceList }
    func agents() async throws -> [Agent] {
        try await Task.sleep(for: delay)
        try check()
        return agentList
    }
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try check()
        messageFetches.append(agentId)
        return MessagePage(messages: [Message(id: "\(id)-m1", role: .assistant, createdAt: Date(), blocks: [.text("from \(id) \(agentId)")])], hasMore: false)
    }
    func prompt(agentId: String, text: String, attachments: [String]) async throws {
        try check()
        prompts.append((agentId, text))
    }
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> RelayKit.Attachment { RelayKit.Attachment(id: "a1", name: filename, kind: .file, size: data.count) }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
    func machine() async throws -> Machine { try check(); return Machine(id: id, name: "Name \(id)", kind: .desktop, os: "Linux") }
    func health() async throws -> Health { try check(); return Health(ok: true, version: "9.9.9", herdr: "connected") }
    func sendKeys(agentId: String, keys: [String]) async throws { try check() }
    func sendText(agentId: String, text: String, submit: Bool) async throws { try check() }
    func approval(agentId: String) async throws -> Approval? { nil }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        try check()
        created.append(request)
        return MachinesTests.agent("\(request.workspaceId):p99", ws: request.workspaceId, title: "new")
    }
    func controls() async throws -> ControlsCatalog { ControlsCatalog(models: [], modes: [], efforts: []) }
    func kindControls(kind: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
    func agentControls(agentId: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent { throw RelayError.badResponse }
    nonisolated func events() -> AsyncStream<ConnectionEvent> {
        let (stream, continuation) = AsyncStream.makeStream(of: ConnectionEvent.self)
        Task { await self.attach(continuation) }
        return stream
    }
    private func attach(_ c: AsyncStream<ConnectionEvent>.Continuation) { continuation = c }
    func usage() async throws -> UsageSnapshot { UsageSnapshot(providers: []) }
    func refreshUsage() async throws {}
}

@Suite("Machines: keys, migration, per-machine state", .serialized)
@MainActor
struct MachinesTests {
    nonisolated static func agent(_ id: String, ws: String, title: String, status: AgentStatus = .idle) -> Agent {
        Agent(id: id, name: nil, kind: "claude", title: title, workspaceId: ws, workspaceName: ws, cwdName: ws,
              status: status, hasTranscript: true, updatedAt: Date(timeIntervalSince1970: 1_000_000))
    }

    private func bridge(_ id: String, title: String) -> FakeBridge {
        FakeBridge(id: id, agents: [Self.agent("w1:p1", ws: "w1", title: title)], workspaces: [Workspace(id: "w1", name: "proj", agentCount: 1)])
    }

    private func tempPairings() -> PairingStore {
        let suite = "relay.tests.pairings.\(UUID().uuidString)"
        return PairingStore(defaults: UserDefaults(suiteName: suite)!, vault: .memory())
    }

    private func connection(_ bridge: FakeBridge, id: String, label: String? = nil) -> MachineConnection {
        MachineConnection(record: MachineRecord(id: id, url: URL(string: "http://\(id):7878")!, label: label), raw: bridge)
    }

    private func makeStore(_ connections: [MachineConnection], pairings: PairingStore? = nil, factory: @escaping BackendFactory = { _ in FakeBridge(id: "x", agents: [], workspaces: []) }) -> AppStore {
        AppDefaults.resetTestState()
        return AppStore(connections: connections, pairingStore: pairings, makeBackend: factory)
    }

    // MARK: Keys

    @Test func machineKeySplitsOnFirstSlash() {
        let key = MachineKey.make("test-vm", "w2:p1")
        #expect(key == "test-vm/w2:p1")
        #expect(MachineKey.machineId(key) == "test-vm")
        #expect(MachineKey.raw(key) == "w2:p1")
        #expect(MachineKey.machineId("w2:p1") == nil)
        #expect(MachineKey.raw("w2:p1") == "w2:p1")
        #expect(MachineKey.belongs(key, to: "test-vm"))
        #expect(!MachineKey.belongs(key, to: "test"))
    }

    @Test func namespacedBackendKeysInAndStripsOut() async throws {
        let fake = bridge("vm", title: "t")
        let backend = NamespacedBackend(machineId: "vm", inner: fake)
        let agents = try await backend.agents()
        #expect(agents.map(\.id) == ["vm/w1:p1"])
        #expect(agents.map(\.workspaceId) == ["vm/w1"])
        #expect(try await backend.workspaces().map(\.id) == ["vm/w1"])
        try await backend.prompt(agentId: "vm/w1:p1", text: "hi", attachments: [])
        #expect(await fake.prompts.map(\.agentId) == ["w1:p1"])
        let created = try await backend.createAgent(CreateAgentRequest(workspaceId: "vm/w1", kind: "claude"))
        #expect(await fake.created.map(\.workspaceId) == ["w1"])
        #expect(created.id == "vm/w1:p99")
        #expect(ServerEvent.agentClosed(agentId: "w1:p1").keyed("vm") == .agentClosed(agentId: "vm/w1:p1"))
        #expect(ServerEvent.replyLive(agentId: "w1:p1", text: "x", seq: 2).keyed("vm") == .replyLive(agentId: "vm/w1:p1", text: "x", seq: 2))
    }

    // MARK: Pairings

    @Test func legacyPairingMovesUnderProvisionalId() throws {
        let pairings = tempPairings()
        let url = URL(string: "http://100.64.0.1:7878")!
        try pairings.writeLegacy(Pairing(url: url, token: "tok"))
        let records = pairings.records()
        #expect(records.map(\.id) == [PairingStore.provisionalId])
        #expect(records.first?.provisional == true)
        #expect(pairings.token(PairingStore.provisionalId) == "tok")
        // Old keys are gone, so a second launch doesn't migrate again.
        #expect(pairings.records().count == 1)
        #expect(pairings.token(url.absoluteString) == nil)
    }

    @Test func upsertReplacesUrlAndTokenKeepsLabelAndOrder() throws {
        let pairings = tempPairings()
        try pairings.upsert(MachineRecord(id: "a", url: URL(string: "http://a")!, label: "Desk"), token: "1")
        try pairings.upsert(MachineRecord(id: "b", url: URL(string: "http://b")!), token: "2")
        try pairings.upsert(MachineRecord(id: "a", url: URL(string: "http://a2")!), token: "3")
        let records = pairings.records()
        #expect(records.map(\.id) == ["a", "b"])
        #expect(records.first?.url.absoluteString == "http://a2")
        #expect(records.first?.label == "Desk")
        #expect(pairings.token("a") == "3")
        pairings.remove("a")
        #expect(pairings.records().map(\.id) == ["b"])
        #expect(pairings.token("a") == nil)
    }

    @Test func rekeyOntoAnAlreadyPairedIdKeepsOneEntry() throws {
        let pairings = tempPairings()
        try pairings.upsert(MachineRecord(id: "real", url: URL(string: "http://old")!), token: "old")
        try pairings.upsert(MachineRecord(id: "p", url: URL(string: "http://new")!, provisional: true), token: "new")
        try pairings.rekey("p", to: MachineRecord(id: "real", url: URL(string: "http://new")!))
        #expect(pairings.records().map(\.id) == ["real"])
        #expect(pairings.records().first?.url.absoluteString == "http://new")
        #expect(pairings.token("real") == "new")
        #expect(pairings.token("p") == nil)
    }

    // MARK: Persisted UI keys

    @Test func bareKeysMigrateAndRemovedMachineKeysDrop() {
        let standard = UserDefaults(suiteName: "relay.tests.std.\(UUID().uuidString)")!
        let shared = UserDefaults(suiteName: "relay.tests.shared.\(UUID().uuidString)")!
        standard.set("w1:p1", forKey: PersistedKeys.selected)
        standard.set(["w1:p1": 5.0, "vm/w2:p1": 7.0], forKey: PersistedKeys.seen)
        shared.set(["w1:p2", "vm/w2:p1"], forKey: PersistedKeys.archived)
        standard.set("w1,w3", forKey: "sidebarCompletedOpen")
        PersistedKeys.migrateBare(to: "mac", standard: standard, shared: shared)
        #expect(standard.string(forKey: PersistedKeys.selected) == "mac/w1:p1")
        #expect(standard.dictionary(forKey: PersistedKeys.seen) as? [String: Double] == ["mac/w1:p1": 5, "vm/w2:p1": 7])
        #expect(shared.stringArray(forKey: PersistedKeys.archived) == ["mac/w1:p2", "vm/w2:p1"])
        #expect(standard.string(forKey: "sidebarCompletedOpen") == "mac/w1,mac/w3")

        standard.set("vm", forKey: PersistedKeys.machineFilter)
        PersistedKeys.drop(machineId: "vm", standard: standard, shared: shared)
        #expect(standard.dictionary(forKey: PersistedKeys.seen) as? [String: Double] == ["mac/w1:p1": 5])
        #expect(shared.stringArray(forKey: PersistedKeys.archived) == ["mac/w1:p2"])
        #expect(standard.string(forKey: PersistedKeys.machineFilter) == nil)
        #expect(standard.string(forKey: PersistedKeys.selected) == "mac/w1:p1")
    }

    // MARK: Store

    @Test func samePaneIdOnTwoMachinesStaysApartAndRoutes() async throws {
        let mac = bridge("mac", title: "On the Mac")
        let vm = bridge("vm", title: "On the VM")
        let store = makeStore([connection(mac, id: "mac"), connection(vm, id: "vm")])
        await store.refresh()
        #expect(Set(store.state.agents.map(\.id)) == ["mac/w1:p1", "vm/w1:p1"])
        #expect(store.state.agent("vm/w1:p1")?.title == "On the VM")
        // Sections never merge across machines.
        #expect(store.state.sections().count == 2)

        store.open("vm/w1:p1")
        try await Task.sleep(for: .milliseconds(100))
        #expect(Set(await vm.messageFetches) == ["w1:p1"])
        #expect(store.messages(for: "vm/w1:p1").first?.plainText == "from vm w1:p1")

        store.send("hello", to: "vm/w1:p1")
        try await Task.sleep(for: .milliseconds(100))
        #expect(await vm.prompts.map(\.agentId) == ["w1:p1"])
        #expect(await mac.prompts.isEmpty)
        #expect(store.machineId(of: "vm/w1:p1") == "vm")
    }

    @Test func offlineMachineNeverBlocksTheOthersAndKeepsItsAgents() async throws {
        let mac = bridge("mac", title: "m")
        let vm = bridge("vm", title: "v")
        let store = makeStore([connection(mac, id: "mac"), connection(vm, id: "vm")])
        await store.refresh()
        #expect(store.state.agents.count == 2)

        await vm.setFailure(.offline)
        await store.refresh()
        #expect(store.machines.first { $0.id == "vm" }?.status == .offline)
        #expect(store.state.agent("vm/w1:p1") != nil)  // last known, greyed
        #expect(!store.isLive("vm/w1:p1"))
        #expect(store.errorMessage == nil)  // a background resync of an offline machine is quiet

        // A machine that was never reached still leaves the others working.
        let fresh = bridge("mac", title: "m")
        let down = bridge("vm", title: "v")
        await down.setFailure(.offline)
        let store2 = makeStore([connection(fresh, id: "mac"), connection(down, id: "vm")])
        await store2.refresh()
        #expect(store2.state.agents.map(\.id) == ["mac/w1:p1"])
        #expect(store2.machines.map(\.status) == [.connecting, .offline])
    }

    @Test func rejectedTokenAndChangedIdMarkOnlyThatMachine() async throws {
        let mac = bridge("mac", title: "m")
        let vm = bridge("vm", title: "v")
        await vm.setFailure(.unauthorized)
        let store = makeStore([connection(mac, id: "mac"), connection(vm, id: "vm")])
        await store.refresh()
        #expect(store.machines.map(\.status) == [.connecting, .needsRePair])

        // The VM's URL now reaches another bridge: nothing of it is merged.
        let moved = bridge("someone-else", title: "x")
        let store2 = makeStore([connection(mac, id: "mac"), connection(moved, id: "vm")])
        await store2.refresh()
        #expect(store2.machines.last?.status == .needsRePair)
        #expect(store2.state.agents.map(\.id) == ["mac/w1:p1"])
    }

    @Test func socketStateDrivesStatus() async throws {
        let mac = bridge("mac", title: "m")
        let c = connection(mac, id: "mac")
        let store = makeStore([c])
        store.start()
        defer { store.stop() }
        try await Task.sleep(for: .milliseconds(50))
        await mac.send(.connected)
        try await Task.sleep(for: .milliseconds(50))
        #expect(c.status == .online)
        await mac.send(.disconnected)
        try await Task.sleep(for: .milliseconds(50))
        #expect(c.status == .connecting)  // a drop from online reconnects first
        await mac.send(.disconnected)
        try await Task.sleep(for: .milliseconds(50))
        #expect(c.status == .offline)  // the retry failed too
        await mac.send(.rejected(status: 401))
        try await Task.sleep(for: .milliseconds(50))
        #expect(c.status == .needsRePair)
    }

    @Test func provisionalPairingIsReKeyedWithItsState() async throws {
        let pairings = tempPairings()
        try pairings.writeLegacy(Pairing(url: URL(string: "http://mac:7878")!, token: "tok"))
        let record = try #require(pairings.records().first)
        let mac = bridge("mac-uuid", title: "m")
        let store = makeStore([MachineConnection(record: record, raw: mac)], pairings: pairings)
        AppDefaults.shared.set(["w9:p9"], forKey: PersistedKeys.archived)
        AppDefaults.standard.set(["w9:p9": 5.0], forKey: PersistedKeys.seen)
        await store.refresh()
        #expect(store.machines.map(\.id) == ["mac-uuid"])
        #expect(pairings.records().map(\.id) == ["mac-uuid"])
        #expect(pairings.records().first?.provisional == false)
        #expect(pairings.token("mac-uuid") == "tok")
        #expect(store.state.agents.map(\.id) == ["mac-uuid/w1:p1"])
        #expect(store.isArchived("mac-uuid/w9:p9"))
        #expect((AppDefaults.standard.dictionary(forKey: PersistedKeys.seen) as? [String: Double])?["mac-uuid/w9:p9"] == 5)
        AppDefaults.resetTestState()
        AppDefaults.shared.removeObject(forKey: PersistedKeys.archived)
    }

    @Test func provisionalPairingOfflineWaitsForTheBridge() async throws {
        let pairings = tempPairings()
        try pairings.writeLegacy(Pairing(url: URL(string: "http://mac:7878")!, token: "tok"))
        let record = try #require(pairings.records().first)
        let mac = bridge("mac-uuid", title: "m")
        await mac.setFailure(.offline)
        let store = makeStore([MachineConnection(record: record, raw: mac)], pairings: pairings)
        await store.refresh()
        #expect(store.machines.map(\.id) == [PairingStore.provisionalId])
        #expect(store.machines.first?.status == .offline)
        await mac.setFailure(.none)
        await store.refresh()
        #expect(store.machines.map(\.id) == ["mac-uuid"])
        #expect(pairings.records().map(\.id) == ["mac-uuid"])
    }

    @Test func addRePairRenameRemove() async throws {
        let pairings = tempPairings()
        let vm = bridge("vm", title: "v")
        let other = bridge("other", title: "o")
        let mac = bridge("mac", title: "m")
        let store = makeStore([connection(mac, id: "mac")], pairings: pairings, factory: { pairing in
            pairing.url.host() == "other" ? other : vm
        })
        await store.refresh()

        let id = try await store.addMachine(Pairing(url: URL(string: "http://vm:7881")!, token: "t1"))
        #expect(id == "vm")
        #expect(store.machines.map(\.id) == ["mac", "vm"])
        #expect(pairings.token("vm") == "t1")

        store.rename("vm", label: "  Build box ")
        #expect(store.machines.last?.displayName == "Build box")

        // Same machine, new address and token: replaced in place, label kept.
        try await store.addMachine(Pairing(url: URL(string: "http://vm2:7881")!, token: "t2"))
        #expect(store.machines.map(\.id) == ["mac", "vm"])
        #expect(store.machines.last?.displayName == "Build box")
        #expect(store.machines.last?.host == "vm2")
        #expect(pairings.token("vm") == "t2")

        await #expect(throws: RelayError.differentMachine) {
            try await store.rePair("vm", pairing: Pairing(url: URL(string: "http://other")!, token: "t"))
        }

        store.rename("vm", label: "")
        #expect(store.machines.last?.displayName == "Name vm")

        await store.refresh()
        store.machineFilter = "vm"
        store.setArchived("vm/w1:p1", true)
        store.remove("vm")
        #expect(store.machines.map(\.id) == ["mac"])
        #expect(store.machineFilter == nil)
        #expect(store.state.agents.allSatisfy { !$0.id.hasPrefix("vm/") })
        #expect(!store.isArchived("vm/w1:p1"))
        #expect(pairings.records().isEmpty)  // "mac" was never saved here; "vm" is gone
    }

    @Test func unreachableAddSavesNothing() async throws {
        let pairings = tempPairings()
        let down = bridge("vm", title: "v")
        await down.setFailure(.offline)
        let store = makeStore([], pairings: pairings, factory: { _ in down })
        await #expect(throws: RelayError.unreachable(timedOut: false)) {
            try await store.addMachine(Pairing(url: URL(string: "http://vm")!, token: "t"))
        }
        #expect(store.machines.isEmpty)
        #expect(pairings.records().isEmpty)
    }

    @Test func machineFilterLimitsTheSidebar() async throws {
        let store = makeStore([connection(bridge("mac", title: "m"), id: "mac"), connection(bridge("vm", title: "v"), id: "vm")])
        await store.refresh()
        #expect(store.sidebar.sessions(.all).count == 2)
        store.machineFilter = "vm"
        #expect(store.sidebar.sessions(.all).map(\.id) == ["vm/w1:p1"])
        store.machineFilter = nil
    }

    @Test func createAgentGoesToTheWorkspacesMachine() async throws {
        let mac = bridge("mac", title: "m")
        let vm = bridge("vm", title: "v")
        let store = makeStore([connection(mac, id: "mac"), connection(vm, id: "vm")])
        await store.refresh()
        #expect(await store.createAgent(workspaceId: "vm/w1", kind: "claude", model: nil, effort: nil))
        #expect(await vm.created.map(\.workspaceId) == ["w1"])
        #expect(await mac.created.isEmpty)
        #expect(store.selectedAgentId == "vm/w1:p99")
    }

    @Test func bareAgentLinkPicksTheFirstMachineInPairOrder() async throws {
        let store = makeStore([connection(bridge("mac", title: "m"), id: "mac"), connection(bridge("vm", title: "v"), id: "vm")])
        store.select(link: "w1:p1")
        await store.refresh()
        #expect(store.selectedAgentId == "mac/w1:p1")
        store.select(link: "vm/w1:p1")
        #expect(store.selectedAgentId == "vm/w1:p1")
    }

    @Test func defaultSelectionFollowsPairOrderNotSpeed() async throws {
        let mac = bridge("mac", title: "m")
        await mac.setDelay(.milliseconds(150))
        let store = makeStore([connection(mac, id: "mac"), connection(bridge("vm", title: "v"), id: "vm")])
        await store.refresh()
        #expect(store.selectedAgentId == "mac/w1:p1")
    }

    @Test func bannerSaysOfflineForAnOfflineMachineOnly() async throws {
        let vm = bridge("vm", title: "v")
        let store = makeStore([connection(bridge("mac", title: "m"), id: "mac"), connection(vm, id: "vm")])
        await store.refresh()
        await vm.setFailure(.offline)
        await store.refresh()
        store.selectedAgentId = "vm/w1:p1"
        #expect(store.connectionNotice == .offline(name: "Name vm"))
        #expect(store.connectionNotice?.text == "Name vm is offline")
        store.selectedAgentId = "mac/w1:p1"
        #expect(store.connectionNotice == nil)
        // A dropped socket that's retrying still reads "Reconnecting…".
        let mac = try #require(store.connection(id: "mac"))
        mac.connection = .reconnecting
        mac.status = .connecting
        #expect(store.connectionNotice == .reconnecting)
    }

    @Test func provisionalPairingRetriesUntilTheBridgeIsBack() async throws {
        let pairings = tempPairings()
        try pairings.writeLegacy(Pairing(url: URL(string: "http://mac:7878")!, token: "tok"))
        let record = try #require(pairings.records().first)
        let mac = bridge("mac-uuid", title: "m")
        await mac.setFailure(.offline)
        let store = makeStore([MachineConnection(record: record, raw: mac)], pairings: pairings)
        store.start()
        defer { store.stop() }
        try await Task.sleep(for: .milliseconds(300))
        #expect(store.machines.map(\.status) == [.offline])
        // The bridge comes up; no foreground, no network change: the retry alone must find it.
        await mac.setFailure(.none)
        try await Task.sleep(for: .milliseconds(1500))
        #expect(store.machines.map(\.id) == ["mac-uuid"])
        #expect(store.state.agents.map(\.id) == ["mac-uuid/w1:p1"])
        #expect(pairings.records().map(\.id) == ["mac-uuid"])
    }
}

import Foundation
import RelayKit
import Observation

/// Launch arguments. `-mock` runs against the bundled fixtures; the rest set up a screen for screenshots:
/// `-demo sidebar|tools|top|card|newChat|usage|pairing|changes`, `-agent <id>`, `-replay off`, `-pair <relay:// link>`, `-uitestAttachments`, `-uitest`, `-resetSidebar`.
/// Machines (docs/tasks/round-9/interfaces.md): `-mockVM`, `-mockOffline <id>`, `-mockOnlineAfter <s>`, `-mockDrop <id> <s>`,
/// `-seedLegacyPairing <link>`. Agent kinds: `-mockSignedOut codex,pi`, `-mockNotInstalled pi`, `-mockKindsStale`.
struct LaunchOptions {
    var mock = false
    var demo: String?
    /// A key (`mock-mac/w2:p1`) or a bare pane id (the first machine that has it).
    var agent: String?
    var replay = true
    /// A `relay://pair` (or `herd://pair`) link handled at launch, same path as `onOpenURL`.
    var pairLink: URL?
    /// `-uitestAttachments`: the `+` menu offers generated test files (UI tests can't drive the Photos picker).
    var testAttachments = false
    /// `-testWord X`: the word drawn into the generated test image and PDF, so a live test can't match old replies.
    var testWord: String?
    /// `-uitest`: keep all UI state in a separate defaults suite (see `AppDefaults`).
    var isUITest = false
    /// `-resetSidebar` (with `-uitest`): start from empty test state.
    var resetTestState = false
    /// `-mockVM`: a second mock machine, "Mock VM".
    var mockVM = false
    /// `-mockPlan`: w1:p2 is blocked on a plan approval and its chat ends with ExitPlanMode (B7, MockPlan).
    var mockPlan = false
    /// `-mockOffline <machineId>`: that mock machine starts offline.
    var mockOffline: String?
    /// `-mockOnlineAfter <seconds>`: the `-mockOffline` machine comes back after that long.
    var mockOnlineAfter: Double?
    /// `-mockDrop <machineId> <seconds>`: that mock machine goes offline after that long.
    var mockDrop: (id: String, after: Double)?
    /// `-seedLegacyPairing <link>` (with `-uitest`): write the pre-round-9 single pairing before launch.
    var seedLegacyPairing: URL?
    /// `-mockSignedOut <kinds>`: on every mock machine those CLIs are installed but not signed in.
    var mockSignedOut: Set<String> = []
    /// `-mockNotInstalled <kinds>`: on every mock machine those CLIs aren't installed.
    var mockNotInstalled: Set<String> = []
    /// `-mockKindsStale`: `GET /kinds` says every kind can start, but `POST /agents` still refuses the ones
    /// above (the race the New chat alert covers).
    var mockKindsStale = false
    /// `-realDictation` (with `-mock`): the mic uses the real on-device speech engine, not the scripted one.
    var realDictation = false

    static let current = LaunchOptions(arguments: ProcessInfo.processInfo.arguments)

    init(arguments: [String]) {
        #if DEBUG
        func value(_ flag: String, _ offset: Int = 1) -> String? {
            guard let i = arguments.firstIndex(of: flag), i + offset < arguments.count else { return nil }
            return arguments[i + offset]
        }
        mock = arguments.contains("-mock")
        demo = value("-demo")
        agent = value("-agent")
        replay = value("-replay") != "off"
        pairLink = value("-pair").flatMap(URL.init(string:))
        testAttachments = arguments.contains("-uitestAttachments")
        testWord = value("-testWord")
        isUITest = arguments.contains("-uitest")
        resetTestState = arguments.contains("-resetSidebar")
        mockVM = arguments.contains("-mockVM")
        mockPlan = arguments.contains("-mockPlan")
        mockOffline = value("-mockOffline")
        mockOnlineAfter = value("-mockOnlineAfter").flatMap(Double.init)
        if let id = value("-mockDrop"), let after = value("-mockDrop", 2).flatMap(Double.init) { mockDrop = (id, after) }
        seedLegacyPairing = value("-seedLegacyPairing").flatMap(URL.init(string:))
        func kinds(_ flag: String) -> Set<String> { Set((value(flag) ?? "").split(separator: ",").map(String.init)) }
        mockSignedOut = kinds("-mockSignedOut")
        mockNotInstalled = kinds("-mockNotInstalled")
        mockKindsStale = arguments.contains("-mockKindsStale")
        realDictation = arguments.contains("-realDictation")
        #endif
    }

    func isDemo(_ name: String) -> Bool { demo == name }
}

/// Paired or not. Builds one `AppStore` for all paired machines (or the mock ones).
@MainActor
@Observable
final class AppModel {
    private(set) var store: AppStore?
    var pairingError: String?
    var isPairing = false
    /// When set, the pairing screen re-pairs this machine instead of adding one.
    var rePairMachineId: String?
    @ObservationIgnored private let pairingStore: PairingStore?
    @ObservationIgnored private let makeBackend: BackendFactory
    @ObservationIgnored private var agentLinkApplied = false

    /// At least one machine is paired; otherwise the app shows pairing.
    var isPaired: Bool { store?.connections.isEmpty == false }

    init(options: LaunchOptions = .current) {
        if options.resetTestState { AppDefaults.resetTestState() }
        #if DEBUG
        if options.mock {
            pairingStore = nil
            let replay: Duration? = options.replay ? .seconds(4) : nil
            makeBackend = { pairing in
                // `http://mock-third:7878` pairs a made-up machine; token `bad` is refused.
                if let host = pairing.url.host(), host.hasPrefix("mock-") {
                    return MockBackend(replayInterval: nil, profile: .extra(host: host), rejectsToken: pairing.token == "bad")
                }
                return LiveBackend(pairing: pairing)
            }
            if !options.isDemo("pairing") {
                store = Self.makeMockStore(options, replay: replay, makeBackend: makeBackend)
            }
            return
        }
        if options.isUITest, let link = options.seedLegacyPairing, let pairing = Pairing(link: link) {
            PairingStore.shared.removeAll()
            try? PairingStore.shared.writeLegacy(pairing)
        }
        #endif
        let pairingStore = PairingStore.shared
        self.pairingStore = pairingStore
        makeBackend = { LiveBackend(pairing: $0) }
        let connections = pairingStore.records().map { record in
            // A token lost from the Keychain can only be fixed by pairing again.
            let token = pairingStore.token(record.id)
            let c = MachineConnection(record: record, raw: LiveBackend(pairing: Pairing(url: record.url, token: token ?? "")))
            if token == nil { c.status = .needsRePair }
            return c
        }
        if !connections.isEmpty {
            let store = AppStore(connections: connections, pairingStore: pairingStore, makeBackend: makeBackend)
            if let agent = options.agent {
                store.select(link: agent)
                agentLinkApplied = true
            }
            self.store = store
        }
        if let link = options.pairLink { handle(url: link) }
    }

    #if DEBUG
    private static func makeMockStore(_ options: LaunchOptions, replay: Duration?, makeBackend: @escaping BackendFactory) -> AppStore {
        var profiles = [MockMachine.mac]
        if options.mockVM { profiles.append(.vm) }
        let connections = profiles.map { profile in
            let backend = MockBackend(replayInterval: profile == .mac ? replay : nil, profile: profile, offline: options.mockOffline == profile.machine.id)
            let url = URL(string: "http://\(profile.machine.id):7878")!
            return MachineConnection(record: MachineRecord(id: profile.machine.id, url: url, machine: profile.machine), raw: backend)
        }
        let store = AppStore(connections: connections, pairingStore: nil, makeBackend: makeBackend)
        if let agent = options.agent {
            // Mock ids are known up front: a bare id means the Mac's.
            store.selectedAgentId = MachineKey.machineId(agent) == nil ? MachineKey.make(MockMachine.mac.machine.id, agent) : agent
        }
        if let id = options.mockOffline, let after = options.mockOnlineAfter {
            Task { @MainActor [weak store] in
                try? await Task.sleep(for: .seconds(after))
                await store?.setMockOffline(id, false)
            }
        }
        if let drop = options.mockDrop {
            Task { @MainActor [weak store] in
                try? await Task.sleep(for: .seconds(drop.after))
                await store?.setMockOffline(drop.id, true)
            }
        }
        return store
    }
    #endif

    /// Pairs a machine: adds it (or updates the one with the same id), or re-pairs `rePairMachineId`.
    /// The bridge must answer `/machine` with this token before anything is saved.
    func pair(_ pairing: Pairing) async {
        isPairing = true
        pairingError = nil
        defer { isPairing = false }
        let store = self.store ?? AppStore(connections: [], pairingStore: pairingStore, makeBackend: makeBackend)
        do {
            if let id = rePairMachineId {
                try await store.rePair(id, pairing: pairing)
                rePairMachineId = nil
            } else {
                try await store.addMachine(pairing)
            }
            if self.store == nil { self.store = store }
            #if DEBUG
            // `-pair <link> -agent <id>` (live UI tests): the store only exists once the pairing has worked.
            if !agentLinkApplied, let agent = LaunchOptions.current.agent {
                agentLinkApplied = true
                store.select(link: agent)
            }
            #endif
        } catch {
            // Only RelayError's curated copy ever reaches the pairing screen; anything else
            // (a decode failure, etc.) is already normalized to RelayError by APIClient.
            pairingError = (error as? RelayError)?.errorDescription ?? RelayError.badResponse.errorDescription
        }
    }

    func handle(url: URL) {
        guard let scheme = url.scheme?.lowercased(), Pairing.schemes.contains(scheme) else { return }
        guard let pairing = Pairing(link: url) else {
            pairingError = RelayError.invalidPairingLink.errorDescription
            return
        }
        Task { await pair(pairing) }
    }

    /// Forgets every machine.
    func unpair() {
        guard let store else { return }
        for machine in store.machines { store.remove(machine.id) }
        store.stop()
        self.store = nil
        pairingStore?.removeAll()
    }
}

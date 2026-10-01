import Foundation
import Network
import RelayKit
import Observation

enum ConnectionState: Equatable {
    case connecting, connected, reconnecting
}

/// The banner above the chat for the machine it's about.
enum ConnectionNotice: Equatable {
    case reconnecting
    case offline(name: String)

    var text: String {
        switch self {
        case .reconnecting: "Reconnecting…"
        case .offline(let name): "\(name) is offline"
        }
    }
}

/// Makes the bridge-facing backend for a pairing: `LiveBackend`, or a mock machine under `-mock`.
typealias BackendFactory = @MainActor (Pairing) -> any Backend

/// Owns the machines, the merged state of all of them and the chat that's open.
///
/// Every agent and workspace id in here is a key, `"<machineId>/<rawId>"` (`MachineKey`), so the per-agent maps
/// below work across machines unchanged. Each machine's `NamespacedBackend` turns keys back into raw ids.
@MainActor
@Observable
final class AppStore {
    /// The paired machines, in pair order.
    private(set) var connections: [MachineConnection]
    /// Where pairings persist; nil for mock and test stores.
    @ObservationIgnored private let pairingStore: PairingStore?
    @ObservationIgnored private let makeBackend: BackendFactory
    /// Subscription usage, per machine. Kept off the main `RelayState` reducer; fed by `apply(_:)` below
    /// and refetched on every machine's resync so it never needs its own socket.
    let usage: UsageStore

    private(set) var state = RelayState()
    /// False until the first `/agents` answer (or failure) from any machine, so the UI doesn't flash "No agents".
    private(set) var hasLoadedAgents = false
    var selectedAgentId: String? {
        didSet { AppDefaults.standard.set(selectedAgentId, forKey: PersistedKeys.selected) }
    }
    /// A bare `-agent` id: selected on the first machine (pair order) that has it once each has been tried.
    @ObservationIgnored private var pendingAgentLink: String?
    /// nil = All machines.
    var machineFilter: String? {
        didSet { AppDefaults.standard.set(machineFilter, forKey: PersistedKeys.machineFilter) }
    }
    /// A just-created agent whose composer should take focus when its chat opens.
    var focusComposerFor: String?
    /// `GET /agents/:id/controls` per agent, with the kind and session it was fetched for.
    private(set) var agentControls: [String: AgentControlsInfo] = [:]
    private var agentControlsKey: [String: String] = [:]
    /// Control changes in flight per agent. The UI shows the requested value until the bridge answers.
    private(set) var pendingControls: [String: ControlRequest] = [:]

    /// Approval for the open agent, fetched when it turns blocked.
    private(set) var approval: Approval?
    var isApprovalSheetPresented = false

    /// Prompts sent from the phone that haven't appeared in the transcript yet.
    private(set) var pending: [String: [Message]] = [:]
    /// Ids (into `pending`) whose send failed. The bubble stays visible with an error state instead
    /// of vanishing, so a failed send is never silently dropped; `retry`/`discardFailed` act on it.
    private(set) var failedPending: Set<String> = []
    /// When each agent's last `send()` went out, to catch a double tap sending the same text twice.
    private var lastSendAt: [String: Date] = [:]
    /// A prompt went out and the agent hasn't visibly picked it up yet (herdr still says idle). The chat
    /// shows "Thinking…" from the tap on, not only once `working` arrives. Cleared by `working`/`blocked`,
    /// a reply landing, a failed send, a stop, or after `awaitingTimeout`.
    private(set) var awaitingReply: [String: Date] = [:]
    static let awaitingTimeout: Duration = .seconds(30)
    /// First/last time this session saw each message grow over the socket; feeds "Worked for 42s".
    private(set) var liveSpans: [String: ClosedRange<Date>] = [:]
    /// `reply.live` per agent: the in-progress reply text and the tool running on screen; never persisted.
    private(set) var live = LiveReplies()
    /// Counts socket agent events. Each agent's last one is stamped in `agentEventStamp` (closed: true for
    /// `agent.closed`), so a resync whose `/agents` snapshot was taken before a newer event can't undo it (B6).
    @ObservationIgnored private var agentEvents = 0
    @ObservationIgnored private var agentEventStamp: [String: (stamp: Int, closed: Bool)] = [:]
    /// Last `reply.live` seq per agent. Not observed: it changes on every frame.
    @ObservationIgnored private var liveReplySeq: [String: Int] = [:]
    var liveReplyText: [String: String] { live.byAgent.compactMapValues(\.text) }
    private var seen: [String: Date]
    /// Client-side archive (App Group defaults); it never closes the agent.
    private(set) var archived: Set<String>
    var filter: SessionFilter {
        didSet { AppDefaults.standard.set(filter.rawValue, forKey: "sessionFilter") }
    }
    /// Thumbnails and files already fetched, by attachment id.
    private var attachmentCache: [String: Data] = [:]
    var errorMessage: String?

    /// Between `start()` and `stop()`: machines added meanwhile start right away.
    private var isStarted = false
    private var pathMonitor: NWPathMonitor?
    /// Bumped every time a message load starts for an agent, so a reply from an earlier, slower
    /// request (a rapid agent switch, or a reconnect racing an `open()`) can't overwrite a newer one.
    private var messagesRequest: [String: Int] = [:]

    init(connections: [MachineConnection], pairingStore: PairingStore?, makeBackend: @escaping BackendFactory) {
        self.connections = connections
        self.pairingStore = pairingStore
        self.makeBackend = makeBackend
        self.usage = UsageStore()
        self.selectedAgentId = AppDefaults.standard.string(forKey: PersistedKeys.selected)
        let raw = AppDefaults.standard.dictionary(forKey: PersistedKeys.seen) as? [String: Double] ?? [:]
        self.seen = raw.mapValues { Date(timeIntervalSince1970: $0) }
        self.archived = Set(AppDefaults.shared.stringArray(forKey: PersistedKeys.archived) ?? [])
        self.filter = SessionFilter(rawValue: AppDefaults.standard.string(forKey: "sessionFilter") ?? "") ?? .all
        let savedFilter = AppDefaults.standard.string(forKey: PersistedKeys.machineFilter)
        self.machineFilter = connections.contains { $0.id == savedFilter } ? savedFilter : nil
        syncUsageMachines()
    }

    /// One machine whose ids stay raw: unit tests and previews that predate machines.
    convenience init(backend: any Backend, hostLabel: String) {
        let url = URL(string: "http://\(hostLabel.replacingOccurrences(of: " ", with: "-"))") ?? URL(string: "http://localhost")!
        let record = MachineRecord(id: "local", url: url)
        self.init(connections: [MachineConnection(record: record, raw: backend, keyed: false)], pairingStore: nil, makeBackend: { _ in backend })
    }

    // MARK: - Machines (read side)

    var machines: [MachineEntry] { connections.map(\.entry) }

    func connection(id: String) -> MachineConnection? { connections.first { $0.id == id } }

    /// The machine an agent or workspace key belongs to.
    func connection(for key: String) -> MachineConnection? { connections.first { $0.owns(key) } }

    func machineId(of key: String) -> String? { connection(for: key)?.id }

    func machine(of key: String) -> MachineEntry? { connection(for: key)?.entry }

    /// Its machine is online, so the agent is live and can be prompted.
    func isLive(_ agentKey: String) -> Bool { connection(for: agentKey)?.status == .online }

    /// Routes each per-agent call to its machine's bridge.
    var backend: any Backend {
        var table: [String: any Backend] = [:]
        var fallback: (any Backend)?
        for c in connections {
            if c.keyed { table[c.id] = c.backend } else { fallback = c.backend }
        }
        return RoutingBackend(table: table, fallback: fallback)
    }

    /// The machine the banner speaks for: the open chat's, else the filtered one, else any online, else the first.
    private var focusConnection: MachineConnection? {
        selectedAgentId.flatMap(connection(for:))
            ?? machineFilter.flatMap(connection(id:))
            ?? connections.first { $0.status == .online }
            ?? connections.first
    }

    /// What the banner says about the focus machine: nothing while it's fine or needs re-pairing (that has
    /// its own prompt), "Reconnecting…" while a dropped socket retries, "<name> is offline" once it can't be reached.
    var connectionNotice: ConnectionNotice? {
        guard let c = focusConnection, c.status != .needsRePair else { return nil }
        if c.status == .offline { return .offline(name: c.entry.displayName) }
        return c.connection == .reconnecting ? .reconnecting : nil
    }

    /// The focus machine's socket, for the "Reconnecting…" banner.
    var connection: ConnectionState { focusConnection?.connection ?? .connecting }

    /// The focus machine rejected its token (or now answers as another machine). Sticky until a request
    /// succeeds again or the person re-pairs; never auto-dismissed like a transient error.
    var needsRePairing: Bool { focusConnection?.status == .needsRePair }

    /// The machine `needsRePairing` is about.
    var rePairMachineId: String? { needsRePairing ? focusConnection?.id : nil }

    /// Claude's `GET /controls` list from the focus machine (older call sites; per machine in `MachineConnection`).
    var controls: ControlsCatalog? { focusConnection?.controls }

    /// The focus machine's name (older call sites).
    var hostLabel: String { focusConnection?.entry.displayName ?? "" }

    /// What the sidebar shows: every machine in All, else only the filtered one.
    var visibleState: RelayState {
        guard let filter = machineFilter, let c = connection(id: filter) else { return state }
        var s = state
        s.agents = s.agents.filter { c.owns($0.id) }
        s.workspaces = s.workspaces.filter { c.owns($0.id) }
        return s
    }

    var sidebar: SidebarModel {
        SidebarModel(state: visibleState, archived: archived, isUnseen: { [seen, selectedAgentId] agent in
            agent.status == .done && agent.id != selectedAgentId && (seen[agent.id] ?? .distantPast) < agent.updatedAt
        })
    }

    func isArchived(_ agentId: String) -> Bool { archived.contains(agentId) }

    func setArchived(_ agentId: String, _ value: Bool) {
        if value { archived.insert(agentId) } else { archived.remove(agentId) }
        AppDefaults.shared.set(Array(archived).sorted(), forKey: "archivedAgents")
    }

    /// An archived chat that starts asking a question comes back, so it's never missed.
    private func unarchiveIfBlocked(_ agents: [Agent]) {
        for agent in agents where agent.status == .blocked && archived.contains(agent.id) {
            setArchived(agent.id, false)
        }
    }

    var selectedAgent: Agent? { state.agent(selectedAgentId) }

    /// SwiftUI reads this on every body evaluation. With no pending bubble (the steady state once a
    /// send resolves) this returns the transcript array as is, no copy; `+` would otherwise reallocate
    /// and copy the whole thing, which on a 5,000-message transcript is real main-thread work for
    /// nothing.
    func messages(for agentId: String) -> [Message] {
        guard let pending = pending[agentId], !pending.isEmpty else { return state.messages[agentId] ?? [] }
        return (state.messages[agentId] ?? []) + pending
    }

    func isLoaded(_ agentId: String) -> Bool { state.messages[agentId] != nil }

    func hasMore(_ agentId: String) -> Bool { state.hasMore[agentId] ?? false }

    func isUnseen(_ agent: Agent) -> Bool {
        agent.status == .done && agent.id != selectedAgentId && (seen[agent.id] ?? .distantPast) < agent.updatedAt
    }

    // MARK: - Lifecycle

    func start() {
        guard !isStarted else { return }
        isStarted = true
        for c in connections { start(c) }
        watchNetwork()
    }

    func stop() {
        isStarted = false
        for c in connections { stopSocket(c) }
        pathMonitor?.cancel()
        pathMonitor = nil
    }

    /// A provisional (migrated) machine opens its socket only once `/machine` has told us its real id.
    private func start(_ c: MachineConnection) {
        if c.record.provisional {
            retryAdoption(c)
        } else {
            openSocket(c)
            Task { await refresh(c) }
        }
    }

    /// A provisional machine has no socket to reconnect it, so nothing else would try it again while its
    /// bridge is down: ask `/machine` with the socket's backoff (1 s, 2 s … 30 s) until it answers.
    /// Lives in `eventsTask`, so stop and suspend cancel it like a socket; adoption opens the real socket.
    private func retryAdoption(_ c: MachineConnection) {
        c.eventsTask?.cancel()
        c.eventsTask = Task { [weak self, weak c] in
            var attempt = 0
            while !Task.isCancelled {
                guard let self, let c, c.record.provisional else { return }
                await self.refresh(c)
                guard c.record.provisional else { return }
                try? await Task.sleep(for: WSClient.backoff(attempt: attempt))
                attempt += 1
            }
        }
    }

    private func stopSocket(_ c: MachineConnection) {
        c.eventsTask?.cancel()
        c.eventsTask = nil
    }

    private func openSocket(_ c: MachineConnection) {
        let stream = c.backend.events()
        c.eventsTask = Task { [weak self, weak c] in
            for await event in stream {
                guard let self, let c else { return }
                await self.handle(event, from: c)
            }
        }
    }

    /// Opens a fresh socket now instead of waiting out the backoff (up to 30 s); its `hello` resyncs.
    func reconnect() {
        for c in connections { reconnect(c) }
    }

    private func reconnect(_ c: MachineConnection) {
        if c.record.provisional { return retryAdoption(c) }
        c.eventsTask?.cancel()
        c.resyncOnConnect = true
        openSocket(c)
    }

    /// Background: close the sockets. A suspended app can't read them anyway, they can come back as
    /// zombies that never error, and while one looks open its bridge keeps reading screens and
    /// polling usage for nobody.
    func suspend() {
        for c in connections where c.eventsTask != nil && !c.suspended {
            stopSocket(c)
            c.suspended = true
        }
    }

    /// Foreground: reopen each socket that was closed or is waiting to retry, and resync now so the
    /// chat is current even before the sockets are back.
    func resume() async {
        for c in connections where c.suspended || c.connection == .reconnecting {
            c.suspended = false
            reconnect(c)
        }
        await refresh()
    }

    /// The network came back (airplane mode off, Wi-Fi joined): retry now rather than after the backoff.
    func networkBecameAvailable() {
        for c in connections where (c.connection == .reconnecting || c.record.provisional) && !c.suspended && c.eventsTask != nil {
            reconnect(c)
        }
    }

    private func watchNetwork() {
        guard pathMonitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            guard path.status == .satisfied else { return }
            Task { @MainActor in self?.networkBecameAvailable() }
        }
        monitor.start(queue: .global(qos: .utility))
        pathMonitor = monitor
    }

    private func handle(_ event: ConnectionEvent, from c: MachineConnection) async {
        guard connections.contains(where: { $0 === c }) else { return }  // removed meanwhile
        switch event {
        case .connected:
            let wasDown = c.connection == .reconnecting || c.resyncOnConnect
            c.connection = .connected
            c.resyncOnConnect = false
            c.wasOnline = true
            if c.status != .needsRePair { c.status = .online }
            // Frames sent while we were away are gone, and a restarted bridge starts `seq` over.
            if wasDown {
                for id in liveReplySeq.keys where c.owns(id) { liveReplySeq[id] = nil }
                updateLive { live in for id in live.byAgent.keys where c.owns(id) { live.remove(agentId: id) } }
            }
            // The socket carries deltas only; resync after every (re)connect.
            if wasDown { await refresh(c) }
        case .disconnected:
            c.connection = .reconnecting
            // A drop from online is a reconnect; a retry that fails too means the bridge is out of reach.
            if c.status != .needsRePair { c.status = c.wasOnline ? .connecting : .offline }
            c.wasOnline = false
        case .rejected(let status):
            // Reachable but refused: usually a rotated token. Only REST says so for sure (the bridge
            // answers the upgrade with a bare 400), and without asking the app would sit on
            // "Reconnecting…" forever instead of offering to pair again.
            c.connection = .reconnecting
            if status == 401 || status == 403 {
                c.status = .needsRePair
            } else if c.status != .needsRePair {
                await refresh(c)
            }
        case .event(let e):
            // A bridge that now answers as another machine must never feed this one's state.
            guard c.status != .needsRePair else { return }
            apply(e, machineId: c.id)
        }
    }

    /// `machineId`: whose socket it came from (usage is per machine); nil means the first machine.
    func apply(_ event: ServerEvent, machineId: String? = nil) {
        let before = selectedAgent?.status
        switch event {
        case .agentUpdated(let agent), .agentCreated(let agent):
            agentEvents += 1
            agentEventStamp[agent.id] = (agentEvents, false)
        case .agentClosed(let id):
            agentEvents += 1
            agentEventStamp[id] = (agentEvents, true)
        default:
            break
        }
        if case .agentUpdated(let updated) = event {
            dropPendingIfFinished(updated, previous: state.agent(updated.id)?.status)
        }
        state.apply(event)
        if case .agentUpdated(let agent) = event, agent.status != .working, agent.status != .blocked {
            updateLive { $0.clear(agentId: agent.id) }
        }
        if case .agentUpdated(let agent) = event, agent.status == .working || agent.status == .blocked {
            stopAwaiting(agent.id)
        }
        switch event {
        case .messageUpserted(let agentId, let message):
            let now = Date()
            let start = min(liveSpans[message.id]?.lowerBound ?? message.createdAt, now)
            liveSpans[message.id] = start...now
            if message.role == .user { resolvePending(agentId: agentId, with: [message]) }
            // A turn short enough that herdr never reported `working`: the reply itself ends the wait.
            if message.role != .user, let since = awaitingReply[agentId], message.createdAt >= since.addingTimeInterval(-1) {
                stopAwaiting(agentId)
            }
            updateLive { $0.match(agentId: agentId, transcript: state.messages[agentId] ?? []) }
        case .replyLive(let agentId, let text, let seq, let tool):
            guard seq > (liveReplySeq[agentId] ?? 0) else { break }
            liveReplySeq[agentId] = seq
            updateLive { $0.apply(agentId: agentId, text: text, tool: tool, transcript: state.messages[agentId] ?? []) }
        case .agentClosed(let id):
            for message in pending[id] ?? [] { failedPending.remove(message.id) }
            pending[id] = nil
            stopAwaiting(id)
            updateLive { $0.remove(agentId: id) }
            liveReplySeq[id] = nil
            if id == selectedAgentId { selectedAgentId = firstAgentId() }
        case .agentUpdated(let agent) where agent.id == selectedAgentId:
            unarchiveIfBlocked([agent])
            markSeen(agent)
            reloadIfDropped(agent.id)
            Task { await loadAgentControls(agent.id) }
        case .agentUpdated(let agent), .agentCreated(let agent):
            unarchiveIfBlocked([agent])
        case .usageUpdated(let provider):
            if let id = machineId ?? connections.first?.id { usage.apply(provider, machineId: id) }
        default:
            break
        }
        let after = selectedAgent?.status
        if before != after { Task { await refreshApproval() } }
    }

    /// Assigns only on a real change, so a frame that changes nothing doesn't re-render the chat.
    private func updateLive(_ change: (inout LiveReplies) -> Void) {
        var next = live
        change(&next)
        if next != live { live = next }
    }

    /// Full resync of every machine at once; a slow or offline one doesn't hold up the others.
    func refresh() async {
        await withDiscardingTaskGroup { group in
            for c in connections {
                group.addTask { await self.refresh(c) }
            }
        }
    }

    /// One machine's resync: who it is, its agents and workspaces, then the open chat if it's this machine's.
    func refresh(_ c: MachineConnection) async {
        c.refreshGeneration += 1
        let generation = c.refreshGeneration
        let isCurrent = { [weak self] in
            c.refreshGeneration == generation && (self?.connections.contains { $0 === c } ?? false)
        }
        if c.record.provisional {
            guard await adoptProvisional(c) else {
                c.hasAttempted = true
                markLoadedIfAllTried()
                resolvePendingLink()
                selectFirstIfNeeded()
                return
            }
        }
        let backend = c.backend
        let raw = c.raw
        let eventsBefore = agentEvents
        do {
            async let identity = raw.machine()
            async let health = try? raw.health()
            async let agents = backend.agents()
            async let workspaces = backend.workspaces()
            let (info, h, a, w) = try await (identity, health, agents, workspaces)
            guard isCurrent() else { return }
            // The URL now reaches another bridge: never merge its data into this machine.
            guard !c.keyed || info.id == c.id else {
                c.status = .needsRePair
                c.hasAttempted = true
                return
            }
            c.health = h
            remember(c, machine: info, version: h?.version)
            merge(agents: a, workspaces: w, of: c, eventsBefore: eventsBefore)
            if c.status != .online { c.status = c.connection == .connected ? .online : .connecting }
        } catch {
            guard isCurrent() else { return }
            c.hasAttempted = true
            markLoadedIfAllTried()
            resolvePendingLink()
            selectFirstIfNeeded()
            report(error, machine: c, quiet: true)
            return
        }
        c.hasAttempted = true
        hasLoadedAgents = true
        resolvePendingLink()
        selectFirstIfNeeded()
        Task { await usage.load(machineId: c.id, backend: raw) }
        if c.controls == nil { c.controls = try? await backend.controls() }
        if let id = selectedAgentId, c.owns(id) {
            async let controlsLoad: Void = loadAgentControls(id)
            async let messagesLoad: Void = loadMessages(id)
            async let approvalLoad: Void = refreshApproval()
            _ = await (controlsLoad, messagesLoad, approvalLoad)
        } else if selectedAgentId == nil {
            await refreshApproval()
        }
    }

    /// A failed machine only ends the loading state once every machine has been tried, so an offline first
    /// machine doesn't flash "No agents" while the others are still answering.
    private func markLoadedIfAllTried() {
        if connections.allSatisfy(\.hasAttempted) { hasLoadedAgents = true }
    }

    /// One machine's fresh agents and workspaces replace its old ones; other machines' stay as they are
    /// (an offline machine's last known agents stay listed). Machine order is kept.
    ///
    /// `eventsBefore`: `agentEvents` when the fetch started. The socket keeps delivering while `/agents` is
    /// in flight, and the bridge sends `agent.updated` only on a change, never again: an agent the socket
    /// updated, created or closed since then keeps that newer state instead of the older snapshot. Otherwise a
    /// `done` landing mid-resync (every foreground) was overwritten by `working` and Stop stuck (B6).
    private func merge(agents snapshot: [Agent], workspaces w: [Workspace], of c: MachineConnection, eventsBefore: Int = .max) {
        let newer = agentEventStamp.filter { $0.value.stamp > eventsBefore && c.owns($0.key) }
        var a = snapshot.compactMap { agent -> Agent? in
            guard let event = newer[agent.id] else { return agent }
            return event.closed ? nil : state.agent(agent.id) ?? agent
        }
        let listed = Set(a.map(\.id))
        a += state.agents.filter { newer[$0.id]?.closed == false && !listed.contains($0.id) }
        var s = state
        for agent in a { s.upsert(agent) }  // drops chats whose session changed
        s.agents = connections.flatMap { other in other === c ? a : s.agents.filter { other.owns($0.id) } }
        s.workspaces = connections.flatMap { other in other === c ? w : s.workspaces.filter { other.owns($0.id) } }
        let live = Set(a.map(\.id))
        s.messages = s.messages.filter { !c.owns($0.key) || live.contains($0.key) }
        state = s
        unarchiveIfBlocked(a)
        // Reselect only when the open chat was this machine's and closed, or its machine is gone.
        if let id = selectedAgentId, (c.owns(id) && !live.contains(id)) || connection(for: id) == nil {
            selectedAgentId = firstAgentId()
        }
    }

    /// Saves what `/machine` and `/health` said, so an offline machine still has a name, OS and version.
    private func remember(_ c: MachineConnection, machine: Machine, version: String?) {
        var record = c.record
        guard record.machine != machine || (version != nil && record.bridgeVersion != version) else { return }
        record.machine = machine
        if let version { record.bridgeVersion = version }
        c.replace(record: record)
        pairingStore?.update(record)
        syncUsageMachines()
    }

    /// A migrated pairing learns its real id from `/machine`, then moves its token, its entry and the old
    /// bare-id UI state under that id and opens its socket. False while its bridge can't be reached.
    private func adoptProvisional(_ c: MachineConnection) async -> Bool {
        let info: Machine
        do {
            info = try await c.raw.machine()
        } catch {
            report(error, machine: c, quiet: true)
            return false
        }
        guard c.record.provisional else { return true }  // another refresh got there first
        let oldId = c.id
        var record = c.record
        record.id = info.id
        record.provisional = false
        record.machine = info
        if let existing = connection(id: info.id), existing !== c {
            // Paired again meanwhile under its real id: keep that one, drop the provisional.
            try? pairingStore?.rekey(oldId, to: existing.record)
            connections.removeAll { $0 === c }
            stopSocket(c)
            return false
        }
        try? pairingStore?.rekey(oldId, to: record)
        c.replace(record: record)
        PersistedKeys.migrateBare(to: info.id, standard: AppDefaults.standard, shared: AppDefaults.shared)
        reloadPersistedKeys()
        syncUsageMachines()
        if isStarted { openSocket(c) }
        return true
    }

    /// Re-reads saved selection, seen and archive after their keys were rewritten on disk.
    private func reloadPersistedKeys() {
        let raw = AppDefaults.standard.dictionary(forKey: PersistedKeys.seen) as? [String: Double] ?? [:]
        seen = raw.mapValues { Date(timeIntervalSince1970: $0) }
        archived = Set(AppDefaults.shared.stringArray(forKey: PersistedKeys.archived) ?? [])
        let selected = AppDefaults.standard.string(forKey: PersistedKeys.selected)
        if selected != selectedAgentId { selectedAgentId = selected }
    }

    private func firstAgentId() -> String? {
        visibleState.sections().first?.agents.first?.id
    }

    // MARK: - Deep links

    /// `-agent`: a key opens that chat; a bare pane id opens it on the first machine, in pair order, that has it.
    func select(link: String) {
        if MachineKey.machineId(link) != nil || connections.contains(where: { !$0.keyed }) {
            selectedAgentId = link
        } else {
            pendingAgentLink = link
            resolvePendingLink()
        }
    }

    private func resolvePendingLink() {
        guard let raw = pendingAgentLink else { return }
        for c in connections {
            let key = c.key(raw)
            if state.agent(key) != nil {
                pendingAgentLink = nil
                open(key)
                return
            }
            // Pair order: an earlier machine that hasn't answered yet might still have it.
            if !c.hasAttempted { return }
        }
        pendingAgentLink = nil
        selectFirstIfNeeded()
    }

    /// With nothing open, open the first chat of the first machine in pair order that has one, waiting for
    /// earlier machines to answer first so the default doesn't depend on which bridge is fastest.
    private func selectFirstIfNeeded() {
        guard selectedAgentId == nil, pendingAgentLink == nil else { return }
        for c in connections {
            guard c.hasAttempted else { return }
            if visibleState.agents.contains(where: { c.owns($0.id) }) {
                selectedAgentId = firstAgentId()
                return
            }
        }
    }

    // MARK: - Machines (actions)

    /// Pairs a machine from a `relay://` (or `herd://`) link. See `addMachine(_:)`.
    @discardableResult
    func addMachine(link: URL) async throws -> String {
        guard let pairing = Pairing(link: link) else { throw RelayError.invalidPairingLink }
        return try await addMachine(pairing)
    }

    /// Asks the bridge who it is (`GET /machine`, which also checks the token). A known id gets the new url and
    /// token and keeps its label and every per-agent key (api.md "Multiple machines"); a new one is appended.
    /// Nothing is saved when the bridge can't be reached or refuses the token.
    @discardableResult
    func addMachine(_ pairing: Pairing) async throws -> String {
        let raw = makeBackend(pairing)
        let info = try await identify(raw)
        if let existing = connection(id: info.id) {
            try replacePairing(existing, pairing: pairing, raw: raw, machine: info)
        } else {
            let health = try? await raw.health()
            let record = MachineRecord(id: info.id, url: pairing.url, machine: info, bridgeVersion: health?.version)
            try pairingStore?.upsert(record, token: pairing.token)
            let c = MachineConnection(record: record, raw: raw)
            c.health = health
            connections.append(c)
            syncUsageMachines()
            if isStarted { start(c) }
        }
        return info.id
    }

    /// New url or token for a machine already paired (new IP, rotated token). Throws `.differentMachine` when
    /// the link reaches another bridge; nothing changes then.
    func rePair(_ machineId: String, pairing: Pairing) async throws {
        guard let c = connection(id: machineId) else { throw RelayError.http(status: 404, code: "not_found", message: "That machine isn't paired.") }
        let raw = makeBackend(pairing)
        let info = try await identify(raw)
        guard info.id == machineId else { throw RelayError.differentMachine }
        try replacePairing(c, pairing: pairing, raw: raw, machine: info)
    }

    func rePair(_ machineId: String, link: URL) async throws {
        guard let pairing = Pairing(link: link) else { throw RelayError.invalidPairingLink }
        try await rePair(machineId, pairing: pairing)
    }

    /// Local name only; nil or blank goes back to the machine's own name.
    func rename(_ machineId: String, label: String?) {
        guard let c = connection(id: machineId) else { return }
        var record = c.record
        let trimmed = label?.trimmingCharacters(in: .whitespacesAndNewlines)
        record.label = trimmed?.isEmpty == false ? trimmed : nil
        c.replace(record: record)
        pairingStore?.update(record)
        syncUsageMachines()
    }

    /// Forgets a machine: its token, its agents and chats, and every saved key of it. Nothing is sent to the
    /// bridge. Removing the last one leaves `machines` empty (the app shows pairing).
    func remove(_ machineId: String) {
        guard let c = connection(id: machineId) else { return }
        stopSocket(c)
        connections.removeAll { $0 === c }
        pairingStore?.remove(machineId)
        var s = state
        s.agents.removeAll { c.owns($0.id) }
        s.workspaces.removeAll { c.owns($0.id) }
        s.messages = s.messages.filter { !c.owns($0.key) }
        s.hasMore = s.hasMore.filter { !c.owns($0.key) }
        state = s
        for id in pending.keys where c.owns(id) {
            for message in pending[id] ?? [] { failedPending.remove(message.id) }
            pending[id] = nil
        }
        for id in liveReplySeq.keys where c.owns(id) { liveReplySeq[id] = nil }
        updateLive { live in for id in live.byAgent.keys where c.owns(id) { live.remove(agentId: id) } }
        agentControls = agentControls.filter { !c.owns($0.key) }
        agentControlsKey = agentControlsKey.filter { !c.owns($0.key) }
        pendingControls = pendingControls.filter { !c.owns($0.key) }
        seen = seen.filter { !c.owns($0.key) }
        archived = archived.filter { !c.owns($0) }
        if machineFilter == machineId { machineFilter = nil }
        let wasSelected = selectedAgentId.map(c.owns) ?? false
        PersistedKeys.drop(machineId: machineId, standard: AppDefaults.standard, shared: AppDefaults.shared)
        if wasSelected {
            selectedAgentId = firstAgentId()
            approval = nil
            isApprovalSheetPresented = false
        }
        usage.remove(machineId: machineId)
        syncUsageMachines()
    }

    /// `GET /machine` on a candidate pairing, with a curated error.
    private func identify(_ raw: any Backend) async throws -> Machine {
        do {
            return try await raw.machine()
        } catch let error as RelayError {
            throw error
        } catch {
            throw RelayError.badResponse
        }
    }

    private func replacePairing(_ c: MachineConnection, pairing: Pairing, raw: any Backend, machine: Machine) throws {
        var record = c.record
        record.url = pairing.url
        record.machine = machine
        try pairingStore?.upsert(record, token: pairing.token)
        stopSocket(c)
        c.replace(record: record)
        c.replace(raw: raw)
        c.status = .connecting
        c.connection = .connecting
        c.wasOnline = false
        c.kindControls = [:]
        c.controls = nil
        syncUsageMachines()
        if isStarted { start(c) }
    }

    /// UsageStore follows the machine list (order, names, removals).
    private func syncUsageMachines() {
        usage.setMachines(connections.map { (id: $0.id, name: $0.entry.displayName) })
    }

    #if DEBUG
    /// `-mockOffline` / `-mockDrop` / `-mockOnlineAfter`: take a mock machine down or bring it back.
    func setMockOffline(_ machineId: String, _ offline: Bool) async {
        guard let mock = connection(id: machineId)?.raw as? MockBackend else { return }
        await mock.setOffline(offline)
    }
    #endif

    // MARK: - Chat

    func open(_ agentId: String) {
        selectedAgentId = agentId
        approval = nil
        isApprovalSheetPresented = false
        if let agent = state.agent(agentId) { markSeen(agent) }
        Task {
            await loadMessages(agentId)
            await refreshApproval()
            await loadAgentControls(agentId)
        }
    }

    func loadMessages(_ agentId: String) async {
        messagesRequest[agentId, default: 0] += 1
        let generation = messagesRequest[agentId]
        do {
            let page = try await backend.messages(agentId: agentId, before: nil, limit: 50)
            guard messagesRequest[agentId] == generation else { return }  // superseded by a newer load
            state.mergeLatestPage(page, agentId: agentId)
            resolvePending(agentId: agentId, with: page.messages)
            updateLive { $0.match(agentId: agentId, transcript: state.messages[agentId] ?? []) }
        } catch {
            guard messagesRequest[agentId] == generation else { return }
            report(error, agentId: agentId)
        }
    }

    func loadEarlier(_ agentId: String) async {
        guard hasMore(agentId), let first = state.messages[agentId]?.first else { return }
        do {
            let page = try await backend.messages(agentId: agentId, before: first.id, limit: 50)
            state.prependPage(page, agentId: agentId)
        } catch {
            report(error, agentId: agentId)
        }
    }

    func send(_ text: String, attachments: [Attachment] = [], to agentId: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty || !attachments.isEmpty else { return }
        var blocks = attachments.map { Block.attachment($0.ref) }
        if !trimmed.isEmpty { blocks.append(.text(trimmed)) }
        let local = Message(id: "local-\(UUID().uuidString)", role: .user, createdAt: Date(), blocks: blocks)
        // A double tap on send (or a stuck composer) can re-fire with the same text while the first
        // send is still in flight; a matching, still-live pending bubble from the last couple of
        // seconds means this is that duplicate, not a genuine repeat message.
        if let lastAt = lastSendAt[agentId], Date().timeIntervalSince(lastAt) < 2,
           let last = pending[agentId]?.last, !failedPending.contains(last.id), Self.pendingKey(last) == Self.pendingKey(local) {
            return
        }
        lastSendAt[agentId] = Date()
        pending[agentId, default: []].append(local)
        startAwaiting(agentId)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: trimmed, attachments: attachments.map(\.id))
            } catch {
                // Keep the bubble and flag it, so a failed send is visible and retryable rather than
                // silently vanishing from the transcript (only the transient error banner would remain).
                failedPending.insert(local.id)
                stopAwaiting(agentId)
                report(error, agentId: agentId)
            }
        }
    }

    /// Resends a bubble that failed; e.g. a send attempted while reconnecting.
    func retry(_ messageId: String, to agentId: String) {
        guard let message = pending[agentId]?.first(where: { $0.id == messageId }) else { return }
        failedPending.remove(messageId)
        startAwaiting(agentId)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: message.plainText, attachments: message.attachments.map(\.id))
            } catch {
                failedPending.insert(messageId)
                stopAwaiting(agentId)
                report(error, agentId: agentId)
            }
        }
    }

    /// Discards a failed bubble instead of retrying it.
    func discardFailed(_ messageId: String, from agentId: String) {
        if let remaining = pending[agentId]?.filter({ $0.id != messageId }) {
            pending[agentId] = remaining.isEmpty ? nil : remaining
        }
        failedPending.remove(messageId)
    }

    /// Attachment bytes for thumbnails, full-screen images and QuickLook; cached for the session.
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        if let data = attachmentCache[attachmentId] { return data }
        let data = try await backend.attachmentData(agentId: agentId, attachmentId: attachmentId)
        if attachmentCache.count > 60 { attachmentCache.removeAll() }
        attachmentCache[attachmentId] = data
        return data
    }

    /// Uploaded bytes are known locally already; seeding the cache avoids a round trip for our own thumbnails.
    func cacheAttachment(_ id: String, data: Data) {
        attachmentCache[id] = data
    }

    /// System memory pressure: drop everything that isn't needed to keep the open chat on screen.
    /// Nothing is lost — `open(_:)` always reloads a chat's messages, so a background agent's
    /// transcript is just refetched if it's opened again.
    func handleMemoryWarning() {
        attachmentCache.removeAll()
        let keep = selectedAgentId
        state.messages = state.messages.filter { $0.key == keep }
        state.hasMore = state.hasMore.filter { $0.key == keep }
    }

    func interrupt(_ agentId: String) {
        stopAwaiting(agentId)
        Task {
            do { try await backend.sendKeys(agentId: agentId, keys: ["esc"]) } catch { report(error, agentId: agentId) }
        }
    }

    /// `text` answers a free-text option: its keys open the agent's input, then the text is typed and submitted.
    func answer(_ option: ApprovalOption, text: String? = nil) {
        guard let approval else { return }
        let typed = text?.trimmingCharacters(in: .whitespacesAndNewlines)
        isApprovalSheetPresented = false
        self.approval = nil
        recentlyAnswered[approval.agentId] = (Self.questionKey(approval), Date())
        Task {
            do {
                try await backend.sendKeys(agentId: approval.agentId, keys: option.keys)
                if let typed, !typed.isEmpty {
                    // The bridge paces the Enter itself; no delay needed here (api.md).
                    try await backend.sendText(agentId: approval.agentId, text: typed, submit: true)
                }
                await followUp(after: approval)
            } catch {
                // The answer never reached the agent (offline, bridge down), so it's still asking.
                // Bring the card back so it can be answered again; nothing else would, since the
                // status stays `blocked` and no change arrives to refetch it.
                recentlyAnswered[approval.agentId] = nil
                if selectedAgentId == approval.agentId, self.approval == nil { self.approval = approval }
                report(error, agentId: approval.agentId)
            }
        }
    }

    /// A multi-question prompt stays `blocked` between questions, so no status change will
    /// announce the next one. Poll briefly until the screen shows a different question or the block ends.
    private func followUp(after answered: Approval) async {
        for _ in 0..<12 {
            try? await Task.sleep(for: .milliseconds(400))
            guard selectedAgentId == answered.agentId, approval == nil else { return }
            guard let next = try? await backend.approval(agentId: answered.agentId) else { return }
            if !isRecentlyAnswered(next) {
                approval = next
                isApprovalSheetPresented = true
                return
            }
        }
    }

    enum CreateOutcome: Equatable {
        case created
        /// `409 not_installed` / `not_signed_in`: the bridge's hint, for an alert in the sheet (no banner).
        case kindNotReady(String)
        /// Anything else, already reported in the banner.
        case failed
    }

    /// Starts an agent with the model/effort picked in New chat (nil = its saved default) and opens its
    /// empty chat with the composer focused.
    func createAgent(workspaceId: String, kind: String, model: String?, effort: String?) async -> CreateOutcome {
        let request = CreateAgentRequest(workspaceId: workspaceId, kind: kind, model: model, effort: effort)
        do {
            let agent = try await backend.createAgent(request)
            state.upsert(agent)
            focusComposerFor = agent.id
            open(agent.id)
            return .created
        } catch let error as RelayError where error.kindNotReady != nil {
            return .kindNotReady(error.kindNotReady ?? "")
        } catch {
            report(error, agentId: workspaceId)
            return .failed
        }
    }

    /// `GET /kinds` on one machine, fresh each time (the bridge re-derives it). nil when unknown (an older
    /// bridge, or the machine is down): the sheet then lets every kind be picked and the bridge decides.
    func kinds(machineId: String) async -> [KindStatus]? {
        guard let c = connection(id: machineId) else { return nil }
        return try? await c.backend.kinds()
    }

    /// `GET /controls?kind=` on one machine for the New chat sheet, cached per machine and kind for the session.
    func kindControls(_ kind: String, machineId: String) async -> AgentControlsInfo? {
        guard let c = connection(id: machineId) else { return nil }
        if let cached = c.kindControls[kind] { return cached }
        guard let info = try? await c.backend.kindControls(kind: kind) else { return nil }
        c.kindControls[kind] = info
        return info
    }

    /// Older call site: the focus machine.
    func kindControls(_ kind: String) async -> AgentControlsInfo? {
        guard let id = focusConnection?.id else { return nil }
        return await kindControls(kind, machineId: id)
    }

    // MARK: - Controls

    /// Title-menu state for one agent: its own lists and supports plus any pending change.
    func controlsState(for agent: Agent) -> AgentControls {
        AgentControls(agent: agent, info: agentControls[agent.id], pending: pendingControls[agent.id])
    }

    /// Fetches the agent's controls unless we already have them for its current kind, session and model.
    /// Bridges without the per-agent route (404) fall back to Claude's global list for Claude agents.
    func loadAgentControls(_ agentId: String, force: Bool = false) async {
        guard let agent = state.agent(agentId) else { return }
        // Effort levels depend on the current model (pi, codex), so a model change refetches too.
        let key = "\(agent.kind)|\(agent.sessionId ?? "")|\(agent.model ?? "")"
        guard force || agentControlsKey[agentId] != key || agentControls[agentId] == nil else { return }
        do {
            agentControls[agentId] = try await backend.agentControls(agentId: agentId)
            agentControlsKey[agentId] = key
        } catch RelayError.http(status: 404, _, _) {
            if agent.kind == "claude", let catalog = connection(for: agentId)?.controls {
                agentControls[agentId] = AgentControlsInfo(claudeCatalog: catalog)
                agentControlsKey[agentId] = key
            }
        } catch {
            // No controls for now; the pill still shows the model from the agent.
        }
    }

    /// Model, mode, effort, /compact or /clear. One change per agent at a time.
    func control(_ request: ControlRequest, for agentId: String) {
        guard pendingControls[agentId] == nil else { return }
        pendingControls[agentId] = request
        Task {
            do {
                let agent = try await backend.control(agentId: agentId, request)
                state.upsert(agent)
                reloadIfDropped(agent.id)
                // pi resets effort on a model switch and the effort list follows the model.
                await loadAgentControls(agent.id)
                if case .command(.compact) = request { await loadMessages(agentId) }
            } catch {
                report(error, prefix: ControlDisplay.failurePrefix(request, info: agentControls[agentId]), agentId: agentId)
            }
            pendingControls[agentId] = nil
        }
    }

    /// A new session (after /clear) drops the cached chat; refetch it if it's open.
    private func reloadIfDropped(_ agentId: String) {
        guard agentId == selectedAgentId, !isLoaded(agentId) else { return }
        Task { await loadMessages(agentId) }
    }

    // MARK: - Approval

    func refreshApproval() async {
        guard let agent = selectedAgent, agent.status == .blocked else {
            approval = nil
            isApprovalSheetPresented = false
            return
        }
        do {
            var fetched = try await backend.approval(agentId: agent.id)
            guard selectedAgentId == agent.id else { return }
            // The dialog we just answered can linger on screen for a moment; don't bring it back.
            if let f = fetched, isRecentlyAnswered(f) { fetched = nil }
            let isNew = fetched.map(Self.questionKey) != nil && fetched.map(Self.questionKey) != approval.map(Self.questionKey)
            approval = fetched
            if isNew && !LaunchOptions.current.isDemo("card") { isApprovalSheetPresented = true }
        } catch {
            report(error, agentId: agent.id)
        }
    }

    /// The question last answered per agent. Keys can't identify a question: for codex and cursor menus
    /// they're arrow moves relative to the cursor, so the same dialog re-read after a key press differs.
    private var recentlyAnswered: [String: (key: String, at: Date)] = [:]

    private static func questionKey(_ approval: Approval) -> String {
        "\(approval.question)\u{1F}\(approval.step.map { "\($0.index)/\($0.count)" } ?? "")"
    }

    private func isRecentlyAnswered(_ approval: Approval) -> Bool {
        guard let last = recentlyAnswered[approval.agentId] else { return false }
        return last.key == Self.questionKey(approval) && Date().timeIntervalSince(last.at) < 15
    }

    // MARK: - Helpers

    private func startAwaiting(_ agentId: String) {
        let since = Date()
        awaitingReply[agentId] = since
        Task {
            try? await Task.sleep(for: Self.awaitingTimeout)
            // Never picked up (or the status change was missed): stop claiming it's thinking.
            if awaitingReply[agentId] == since { awaitingReply[agentId] = nil }
        }
    }

    private func stopAwaiting(_ agentId: String) {
        if awaitingReply[agentId] != nil { awaitingReply[agentId] = nil }
    }

    private func resolvePending(agentId: String, with messages: [Message]) {
        guard var list = pending[agentId], !list.isEmpty else { return }
        // Stop markers are Claude's, not the user's prompt; never let one resolve a pending prompt.
        let keys = Set(messages.filter { $0.role == .user && !$0.isInterruptionMarker }.map(Self.pendingKey))
        let resolved = list.filter { keys.contains(Self.pendingKey($0)) }
        list.removeAll { keys.contains(Self.pendingKey($0)) }
        pending[agentId] = list.isEmpty ? nil : list
        for message in resolved { failedPending.remove(message.id) }
    }

    /// Stopgap for agents without a transcript (`unsupported`, screen-read fallback): nothing will ever echo a prompt
    /// back, so its pending bubble would stay forever. Drop it once the agent finishes the turn.
    private func dropPendingIfFinished(_ agent: Agent, previous: AgentStatus?) {
        guard agent.transcript == .unsupported, previous == .working,
              agent.status == .idle || agent.status == .done,
              let dropped = pending[agent.id]
        else { return }
        pending[agent.id] = nil
        for message in dropped { failedPending.remove(message.id) }
    }

    /// Same text and same attachment ids: the transcript copy of a prompt sent from the phone.
    private static func pendingKey(_ message: Message) -> String {
        message.plainText.trimmingCharacters(in: .whitespacesAndNewlines) + "\u{1F}" + message.attachments.map(\.id).sorted().joined(separator: ",")
    }

    private func markSeen(_ agent: Agent) {
        seen[agent.id] = agent.updatedAt
        AppDefaults.standard.set(seen.mapValues(\.timeIntervalSince1970), forKey: "seenAgents")
    }

    /// `agentId` or `machine` says whose bridge failed, so a rejected token marks only that machine.
    /// `quiet` (background resyncs): an unreachable machine just shows as offline, with no banner.
    private func report(_ error: any Error, prefix: String? = nil, agentId: String? = nil, machine: MachineConnection? = nil, quiet: Bool = false) {
        if error is CancellationError { return }
        if let url = error as? URLError, url.code == .cancelled { return }
        let c = machine ?? agentId.flatMap(connection(for:))
        if case .unauthorized = error as? RelayError {
            c?.status = .needsRePair
        } else if case .unreachable = error as? RelayError, let c, c.status != .needsRePair {
            c.status = .offline
            if quiet { return }
        }
        // Only RelayError's curated copy (and the bridge's own `message`, api.md) ever reaches the UI:
        // anything else (a decode failure that slipped past RelayKit, etc.) gets a generic message
        // instead of raw Swift/Foundation error text.
        let text = (error as? RelayError)?.errorDescription ?? "Something went wrong. Try again."
        errorMessage = prefix.map { "\($0): \(text)" } ?? text
    }
}

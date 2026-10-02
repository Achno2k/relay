import Foundation
import Observation
import RelayKit

/// Re-derived per machine, never stored (api.md "Multiple machines").
enum MachineStatus: Equatable, Sendable {
    /// First attempt, or reconnecting after a drop.
    case connecting
    /// `/ws` connected.
    case online
    /// Unreachable.
    case offline
    /// `401`, or `/machine` now answers another id.
    case needsRePair
}

/// One paired machine as the UI sees it. See docs/tasks/round-9/interfaces.md.
struct MachineEntry: Identifiable, Equatable, Sendable {
    let id: String
    /// Local label, else the machine's own name, else the URL host.
    var displayName: String
    var label: String?
    /// Last `GET /machine`.
    var machine: Machine?
    /// `/health` version.
    var bridgeVersion: String?
    /// `/health` herdr state; nil until known.
    var herdrAvailable: Bool?
    var host: String
    var status: MachineStatus

    var name: String { displayName }
    var kind: Machine.Kind? { machine?.kind }
    var os: String? { machine?.os }
    var isOnline: Bool { status == .online }
}

/// One bridge: its backend, socket loop state and status. `AppStore` drives it; each machine reconnects on
/// its own, so one being down never holds up another.
@MainActor
@Observable
final class MachineConnection {
    private(set) var record: MachineRecord
    /// The bridge as it speaks: raw ids.
    private(set) var raw: any Backend
    /// False only for the single-backend store unit tests and previews use: ids stay raw.
    let keyed: Bool
    var status: MachineStatus = .connecting
    /// The old per-socket state; feeds the "Reconnecting…" banner.
    var connection: ConnectionState = .connecting
    var health: Health?
    /// Claude's `GET /controls` list from this bridge (fallback for bridges without per-agent controls).
    var controls: ControlsCatalog?
    @ObservationIgnored var kindControls: [String: AgentControlsInfo] = [:]

    @ObservationIgnored var eventsTask: Task<Void, Never>?
    @ObservationIgnored var suspended = false
    @ObservationIgnored var resyncOnConnect = false
    @ObservationIgnored var refreshGeneration = 0
    /// Set on `hello`, cleared on a drop: a drop from online reads `connecting`, a failed retry `offline`.
    @ObservationIgnored var wasOnline = false
    /// A refresh finished (either way), so a bare `-agent` id can be resolved in pair order.
    @ObservationIgnored var hasAttempted = false

    init(record: MachineRecord, raw: any Backend, keyed: Bool = true) {
        self.record = record
        self.raw = raw
        self.keyed = keyed
    }

    var id: String { record.id }

    /// The backend the store uses: keyed ids in and out.
    var backend: any Backend { keyed ? NamespacedBackend(machineId: id, inner: raw) : raw }

    /// Whether an agent or workspace key is this machine's.
    func owns(_ key: String) -> Bool {
        keyed ? MachineKey.belongs(key, to: id) : MachineKey.machineId(key) == nil
    }

    func key(_ rawId: String) -> String { keyed ? MachineKey.make(id, rawId) : rawId }

    func replace(record: MachineRecord) { self.record = record }

    func replace(raw: any Backend) { self.raw = raw }

    var entry: MachineEntry {
        let host = record.url.host() ?? record.url.absoluteString
        let label = record.label.flatMap { $0.isEmpty ? nil : $0 }
        return MachineEntry(
            id: id,
            displayName: label ?? record.machine?.name ?? host,
            label: label,
            machine: record.machine,
            bridgeVersion: health?.version ?? record.bridgeVersion,
            herdrAvailable: health?.herdrAvailable,
            host: host,
            status: status
        )
    }
}

/// Sends each per-agent call to the bridge of the agent's machine (by key). What `store.backend` hands the UI
/// (uploads); machine-wide calls (`agents()`, `events()`) aren't meant for it.
struct RoutingBackend: Backend {
    let table: [String: any Backend]
    /// The unkeyed single machine (tests), for bare ids.
    let fallback: (any Backend)?

    func route(_ key: String) throws -> any Backend {
        if let id = MachineKey.machineId(key), let backend = table[id] { return backend }
        if MachineKey.machineId(key) == nil, let fallback { return fallback }
        throw RelayError.http(status: 404, code: "not_found", message: "That machine isn't paired.")
    }

    private var any: (any Backend)? { fallback ?? table.values.first }
    private func first() throws -> any Backend {
        guard let any else { throw RelayError.http(status: 404, code: "not_found", message: "No machine is paired.") }
        return any
    }

    func workspaces() async throws -> [Workspace] { try await first().workspaces() }
    func agents() async throws -> [Agent] { try await first().agents() }
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try await route(agentId).messages(agentId: agentId, before: before, limit: limit)
    }
    func prompt(agentId: String, text: String, attachments: [String]) async throws {
        try await route(agentId).prompt(agentId: agentId, text: text, attachments: attachments)
    }
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment {
        try await route(agentId).uploadAttachment(agentId: agentId, data: data, filename: filename, contentType: contentType, progress: progress)
    }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        try await route(agentId).attachmentData(agentId: agentId, attachmentId: attachmentId)
    }
    func toolImage(agentId: String, toolCallId: String, index: Int) async throws -> Data {
        try await route(agentId).toolImage(agentId: agentId, toolCallId: toolCallId, index: index)
    }
    func file(agentId: String, path: String) async throws -> AgentFile {
        try await route(agentId).file(agentId: agentId, path: path)
    }
    func changes(agentId: String) async throws -> Changes { try await route(agentId).changes(agentId: agentId) }
    func changesDiff(agentId: String, path: String) async throws -> FileDiffText {
        try await route(agentId).changesDiff(agentId: agentId, path: path)
    }
    func commit(agentId: String, sha: String) async throws -> CommitDetail { try await route(agentId).commit(agentId: agentId, sha: sha) }
    func machine() async throws -> Machine { try await first().machine() }
    func sendKeys(agentId: String, keys: [String]) async throws { try await route(agentId).sendKeys(agentId: agentId, keys: keys) }
    func sendText(agentId: String, text: String, submit: Bool) async throws {
        try await route(agentId).sendText(agentId: agentId, text: text, submit: submit)
    }
    func approval(agentId: String) async throws -> Approval? { try await route(agentId).approval(agentId: agentId) }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent { try await route(request.workspaceId).createAgent(request) }
    func controls() async throws -> ControlsCatalog { try await first().controls() }
    func kindControls(kind: String) async throws -> AgentControlsInfo { try await first().kindControls(kind: kind) }
    func kinds() async throws -> [KindStatus] { try await first().kinds() }
    func agentControls(agentId: String) async throws -> AgentControlsInfo { try await route(agentId).agentControls(agentId: agentId) }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        try await route(agentId).control(agentId: agentId, request)
    }
    func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }
    func usage() async throws -> UsageSnapshot { try await first().usage() }
    func refreshUsage() async throws { try await first().refreshUsage() }
}

/// UI state saved under agent and workspace keys (AppDefaults). Moves bare pre-round-9 ids under the migrated
/// machine, and drops a removed machine's keys.
enum PersistedKeys {
    static let selected = "selectedAgentId"
    static let seen = "seenAgents"
    static let archived = "archivedAgents"
    static let machineFilter = "machineFilter"
    /// Comma-separated workspace keys (`ExpansionSet`).
    static let expansionSets = ["sidebarCompletedOpen", "sidebarProjectsOpen"]

    /// Bare ids → `<machineId>/<id>`. Keys that already have a machine part are left alone.
    static func migrateBare(to machineId: String, standard: UserDefaults, shared: UserDefaults) {
        rewrite(standard: standard, shared: shared) { key in
            MachineKey.machineId(key) == nil ? MachineKey.make(machineId, key) : key
        }
    }

    /// Drops every key of `machineId`.
    static func drop(machineId: String, standard: UserDefaults, shared: UserDefaults) {
        rewrite(standard: standard, shared: shared) { key in
            MachineKey.belongs(key, to: machineId) ? nil : key
        }
        if standard.string(forKey: machineFilter) == machineId { standard.removeObject(forKey: machineFilter) }
    }

    /// Applies `change` to every saved key; nil removes it.
    private static func rewrite(standard: UserDefaults, shared: UserDefaults, _ change: (String) -> String?) {
        if let id = standard.string(forKey: selected) {
            if let new = change(id) { standard.set(new, forKey: selected) } else { standard.removeObject(forKey: selected) }
        }
        if let dict = standard.dictionary(forKey: seen) as? [String: Double] {
            var out: [String: Double] = [:]
            for (k, v) in dict { if let new = change(k) { out[new] = max(v, out[new] ?? 0) } }
            standard.set(out, forKey: seen)
        }
        if let list = shared.stringArray(forKey: archived) {
            shared.set(Array(Set(list.compactMap(change))).sorted(), forKey: archived)
        }
        for name in expansionSets {
            guard let raw = standard.string(forKey: name) else { continue }
            let ids = raw.split(separator: ",").map(String.init).compactMap(change)
            standard.set(Array(Set(ids)).sorted().joined(separator: ","), forKey: name)
        }
    }
}

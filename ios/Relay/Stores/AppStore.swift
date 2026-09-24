import Foundation
import HerdKit
import Observation

enum ConnectionState: Equatable {
    case connecting, connected, reconnecting
}

/// Owns the backend, the reduced state and the chat that's open.
@MainActor
@Observable
final class AppStore {
    let backend: any Backend
    /// Shown in the sidebar footer.
    let hostLabel: String

    private(set) var state = HerdState()
    private(set) var connection: ConnectionState = .connecting
    /// False until the first `/agents` answer (or failure), so the UI doesn't flash "No agents".
    private(set) var hasLoadedAgents = false
    var selectedAgentId: String? {
        didSet { AppDefaults.standard.set(selectedAgentId, forKey: "selectedAgentId") }
    }

    /// Claude's global list from `GET /controls`; only used when the bridge has no per-agent route.
    private(set) var controls: ControlsCatalog?
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
    /// First/last time this session saw each message grow over the socket; feeds "Worked for 42s".
    private(set) var liveSpans: [String: ClosedRange<Date>] = [:]
    private var seen: [String: Date]
    /// Client-side archive (App Group defaults); it never closes the agent.
    private(set) var archived: Set<String>
    /// The Macs we know about. One bridge serves one machine; a list so more can plug in later.
    private(set) var machines: [Machine] = []
    var filter: SessionFilter {
        didSet { AppDefaults.standard.set(filter.rawValue, forKey: "sessionFilter") }
    }
    /// Thumbnails and files already fetched, by attachment id.
    private var attachmentCache: [String: Data] = [:]
    var errorMessage: String?

    private var eventsTask: Task<Void, Never>?

    init(backend: any Backend, hostLabel: String) {
        self.backend = backend
        self.hostLabel = hostLabel
        self.selectedAgentId = AppDefaults.standard.string(forKey: "selectedAgentId")
        let raw = AppDefaults.standard.dictionary(forKey: "seenAgents") as? [String: Double] ?? [:]
        self.seen = raw.mapValues { Date(timeIntervalSince1970: $0) }
        self.archived = Set(AppDefaults.shared.stringArray(forKey: "archivedAgents") ?? [])
        self.filter = SessionFilter(rawValue: AppDefaults.standard.string(forKey: "sessionFilter") ?? "") ?? .all
    }

    var sidebar: SidebarModel {
        SidebarModel(state: state, archived: archived, isUnseen: { [seen, selectedAgentId] agent in
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

    func messages(for agentId: String) -> [Message] {
        (state.messages[agentId] ?? []) + (pending[agentId] ?? [])
    }

    func isLoaded(_ agentId: String) -> Bool { state.messages[agentId] != nil }

    func hasMore(_ agentId: String) -> Bool { state.hasMore[agentId] ?? false }

    func isUnseen(_ agent: Agent) -> Bool {
        agent.status == .done && agent.id != selectedAgentId && (seen[agent.id] ?? .distantPast) < agent.updatedAt
    }

    // MARK: - Lifecycle

    func start() {
        guard eventsTask == nil else { return }
        let stream = backend.events()
        eventsTask = Task { [weak self] in
            for await event in stream {
                guard let self else { return }
                await self.handle(event)
            }
        }
        Task { await refresh() }
    }

    func stop() {
        eventsTask?.cancel()
        eventsTask = nil
    }

    private func handle(_ event: ConnectionEvent) async {
        switch event {
        case .connected:
            let wasDown = connection == .reconnecting
            connection = .connected
            // The socket carries deltas only; resync after every (re)connect.
            if wasDown { await refresh() }
        case .disconnected:
            connection = .reconnecting
        case .event(let e):
            apply(e)
        }
    }

    func apply(_ event: ServerEvent) {
        let before = selectedAgent?.status
        if case .agentUpdated(let updated) = event {
            dropPendingIfFinished(updated, previous: state.agent(updated.id)?.status)
        }
        state.apply(event)
        switch event {
        case .messageUpserted(let agentId, let message):
            let now = Date()
            let start = min(liveSpans[message.id]?.lowerBound ?? message.createdAt, now)
            liveSpans[message.id] = start...now
            if message.role == .user { resolvePending(agentId: agentId, with: [message]) }
        case .agentClosed(let id):
            pending[id] = nil
            if id == selectedAgentId { selectedAgentId = firstAgentId() }
        case .agentUpdated(let agent) where agent.id == selectedAgentId:
            unarchiveIfBlocked([agent])
            markSeen(agent)
            reloadIfDropped(agent.id)
            Task { await loadAgentControls(agent.id) }
        case .agentUpdated(let agent), .agentCreated(let agent):
            unarchiveIfBlocked([agent])
        default:
            break
        }
        let after = selectedAgent?.status
        if before != after { Task { await refreshApproval() } }
    }

    /// Full resync: agents, workspaces, then the open chat.
    func refresh() async {
        do {
            async let agents = backend.agents()
            async let workspaces = backend.workspaces()
            let (a, w) = try await (agents, workspaces)
            var s = state
            for agent in a { s.upsert(agent) }  // drops chats whose session changed
            s.agents = a
            unarchiveIfBlocked(a)
            s.workspaces = w
            let live = Set(a.map(\.id))
            s.messages = s.messages.filter { live.contains($0.key) }
            state = s
            if selectedAgentId.map({ !live.contains($0) }) ?? true {
                selectedAgentId = firstAgentId()
            }
            if connection == .connecting { connection = .connected }
        } catch {
            report(error)
        }
        hasLoadedAgents = true
        if controls == nil { controls = try? await backend.controls() }
        if let id = selectedAgentId { await loadAgentControls(id) }
        if let machine = try? await backend.machine() {
            machines = [machine]
        }
        if let id = selectedAgentId { await loadMessages(id) }
        await refreshApproval()
    }

    private func firstAgentId() -> String? {
        state.sections().first?.agents.first?.id
    }

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
        do {
            let page = try await backend.messages(agentId: agentId, before: nil, limit: 50)
            state.setPage(page, agentId: agentId)
            resolvePending(agentId: agentId, with: page.messages)
        } catch {
            report(error)
        }
    }

    func loadEarlier(_ agentId: String) async {
        guard hasMore(agentId), let first = state.messages[agentId]?.first else { return }
        do {
            let page = try await backend.messages(agentId: agentId, before: first.id, limit: 50)
            state.prependPage(page, agentId: agentId)
        } catch {
            report(error)
        }
    }

    func send(_ text: String, attachments: [Attachment] = [], to agentId: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty || !attachments.isEmpty else { return }
        var blocks = attachments.map { Block.attachment($0.ref) }
        if !trimmed.isEmpty { blocks.append(.text(trimmed)) }
        let local = Message(id: "local-\(UUID().uuidString)", role: .user, createdAt: Date(), blocks: blocks)
        pending[agentId, default: []].append(local)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: trimmed, attachments: attachments.map(\.id))
            } catch {
                pending[agentId]?.removeAll { $0.id == local.id }
                report(error)
            }
        }
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

    func interrupt(_ agentId: String) {
        Task {
            do { try await backend.sendKeys(agentId: agentId, keys: ["esc"]) } catch { report(error) }
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
                report(error)
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

    func createAgent(workspaceId: String, kind: String, prompt: String?) async -> Bool {
        let text = prompt?.trimmingCharacters(in: .whitespacesAndNewlines)
        let request = CreateAgentRequest(workspaceId: workspaceId, kind: kind, prompt: text?.isEmpty == false ? text : nil)
        do {
            let agent = try await backend.createAgent(request)
            state.upsert(agent)
            open(agent.id)
            return true
        } catch {
            report(error)
            return false
        }
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
        } catch HerdError.http(status: 404, _, _) {
            if agent.kind == "claude", let catalog = controls {
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
                report(error, prefix: ControlDisplay.failurePrefix(request, info: agentControls[agentId]))
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
            report(error)
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

    private func resolvePending(agentId: String, with messages: [Message]) {
        guard var list = pending[agentId], !list.isEmpty else { return }
        // Stop markers are Claude's, not the user's prompt; never let one resolve a pending prompt.
        let keys = Set(messages.filter { $0.role == .user && !$0.isInterruptionMarker }.map(Self.pendingKey))
        list.removeAll { keys.contains(Self.pendingKey($0)) }
        pending[agentId] = list.isEmpty ? nil : list
    }

    /// Stopgap for agents without a transcript (screen-read fallback): nothing will ever echo a prompt
    /// back, so its pending bubble would stay forever. Drop it once the agent finishes the turn.
    private func dropPendingIfFinished(_ agent: Agent, previous: AgentStatus?) {
        guard !agent.hasTranscript, previous == .working,
              agent.status == .idle || agent.status == .done,
              pending[agent.id] != nil
        else { return }
        pending[agent.id] = nil
    }

    /// Same text and same attachment ids: the transcript copy of a prompt sent from the phone.
    private static func pendingKey(_ message: Message) -> String {
        message.plainText.trimmingCharacters(in: .whitespacesAndNewlines) + "\u{1F}" + message.attachments.map(\.id).sorted().joined(separator: ",")
    }

    private func markSeen(_ agent: Agent) {
        seen[agent.id] = agent.updatedAt
        AppDefaults.standard.set(seen.mapValues(\.timeIntervalSince1970), forKey: "seenAgents")
    }

    private func report(_ error: any Error, prefix: String? = nil) {
        if error is CancellationError { return }
        if let url = error as? URLError, url.code == .cancelled { return }
        errorMessage = prefix.map { "\($0): \(error.localizedDescription)" } ?? error.localizedDescription
    }
}

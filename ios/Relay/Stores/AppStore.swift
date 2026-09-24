import Foundation
import RelayKit
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

    private(set) var state = RelayState()
    private(set) var connection: ConnectionState = .connecting
    /// False until the first `/agents` answer (or failure), so the UI doesn't flash "No agents".
    private(set) var hasLoadedAgents = false
    var selectedAgentId: String? {
        didSet { AppDefaults.standard.set(selectedAgentId, forKey: "selectedAgentId") }
    }

    /// Claude's global list from `GET /controls`; only used when the bridge has no per-agent route.
    private(set) var controls: ControlsCatalog?
    private var kindControlsCache: [String: AgentControlsInfo] = [:]
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
    /// First/last time this session saw each message grow over the socket; feeds "Worked for 42s".
    private(set) var liveSpans: [String: ClosedRange<Date>] = [:]
    /// In-progress assistant text preview from `reply.live`, keyed by agent id; never persisted.
    private(set) var liveReplyText: [String: String] = [:]
    private var liveReplySeq: [String: Int] = [:]
    private var seen: [String: Date]
    /// Client-side archive (App Group defaults); it never closes the agent.
    private(set) var archived: Set<String>
    /// The Macs we know about. One bridge serves one machine; a list so more can plug in later.
    private(set) var machines: [Machine] = []
    var filter: SessionFilter {
        didSet { AppDefaults.standard.set(filter.rawValue, forKey: "sessionFilter") }
    }
    /// The bridge rejected the token (rotated on the Mac, or a stale pairing). Sticky until a request
    /// succeeds again or the person re-pairs; never auto-dismissed like a transient error.
    private(set) var needsRePairing = false
    /// Thumbnails and files already fetched, by attachment id.
    private var attachmentCache: [String: Data] = [:]
    var errorMessage: String?

    private var eventsTask: Task<Void, Never>?
    /// Bumped every time a message load starts for an agent, so a reply from an earlier, slower
    /// request (a rapid agent switch, or a reconnect racing an `open()`) can't overwrite a newer one.
    private var messagesRequest: [String: Int] = [:]
    /// Bumped every time a full resync starts, so an overlapping `refresh()` (foreground and a
    /// reconnect can both trigger one at once) can't apply its agents/workspaces snapshot out of order.
    private var refreshGeneration = 0

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
        case .replyLive(let agentId, let text, let seq):
            guard seq > (liveReplySeq[agentId] ?? 0) else { break }
            liveReplySeq[agentId] = seq
            liveReplyText[agentId] = text
        case .agentClosed(let id):
            for message in pending[id] ?? [] { failedPending.remove(message.id) }
            pending[id] = nil
            liveReplyText[id] = nil
            liveReplySeq[id] = nil
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
        refreshGeneration += 1
        let generation = refreshGeneration
        do {
            async let agents = backend.agents()
            async let workspaces = backend.workspaces()
            let (a, w) = try await (agents, workspaces)
            guard refreshGeneration == generation else { return }  // a newer refresh already landed
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
            needsRePairing = false
        } catch {
            guard refreshGeneration == generation else { return }
            report(error)
        }
        guard refreshGeneration == generation else { return }
        hasLoadedAgents = true
        // Machine info doesn't depend on the selected agent, so fetch it alongside the open chat's
        // controls/messages/approval instead of one round trip after another: on a slow Tailscale
        // link that's the difference between the chat reappearing in ~1s after a long background
        // and several seconds. (Claude's catalog stays ahead of `loadAgentControls`, which falls
        // back to it for older bridges with no per-agent route.)
        async let machineFetch: Machine? = try? await backend.machine()
        if controls == nil { controls = try? await backend.controls() }
        if let id = selectedAgentId {
            async let controlsLoad: Void = loadAgentControls(id)
            async let messagesLoad: Void = loadMessages(id)
            async let approvalLoad: Void = refreshApproval()
            _ = await (controlsLoad, messagesLoad, approvalLoad)
        } else {
            await refreshApproval()
        }
        guard refreshGeneration == generation else { return }
        if let machine = await machineFetch { machines = [machine] }
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
        messagesRequest[agentId, default: 0] += 1
        let generation = messagesRequest[agentId]
        do {
            let page = try await backend.messages(agentId: agentId, before: nil, limit: 50)
            guard messagesRequest[agentId] == generation else { return }  // superseded by a newer load
            state.setPage(page, agentId: agentId)
            resolvePending(agentId: agentId, with: page.messages)
        } catch {
            guard messagesRequest[agentId] == generation else { return }
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
        // A double tap on send (or a stuck composer) can re-fire with the same text while the first
        // send is still in flight; a matching, still-live pending bubble from the last couple of
        // seconds means this is that duplicate, not a genuine repeat message.
        if let lastAt = lastSendAt[agentId], Date().timeIntervalSince(lastAt) < 2,
           let last = pending[agentId]?.last, !failedPending.contains(last.id), Self.pendingKey(last) == Self.pendingKey(local) {
            return
        }
        lastSendAt[agentId] = Date()
        pending[agentId, default: []].append(local)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: trimmed, attachments: attachments.map(\.id))
            } catch {
                // Keep the bubble and flag it, so a failed send is visible and retryable rather than
                // silently vanishing from the transcript (only the transient error banner would remain).
                failedPending.insert(local.id)
                report(error)
            }
        }
    }

    /// Resends a bubble that failed; e.g. a send attempted while reconnecting.
    func retry(_ messageId: String, to agentId: String) {
        guard let message = pending[agentId]?.first(where: { $0.id == messageId }) else { return }
        failedPending.remove(messageId)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: message.plainText, attachments: message.attachments.map(\.id))
            } catch {
                failedPending.insert(messageId)
                report(error)
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

    /// Starts an agent with the model/effort picked in New chat (nil = its saved default) and opens its
    /// empty chat with the composer focused.
    func createAgent(workspaceId: String, kind: String, model: String?, effort: String?) async -> Bool {
        let request = CreateAgentRequest(workspaceId: workspaceId, kind: kind, model: model, effort: effort)
        do {
            let agent = try await backend.createAgent(request)
            state.upsert(agent)
            focusComposerFor = agent.id
            open(agent.id)
            return true
        } catch {
            report(error)
            return false
        }
    }

    /// `GET /controls?kind=` for the New chat sheet, cached per kind for the session.
    func kindControls(_ kind: String) async -> AgentControlsInfo? {
        if let cached = kindControlsCache[kind] { return cached }
        guard let info = try? await backend.kindControls(kind: kind) else { return nil }
        kindControlsCache[kind] = info
        return info
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

    private func report(_ error: any Error, prefix: String? = nil) {
        if error is CancellationError { return }
        if let url = error as? URLError, url.code == .cancelled { return }
        // Only RelayError's curated copy (and the bridge's own `message`, api.md) ever reaches the UI:
        // anything else (a decode failure that slipped past RelayKit, etc.) gets a generic message
        // instead of raw Swift/Foundation error text.
        let text = (error as? RelayError)?.errorDescription ?? "Something went wrong. Try again."
        errorMessage = prefix.map { "\($0): \(text)" } ?? text
        if case .unauthorized = error as? RelayError { needsRePairing = true }
    }
}

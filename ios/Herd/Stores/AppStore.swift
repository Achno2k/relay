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
        didSet { UserDefaults.standard.set(selectedAgentId, forKey: "selectedAgentId") }
    }

    /// Approval for the open agent, fetched when it turns blocked.
    private(set) var approval: Approval?
    var isApprovalSheetPresented = false

    /// Prompts sent from the phone that haven't appeared in the transcript yet.
    private(set) var pending: [String: [Message]] = [:]
    /// First/last time this session saw each message grow over the socket; feeds "Worked for 42s".
    private(set) var liveSpans: [String: ClosedRange<Date>] = [:]
    private var seen: [String: Date]
    var errorMessage: String?

    private var eventsTask: Task<Void, Never>?

    init(backend: any Backend, hostLabel: String) {
        self.backend = backend
        self.hostLabel = hostLabel
        self.selectedAgentId = UserDefaults.standard.string(forKey: "selectedAgentId")
        let raw = UserDefaults.standard.dictionary(forKey: "seenAgents") as? [String: Double] ?? [:]
        self.seen = raw.mapValues { Date(timeIntervalSince1970: $0) }
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
            markSeen(agent)
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
            s.agents = a
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

    func send(_ text: String, to agentId: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        let local = Message(id: "local-\(UUID().uuidString)", role: .user, createdAt: Date(), blocks: [.text(trimmed)])
        pending[agentId, default: []].append(local)
        Task {
            do {
                try await backend.prompt(agentId: agentId, text: trimmed)
            } catch {
                pending[agentId]?.removeAll { $0.id == local.id }
                report(error)
            }
        }
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
            if next != answered {
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

    // MARK: - Approval

    func refreshApproval() async {
        guard let agent = selectedAgent, agent.status == .blocked else {
            approval = nil
            isApprovalSheetPresented = false
            return
        }
        do {
            let fetched = try await backend.approval(agentId: agent.id)
            guard selectedAgentId == agent.id else { return }
            let isNew = fetched != nil && fetched != approval
            approval = fetched
            if isNew && !LaunchOptions.current.isDemo("card") { isApprovalSheetPresented = true }
        } catch {
            report(error)
        }
    }

    // MARK: - Helpers

    private func resolvePending(agentId: String, with messages: [Message]) {
        guard var list = pending[agentId], !list.isEmpty else { return }
        let texts = Set(messages.filter { $0.role == .user }.map { $0.plainText.trimmingCharacters(in: .whitespacesAndNewlines) })
        list.removeAll { texts.contains($0.plainText) }
        pending[agentId] = list.isEmpty ? nil : list
    }

    private func markSeen(_ agent: Agent) {
        seen[agent.id] = agent.updatedAt
        UserDefaults.standard.set(seen.mapValues(\.timeIntervalSince1970), forKey: "seenAgents")
    }

    private func report(_ error: any Error) {
        if error is CancellationError { return }
        if let url = error as? URLError, url.code == .cancelled { return }
        errorMessage = error.localizedDescription
    }
}

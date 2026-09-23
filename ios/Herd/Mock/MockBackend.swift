#if DEBUG
import Foundation
import HerdKit

/// Serves `docs/fixtures` (copied into Debug builds as `Fixtures/`) and replays `ws-events.jsonl`.
/// It also reacts to prompts, stop and approvals so the app can be driven without a bridge.
/// Every chat besides the fixture one is made up here; nothing comes from a real machine.
actor MockBackend: Backend {
    private var agentList: [Agent]
    private var workspaceList: [Workspace]
    private var chats: [String: [Message]]
    private var approvals: [String: Approval]
    private let replay: [ServerEvent]
    private let replayInterval: Duration?
    private var continuation: AsyncStream<ConnectionEvent>.Continuation?
    private var counter = 0
    private let latency: Duration = .milliseconds(UserDefaults.standard.integer(forKey: "latency").nonZero ?? 120)

    init(replayInterval: Duration? = .seconds(4)) {
        let fixtures = Fixtures()
        agentList = fixtures.agents
        workspaceList = fixtures.workspaces
        approvals = fixtures.approval.map { [$0.agentId: Self.extended($0)] } ?? [:]
        replay = fixtures.events
        self.replayInterval = replayInterval
        chats = MockChats.all(fixtureMessages: fixtures.messages)
    }

    func workspaces() async throws -> [Workspace] {
        workspaceList.map { w in
            var w = w
            w.agentCount = agentList.filter { $0.workspaceId == w.id }.count
            return w
        }
    }

    func agents() async throws -> [Agent] { agentList }

    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try await Task.sleep(for: latency)
        var list = chats[agentId] ?? []
        if let before, let i = list.firstIndex(where: { $0.id == before }) { list = Array(list[..<i]) }
        return MessagePage(messages: Array(list.suffix(limit)), hasMore: list.count > limit)
    }

    func prompt(agentId: String, text: String) async throws {
        let user = Message(id: nextId(), role: .user, createdAt: Date(), blocks: [.text(text)])
        push(user, to: agentId)
        setStatus(.working, for: agentId)
        let replyId = nextId()
        Task {
            try? await Task.sleep(for: .seconds(1.2))
            let call = ToolCall(id: nextId(), name: "Read", summary: "Read README.md")
            var reply = Message(id: replyId, role: .assistant, createdAt: Date(), blocks: [.toolCall(call)])
            push(reply, to: agentId)
            try? await Task.sleep(for: .seconds(1.5))
            reply.blocks.append(.toolResult(ToolResult(toolCallId: call.id, isError: false, preview: "# Project")))
            reply.blocks.append(.text("This is the mock backend. On a real bridge, **\(agentId)** would answer here."))
            push(reply, to: agentId)
            setStatus(.idle, for: agentId)
        }
    }

    func sendKeys(agentId: String, keys: [String]) async throws {
        guard let agent = agentList.first(where: { $0.id == agentId }) else { return }
        if agent.status == .blocked {
            approvals[agentId] = nil
            setStatus(keys == ["esc"] ? .idle : .working, for: agentId)
        } else if keys == ["esc"] {
            setStatus(.idle, for: agentId)
        }
    }

    func sendText(agentId: String, text: String, submit: Bool) async throws {
        guard submit else { return }
        let reply = Message(id: nextId(), role: .assistant, createdAt: Date(), blocks: [.text("Got it: \(text)")])
        Task {
            try? await Task.sleep(for: .seconds(0.8))
            push(reply, to: agentId)
            setStatus(.idle, for: agentId)
        }
    }

    /// The fixture approval plus what newer bridges send: a step header and Claude's free-text row.
    private static func extended(_ approval: Approval) -> Approval {
        var a = approval
        a.step = a.step ?? ApprovalStep(index: 1, count: 2, title: "Tests")
        if !a.options.contains(where: \.isFreeText) {
            a.options.append(ApprovalOption(label: "Type something.", keys: ["\(a.options.count + 1)"], freeText: true))
        }
        return a
    }

    func approval(agentId: String) async throws -> Approval? {
        guard agentList.first(where: { $0.id == agentId })?.status == .blocked else { return nil }
        return approvals[agentId]
    }

    func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        let workspace = workspaceList.first { $0.id == request.workspaceId }
        let pane = agentList.filter { $0.workspaceId == request.workspaceId }.count + 1
        let agent = Agent(
            id: "\(request.workspaceId):p\(pane + 10)", name: request.name, kind: request.kind, title: request.kind,
            workspaceId: request.workspaceId, workspaceName: workspace?.name ?? request.workspaceId,
            cwdName: workspace?.name ?? request.workspaceId, status: .idle, hasTranscript: true, updatedAt: Date()
        )
        agentList.append(agent)
        chats[agent.id] = []
        continuation?.yield(.event(.agentCreated(agent)))
        if let prompt = request.prompt {
            Task { try? await self.prompt(agentId: agent.id, text: prompt) }
        }
        return agent
    }

    nonisolated func events() -> AsyncStream<ConnectionEvent> {
        let (stream, continuation) = AsyncStream.makeStream(of: ConnectionEvent.self)
        Task { await attach(continuation) }
        return stream
    }

    // MARK: - Internals

    private func attach(_ continuation: AsyncStream<ConnectionEvent>.Continuation) {
        self.continuation = continuation
        continuation.yield(.connected)
        guard let replayInterval else { return }
        let events = replay
        Task {
            for event in events where event != .hello {
                try? await Task.sleep(for: replayInterval)
                apply(event)
            }
        }
    }

    private func apply(_ event: ServerEvent) {
        switch event {
        case .agentUpdated(let agent), .agentCreated(let agent):
            if let i = agentList.firstIndex(where: { $0.id == agent.id }) { agentList[i] = agent } else { agentList.append(agent) }
            if agent.status != .blocked { approvals[agent.id] = nil }
        case .agentClosed(let id):
            agentList.removeAll { $0.id == id }
            chats[id] = nil
        case .messageUpserted(let agentId, let message):
            store(message, agentId: agentId)
        case .hello, .unknown:
            break
        }
        continuation?.yield(.event(event))
    }

    private func push(_ message: Message, to agentId: String? = nil) {
        guard let agentId = agentId ?? chats.first(where: { $0.value.contains { $0.id == message.id } })?.key else { return }
        store(message, agentId: agentId)
        continuation?.yield(.event(.messageUpserted(agentId: agentId, message: message)))
    }

    private func store(_ message: Message, agentId: String) {
        var list = chats[agentId] ?? []
        if let i = list.firstIndex(where: { $0.id == message.id }) { list[i] = message } else { list.append(message) }
        chats[agentId] = list
    }

    private func setStatus(_ status: AgentStatus, for agentId: String) {
        guard let i = agentList.firstIndex(where: { $0.id == agentId }) else { return }
        agentList[i].status = status
        agentList[i].updatedAt = Date()
        continuation?.yield(.event(.agentUpdated(agentList[i])))
    }

    private func nextId() -> String {
        counter += 1
        return "mock-\(counter)"
    }
}

private extension Int {
    var nonZero: Int? { self == 0 ? nil : self }
}

/// Decoded `docs/fixtures`.
struct Fixtures: Sendable {
    var agents: [Agent] = []
    var workspaces: [Workspace] = []
    var messages: MessagePage = MessagePage(messages: [], hasMore: false)
    var approval: Approval?
    var events: [ServerEvent] = []

    init(directory: URL? = Bundle.main.url(forResource: "Fixtures", withExtension: nil)) {
        guard let directory else { return }
        let decoder = HerdJSON.decoder()
        func load<T: Decodable>(_ name: String) -> T? {
            guard let data = try? Data(contentsOf: directory.appending(path: name)) else { return nil }
            return try? decoder.decode(T.self, from: data)
        }
        agents = load("agents.json") ?? []
        workspaces = load("workspaces.json") ?? []
        messages = load("messages.json") ?? messages
        approval = load("approval.json")
        if let text = try? String(contentsOf: directory.appending(path: "ws-events.jsonl"), encoding: .utf8) {
            events = text.split(separator: "\n").compactMap { try? decoder.decode(ServerEvent.self, from: Data($0.utf8)) }
        }
    }
}
#endif

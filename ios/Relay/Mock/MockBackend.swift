#if DEBUG
import Foundation
import RelayKit

/// Serves `docs/fixtures` (copied into Debug builds as `Fixtures/`) and replays `ws-events.jsonl`.
/// It also reacts to prompts, stop and approvals so the app can be driven without a bridge.
/// Every chat besides the fixture one is made up here; nothing comes from a real machine.
actor MockBackend: Backend {
    private var agentList: [Agent]
    private var workspaceList: [Workspace]
    private var chats: [String: [Message]]
    private var approvals: [String: Approval]
    private let catalog: ControlsCatalog
    private let fixtures: Fixtures
    private let replay: [ServerEvent]
    private let replayInterval: Duration?
    private var continuation: AsyncStream<ConnectionEvent>.Continuation?
    private var counter = 0
    private var uploads: [String: (attachment: Attachment, data: Data)] = [:]
    private let latency: Duration = .milliseconds(UserDefaults.standard.integer(forKey: "latency").nonZero ?? 120)
    private var usageSnapshot: UsageSnapshot
    /// Prompts starting "unsent" fail once, like a send while offline; the retry goes through.
    private var failedOnce: Set<String> = []
    let profile: MockMachine
    /// Every call throws `unreachable` and the socket stays down; `setOffline(false)` brings it back.
    private var offline: Bool
    /// Every call throws `unauthorized` (a mock pair link with token `bad`).
    private let rejectsToken: Bool

    init(replayInterval: Duration? = .seconds(4), profile: MockMachine = .mac, offline: Bool = false, rejectsToken: Bool = false) {
        let fixtures = Fixtures()
        self.profile = profile
        self.offline = offline
        self.rejectsToken = rejectsToken
        catalog = fixtures.controls
        self.fixtures = fixtures
        usageSnapshot = fixtures.usage
        switch profile.variant {
        case .mac:
            agentList = MockControls.extraAgents(fixtures.agents) + MockSidebar.agents()
            workspaceList = fixtures.workspaces + MockSidebar.workspaces
            MockSidebar.markCompletedSeen(agentList, machineId: profile.machine.id)
            approvals = fixtures.approval.map { [$0.agentId: Self.extended($0)] } ?? [:]
            if LaunchOptions.current.mockPlan { approvals[MockPlan.agentId] = MockPlan.approval }
            replay = fixtures.events
            self.replayInterval = replayInterval
            chats = MockChats.all(fixtureMessages: fixtures.messages)
        case .vm, .blank:
            let other = MockMachines.content(for: profile)
            agentList = other.agents
            workspaceList = other.workspaces
            approvals = other.approvals
            replay = []
            self.replayInterval = nil
            chats = other.chats
        }
    }

    /// Mock machines only: goes offline (socket drops, calls fail) or comes back (socket reconnects).
    func setOffline(_ value: Bool) {
        guard value != offline else { return }
        offline = value
        continuation?.yield(value ? .disconnected : .connected)
        // Like WSClient: the first retry (after 1 s) fails too, so the machine reads offline, not reconnecting.
        if value { Task { await failedRetry() } }
    }

    private func failedRetry() async {
        try? await Task.sleep(for: .seconds(1))
        if offline { continuation?.yield(.disconnected) }
    }

    private func check() throws {
        if rejectsToken { throw RelayError.unauthorized }
        if offline { throw RelayError.unreachable(timedOut: false) }
    }

    func workspaces() async throws -> [Workspace] {
        try check()
        return workspaceList.map { w in
            var w = w
            w.agentCount = agentList.filter { $0.workspaceId == w.id }.count
            return w
        }
    }

    func agents() async throws -> [Agent] {
        try check()
        return agentList
    }

    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try await Task.sleep(for: latency)
        try check()
        // A new agent has no transcript yet: the bridge sends nothing rather than a read of the screen.
        if agentList.first(where: { $0.id == agentId })?.transcript == .pending {
            return MessagePage(messages: [], hasMore: false)
        }
        var list = chats[agentId] ?? []
        if let before, let i = list.firstIndex(where: { $0.id == before }) { list = Array(list[..<i]) }
        return MessagePage(messages: Array(list.suffix(limit)), hasMore: list.count > limit)
    }

    func machine() async throws -> Machine {
        try check()
        return profile.machine
    }

    func health() async throws -> Health {
        if offline { throw RelayError.unreachable(timedOut: false) }
        return Health(ok: true, name: "relay", version: "0.9.0-mock", herdr: "connected", uptimeSeconds: 42)
    }

    func usage() async throws -> UsageSnapshot {
        try await Task.sleep(for: latency)
        try check()
        return usageSnapshot
    }

    /// Bumps every window's `usedPercent` a bit and pushes `usage.updated`, like a real refresh landing.
    func refreshUsage() async throws {
        try await Task.sleep(for: latency)
        try check()
        usageSnapshot.providers = usageSnapshot.providers.map { p in
            var p = p
            p.updatedAt = Date()
            p.stale = false
            p.windows = p.windows.map { w in
                var w = w
                if let percent = w.usedPercent { w.usedPercent = min(100, percent + 1) }
                return w
            }
            return p
        }
        for p in usageSnapshot.providers { continuation?.yield(.event(.usageUpdated(p))) }
    }

    /// Stores the bytes and reports progress in a few steps, like a slow network.
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment {
        try check()
        guard !data.isEmpty else { throw RelayError.http(status: 400, code: "bad_request", message: "Empty body.") }
        guard data.count <= 20 * 1024 * 1024 else { throw RelayError.http(status: 413, code: "too_large", message: "Over 20 MB.") }
        for step in 1...4 {
            try await Task.sleep(for: .milliseconds(150))
            progress(Double(step) / 4)
        }
        counter += 1
        let id = String(format: "%016x", counter + profile.idOffset)
        let ext = (filename as NSString).pathExtension.lowercased()
        let kind: AttachmentKind = ["jpg", "jpeg", "png", "heic", "gif", "webp"].contains(ext) ? .image : ext == "pdf" ? .pdf : .file
        let attachment = Attachment(id: id, name: filename, kind: kind, size: data.count)
        uploads[id] = (attachment, data)
        return attachment
    }

    func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        guard let upload = uploads[attachmentId] else { throw RelayError.http(status: 404, code: "not_found", message: "Expired.") }
        return upload.data
    }

    func toolImage(agentId: String, toolCallId: String, index: Int) async throws -> Data {
        try check()
        try await Task.sleep(for: latency)
        guard let data = MockToolImages.data(toolCallId: toolCallId, index: index) else {
            throw RelayError.http(status: 404, code: "not_found", message: "No such image.")
        }
        return data
    }

    func file(agentId: String, path: String) async throws -> AgentFile {
        try check()
        try await Task.sleep(for: latency)
        return try MockFiles.file(path)
    }

    func changes(agentId: String) async throws -> Changes {
        try check()
        try await Task.sleep(for: latency)
        return MockChanges.changes(agentId: agentId, workspaceName: try workspaceName(of: agentId))
    }

    func changesDiff(agentId: String, path: String) async throws -> FileDiffText {
        try check()
        try await Task.sleep(for: latency)
        return try MockChanges.diff(agentId: agentId, workspaceName: try workspaceName(of: agentId), path: path)
    }

    func commit(agentId: String, sha: String) async throws -> CommitDetail {
        try check()
        try await Task.sleep(for: latency)
        return try MockChanges.commit(agentId: agentId, workspaceName: try workspaceName(of: agentId), sha: sha)
    }

    private func workspaceName(of agentId: String) throws -> String {
        guard let agent = agentList.first(where: { $0.id == agentId }) else {
            throw RelayError.http(status: 404, code: "not_found", message: "no such agent")
        }
        return agent.workspaceName
    }

    func prompt(agentId: String, text: String, attachments: [String]) async throws {
        try check()
        if text.lowercased().hasPrefix("unsent"), failedOnce.insert(text).inserted {
            throw RelayError.unreachable(timedOut: false)
        }
        if let i = agentList.firstIndex(where: { $0.id == agentId }), agentList[i].transcriptState == .pending {
            agentList[i].transcriptState = .ready
            agentList[i].hasTranscript = true
        }
        let refs = try attachments.map { id in
            guard let upload = uploads[id] else { throw RelayError.http(status: 400, code: "bad_request", message: "Unknown attachment \(id).") }
            return upload.attachment.ref
        }
        var blocks = refs.map { Block.attachment($0) }
        if !text.isEmpty { blocks.append(.text(text)) }
        let user = Message(id: nextId(), role: .user, createdAt: Date(), blocks: blocks)
        push(user, to: agentId)
        setStatus(.working, for: agentId)
        if text.lowercased().hasPrefix("slow bash") {
            Task { await liveToolTurn(agentId: agentId) }
            return
        }
        let replyId = nextId()
        Task {
            try? await Task.sleep(for: .seconds(1.2))
            let call = ToolCall(id: nextId(), name: "Read", summary: "Read README.md")
            var reply = Message(id: replyId, role: .assistant, createdAt: Date(), blocks: [.toolCall(call)])
            push(reply, to: agentId)
            try? await Task.sleep(for: .seconds(1.5))
            reply.blocks.append(.toolResult(ToolResult(toolCallId: call.id, isError: false, preview: "# Project")))
            let seen = attachments.isEmpty ? "" : " I got \(attachments.count) attachment\(attachments.count == 1 ? "" : "s")."
            reply.blocks.append(.text("This is the mock backend.\(seen) On a real bridge, **\(agentId)** would answer here."))
            push(reply, to: agentId)
            setStatus(.idle, for: agentId)
        }
    }

    func sendKeys(agentId: String, keys: [String]) async throws {
        try check()
        guard let agent = agentList.first(where: { $0.id == agentId }) else { return }
        if agent.status == .blocked {
            approvals[agentId] = nil
            setStatus(keys == ["esc"] ? .idle : .working, for: agentId)
        } else if keys == ["esc"] {
            setStatus(.idle, for: agentId)
        }
    }

    func sendText(agentId: String, text: String, submit: Bool) async throws {
        try check()
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
            // Arrow moves from the cursor (on the first row), like the real bridge.
            a.options.append(ApprovalOption(label: "Type something.", keys: Array(repeating: "down", count: a.options.count), freeText: true))
        }
        return a
    }

    func approval(agentId: String) async throws -> Approval? {
        try check()
        guard agentList.first(where: { $0.id == agentId })?.status == .blocked else { return nil }
        return approvals[agentId]
    }

    func controls() async throws -> ControlsCatalog {
        try check()
        return catalog
    }

    func kindControls(kind: String) async throws -> AgentControlsInfo {
        try check()
        guard ["claude", "codex", "pi"].contains(kind) else {
            throw RelayError.http(status: 400, code: "unsupported", message: "No controls for \(kind).")
        }
        return MockControls.kindInfo(for: kind, fixtures: fixtures)
    }

    func agentControls(agentId: String) async throws -> AgentControlsInfo {
        try check()
        guard let agent = agentList.first(where: { $0.id == agentId }) else {
            throw RelayError.http(status: 404, code: "not_found", message: "No such agent.")
        }
        return MockControls.info(for: agent.kind, fixtures: fixtures)
    }

    /// Mirrors the bridge: 409 while busy, 400 for non-Claude agents and for bypass (not in this cycle),
    /// then a short "confirm on screen" delay before the updated agent comes back.
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        try check()
        guard let i = agentList.firstIndex(where: { $0.id == agentId }) else {
            throw RelayError.http(status: 404, code: "not_found", message: "No such agent.")
        }
        let agent = agentList[i]
        let info = MockControls.info(for: agent.kind, fixtures: fixtures)
        let supported: Bool = switch request {
        case .model(let id): info.supports.model && info.models.contains { $0.id == id }
        case .effort(let id): info.supports.effort && info.efforts.contains { $0.id == id }
        case .permissionMode(let id): info.supports.mode && info.modes.contains { $0.id == id }
        case .command(.compact): info.supports.compact
        case .command(.clear): info.supports.clear
        }
        if !supported {
            throw RelayError.http(status: 400, code: "unsupported", message: "\(agent.kind) can't do that here.")
        }
        if agent.status == .working { throw RelayError.http(status: 409, code: "agent_busy", message: "The agent is working.") }
        if agent.status == .blocked { throw RelayError.http(status: 409, code: "agent_blocked", message: "The agent is waiting at a dialog.") }
        if request == .permissionMode("bypassPermissions") {
            throw RelayError.http(status: 400, code: "unsupported", message: "Bypass permissions isn't in this agent's Shift+Tab cycle.")
        }
        try await Task.sleep(for: request == .command(.compact) ? .seconds(2.5) : .seconds(1.2))
        var updated = agentList[i]
        switch request {
        case .model(let id):
            let option = info.models.first { $0.id == id }
            updated.model = agent.kind == "claude" ? "claude-\(id)-mock" : id
            updated.modelLabel = option?.label ?? id
            // pi resets thinking to the new model's default.
            if agent.kind == "pi" { updated.effort = "medium" }
        case .permissionMode(let mode):
            updated.permissionMode = mode
        case .effort(let effort):
            updated.effort = effort
        case .command(.compact):
            push(Message(id: nextId(), role: .assistant, createdAt: Date(), blocks: [.text("Compacted the conversation.")]), to: agentId)
        case .command(.clear):
            updated.sessionId = UUID().uuidString
            chats[agentId] = []
        }
        updated.updatedAt = Date()
        agentList[i] = updated
        continuation?.yield(.event(.agentUpdated(updated)))
        return updated
    }

    /// Like the bridge's `GET /kinds`, from `-mockSignedOut` / `-mockNotInstalled`.
    static func kindStatuses(signedOut: Set<String>, notInstalled: Set<String>) -> [KindStatus] {
        ["claude", "codex", "pi"].map { kind in
            if notInstalled.contains(kind) {
                return KindStatus(kind: kind, installed: false, signedIn: false, signInHint: "`\(kind)` isn't installed on this machine.")
            }
            if signedOut.contains(kind) {
                let hint = switch kind {
                case "claude": "Run `claude auth login` on this machine, then try again."
                case "codex": "Run `codex login` on this machine, then try again."
                default: "Run `pi` on this machine and sign in with `/login`, then try again."
                }
                return KindStatus(kind: kind, installed: true, signedIn: false, signInHint: hint)
            }
            return KindStatus(kind: kind, installed: true, signedIn: true)
        }
    }

    func kinds() async throws -> [KindStatus] {
        try check()
        try await Task.sleep(for: latency)
        let options = LaunchOptions.current
        if options.mockKindsStale { return Self.kindStatuses(signedOut: [], notInstalled: []) }
        return Self.kindStatuses(signedOut: options.mockSignedOut, notInstalled: options.mockNotInstalled)
    }

    func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        try check()
        // Like the bridge: refused before anything is created.
        let options = LaunchOptions.current
        if let status = Self.kindStatuses(signedOut: options.mockSignedOut, notInstalled: options.mockNotInstalled)
            .first(where: { $0.kind == request.kind }), !status.canStart {
            throw RelayError.http(status: 409, code: status.installed ? "not_signed_in" : "not_installed", message: status.signInHint)
        }
        // Like the bridge: model/effort must come from the kind's lists; left out means the saved default.
        let kindInfo = MockControls.kindInfo(for: request.kind, fixtures: fixtures)
        let model = request.model ?? kindInfo.defaultModel
        if let m = request.model, !kindInfo.models.contains(where: { $0.id == m }) {
            throw RelayError.http(status: 400, code: "bad_request", message: "Unknown model \(m).")
        }
        if let e = request.effort, !kindInfo.efforts(for: model).contains(where: { $0.id == e }) {
            throw RelayError.http(status: 400, code: "bad_request", message: "\(e) isn't an effort for \(model ?? "the default model").")
        }
        let workspace = workspaceList.first { $0.id == request.workspaceId }
        let pane = agentList.filter { $0.workspaceId == request.workspaceId }.count + 1
        let agent = Agent(
            id: "\(request.workspaceId):p\(pane + 10)", name: request.name, kind: request.kind, title: request.kind,
            workspaceId: request.workspaceId, workspaceName: workspace?.name ?? request.workspaceId,
            cwdName: workspace?.name ?? request.workspaceId, status: .idle, hasTranscript: false, updatedAt: Date(),
            model: model.map { request.kind == "claude" ? "claude-\($0)-mock" : $0 },
            modelLabel: model.flatMap { m in kindInfo.models.first { $0.id == m }?.label } ?? model,
            permissionMode: request.kind == "claude" ? "default" : request.kind == "codex" ? "ask" : nil,
            effort: request.effort ?? kindInfo.defaultEffort,
            sessionId: UUID().uuidString,
            transcriptState: .pending
        )
        agentList.append(agent)
        chats[agent.id] = []
        continuation?.yield(.event(.agentCreated(agent)))
        if let prompt = request.prompt {
            Task { try? await self.prompt(agentId: agent.id, text: prompt, attachments: []) }
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
        if rejectsToken {
            continuation.yield(.rejected(status: 401))
            continuation.yield(.disconnected)
            return
        }
        continuation.yield(offline ? .disconnected : .connected)
        if offline { Task { await failedRetry() } }
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
        case .hello, .unknown, .replyLive, .usageUpdated:
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

    /// A slow Bash call the way the bridge reports it: `reply.live` shows "Running Bash…" right away
    /// (one frame carries the raw tool block an older bridge would send as text), the transcript's
    /// toolCall lands seconds later, then `tool: null`.
    private func liveToolTurn(agentId: String) async {
        let live = LiveTool(name: "Bash", summary: "Ran sleep 5")
        var seq = 0
        func frame(_ text: String?, _ tool: LiveTool?) {
            seq += 1
            continuation?.yield(.event(.replyLive(agentId: agentId, text: text, seq: seq, tool: tool)))
        }
        try? await Task.sleep(for: .milliseconds(300))
        // Claude's "Running 1 shell command…" comes first; the arguments show a frame later.
        frame(nil, LiveTool(name: "Bash", summary: "Ran a command"))
        for i in 0..<20 {
            try? await Task.sleep(for: .milliseconds(250))
            frame(i.isMultiple(of: 3) ? "⏺ Bash(sleep 5)\n  ⎿  Running…" : nil, live)
        }
        let call = ToolCall(id: nextId(), name: "Bash", summary: "Ran sleep 5")
        var reply = Message(id: nextId(), role: .assistant, createdAt: Date(), blocks: [.toolCall(call)])
        push(reply, to: agentId)
        try? await Task.sleep(for: .milliseconds(300))
        frame(nil, nil)
        try? await Task.sleep(for: .seconds(1.5))
        reply.blocks.append(.toolResult(ToolResult(toolCallId: call.id, isError: false, preview: "")))
        reply.blocks.append(.text("Slept for 5 seconds."))
        push(reply, to: agentId)
        setStatus(.idle, for: agentId)
    }

    private func setStatus(_ status: AgentStatus, for agentId: String) {
        guard let i = agentList.firstIndex(where: { $0.id == agentId }) else { return }
        agentList[i].status = status
        agentList[i].updatedAt = Date()
        continuation?.yield(.event(.agentUpdated(agentList[i])))
    }

    /// Per machine, so two mock machines never hand out the same message or attachment id.
    private func nextId() -> String {
        counter += 1
        return "\(profile.machine.id)-\(counter)"
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
    var controls = ControlsCatalog(models: [], modes: [], efforts: [])
    var agentControls: [String: AgentControlsInfo] = [:]
    var usage = UsageSnapshot(providers: [])

    init(directory: URL? = Bundle.main.url(forResource: "Fixtures", withExtension: nil)) {
        guard let directory else { return }
        let decoder = RelayJSON.decoder()
        func load<T: Decodable>(_ name: String) -> T? {
            guard let data = try? Data(contentsOf: directory.appending(path: name)) else { return nil }
            return try? decoder.decode(T.self, from: data)
        }
        agents = load("agents.json") ?? []
        workspaces = load("workspaces.json") ?? []
        messages = load("messages.json") ?? messages
        approval = load("approval.json")
        controls = load("controls.json") ?? controls
        for kind in ["claude", "pi", "codex"] {
            agentControls[kind] = load("agent-controls-\(kind).json")
        }
        usage = load("usage.json") ?? usage
        if let text = try? String(contentsOf: directory.appending(path: "ws-events.jsonl"), encoding: .utf8) {
            events = text.split(separator: "\n").compactMap { try? decoder.decode(ServerEvent.self, from: Data($0.utf8)) }
        }
    }
}
#endif

import Foundation
import RelayKit
import Testing
@testable import Relay

/// A backend whose behavior each test configures directly: slow/fast responses, failures on demand,
/// and call counts. Used for the staleness, race and error-mapping fixes in
/// docs/tasks/round-5/ios-harden.md that `RecordingBackend` (StoreTests.swift) isn't shaped for.
actor ProgrammableBackend: Backend {
    var agentsToReturn: [Agent] = []
    var workspacesToReturn: [Workspace] = []
    var machineToReturn = Machine(id: "m1", name: "Test Mac", kind: .laptop)
    var controlsToReturn = ControlsCatalog(models: [], modes: [], efforts: [])
    var approvalToReturn: Approval?
    var agentsError: (any Error)?
    var agentsDelay: Duration?

    /// One handler invocation per call to `messages`, in order; the last one repeats once exhausted.
    var messagesResponses: [() async throws -> MessagePage] = [{ MessagePage(messages: [], hasMore: false) }]
    private var messagesCallCount = 0

    var promptError: (any Error)?
    private(set) var promptCalls: [(agentId: String, text: String, attachments: [String])] = []

    func agents() async throws -> [Agent] {
        // Snapshot what this call will answer with before waiting, so reconfiguring the backend for
        // a later, faster call (the staleness scenario under test) can't retroactively change what an
        // already in-flight call returns — that's what a real, earlier network response would do.
        let result = agentsToReturn
        let error = agentsError
        if let agentsDelay { try await Task.sleep(for: agentsDelay) }
        if let error { throw error }
        return result
    }
    func workspaces() async throws -> [Workspace] { workspacesToReturn }

    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        let index = min(messagesCallCount, messagesResponses.count - 1)
        messagesCallCount += 1
        return try await messagesResponses[index]()
    }

    func prompt(agentId: String, text: String, attachments: [String]) async throws {
        promptCalls.append((agentId, text, attachments))
        if let promptError { throw promptError }
    }

    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> RelayKit.Attachment {
        progress(1)
        return RelayKit.Attachment(id: "a", name: filename, kind: .file, size: data.count)
    }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
    func machine() async throws -> Machine { machineToReturn }
    func sendKeys(agentId: String, keys: [String]) async throws {}
    func sendText(agentId: String, text: String, submit: Bool) async throws {}
    func approval(agentId: String) async throws -> Approval? { approvalToReturn }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent { agentsToReturn[0] }
    func controls() async throws -> ControlsCatalog { controlsToReturn }
    func kindControls(kind: String) async throws -> AgentControlsInfo {
        AgentControlsInfo(models: [], efforts: [], modes: [], supports: .init(model: false, effort: false, mode: false, compact: false, clear: false))
    }
    func agentControls(agentId: String) async throws -> AgentControlsInfo {
        throw RelayError.http(status: 404, code: "not_found", message: nil)
    }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent { agentsToReturn[0] }
    nonisolated func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }
}

private func testAgent(id: String = "w1:p1", title: String = "Test agent", status: AgentStatus = .idle) -> Agent {
    Agent(
        id: id, name: nil, kind: "claude", title: title, workspaceId: "w1", workspaceName: "shop-api",
        cwdName: "shop-api", status: status, hasTranscript: true, updatedAt: .now
    )
}

private func message(_ id: String, _ text: String, at date: Date = .now) -> Message {
    Message(id: id, role: .assistant, createdAt: date, blocks: [.text(text)])
}

@MainActor
@Suite("Connection, race and error hardening")
struct ConnectionHardeningTests {
    // MARK: - Staleness

    /// A rapid agent switch (or a reconnect racing an `open()`) can leave an older, slower request
    /// in flight behind a newer one. It must never win.
    @Test func staleMessageLoadNeverOverwritesNewer() async throws {
        let backend = ProgrammableBackend()
        let agent = testAgent()
        await backend.setAgents([agent])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id

        await backend.setMessagesResponses([
            { try await Task.sleep(for: .milliseconds(150)); return MessagePage(messages: [message("old", "stale")], hasMore: false) },
            { MessagePage(messages: [message("new", "fresh")], hasMore: false) },
        ])

        let slow = Task { await store.loadMessages(agent.id) }
        try await Task.sleep(for: .milliseconds(10))
        let fast = Task { await store.loadMessages(agent.id) }
        _ = await (slow.value, fast.value)
        try await Task.sleep(for: .milliseconds(200))  // let the slow response land and be discarded

        #expect(store.messages(for: agent.id).map(\.id) == ["new"])
    }

    /// Foreground and a reconnect can both call `refresh()` around the same moment. An older
    /// resync landing after a newer one must not un-apply it.
    @Test func overlappingRefreshAppliesOnlyTheNewestSnapshot() async throws {
        let backend = ProgrammableBackend()
        let stale = testAgent(title: "Stale title")
        let fresh = testAgent(title: "Fresh title")
        let store = AppStore(backend: backend, hostLabel: "test")

        // The first refresh's `agents()` call is slow and, at the moment it's issued, would answer
        // with the stale snapshot.
        await backend.setAgents([stale])
        await backend.setAgentsDelay(.milliseconds(120))
        let firstRefresh = Task { await store.refresh() }
        try await Task.sleep(for: .milliseconds(20))  // let it start and begin waiting

        // A second refresh (e.g. the foreground handler firing right as a reconnect's refresh does)
        // is fast and carries the newer snapshot; it lands well before the first one wakes up.
        await backend.setAgents([fresh])
        await backend.setAgentsDelay(nil)
        let secondRefresh = Task { await store.refresh() }
        await secondRefresh.value
        #expect(store.state.agent(fresh.id)?.title == "Fresh title")

        await firstRefresh.value  // now resolves with the stale snapshot; must be discarded
        #expect(store.state.agent(fresh.id)?.title == "Fresh title")
    }

    // MARK: - Races: double tap

    @Test func doubleTapSendGoesOutOnce() async throws {
        let backend = ProgrammableBackend()
        let agent = testAgent()
        await backend.setAgents([agent])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id

        store.send("hello", to: agent.id)
        store.send("hello", to: agent.id)  // the double tap
        try await Task.sleep(for: .milliseconds(100))

        #expect(store.pending[agent.id]?.count == 1)
        #expect(await backend.promptCalls.count == 1)
    }

    @Test func distinctSendsBothGoOut() async throws {
        let backend = ProgrammableBackend()
        let agent = testAgent()
        await backend.setAgents([agent])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id

        store.send("first", to: agent.id)
        store.send("second", to: agent.id)
        try await Task.sleep(for: .milliseconds(100))

        #expect(store.pending[agent.id]?.count == 2)
        #expect(await backend.promptCalls.count == 2)
    }

    // MARK: - Send while reconnecting / failed sends never silently drop

    @Test func failedSendKeepsBubbleVisibleAndRetryable() async throws {
        let backend = ProgrammableBackend()
        let agent = testAgent()
        await backend.setAgents([agent])
        await backend.setPromptError(RelayError.unreachable(timedOut: false))
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id

        store.send("still here?", to: agent.id)
        try await Task.sleep(for: .milliseconds(100))

        let bubble = try #require(store.pending[agent.id]?.first)
        #expect(store.failedPending.contains(bubble.id), "a failed send must stay visible, not vanish")
        #expect(store.errorMessage != nil)

        await backend.setPromptError(nil)
        store.retry(bubble.id, to: agent.id)
        try await Task.sleep(for: .milliseconds(100))

        #expect(!store.failedPending.contains(bubble.id))
        #expect(store.pending[agent.id]?.count == 1, "retry resends the same bubble, not a duplicate")
        #expect(await backend.promptCalls.count == 2)
    }

    @Test func discardFailedRemovesTheBubble() async throws {
        let backend = ProgrammableBackend()
        let agent = testAgent()
        await backend.setAgents([agent])
        await backend.setPromptError(RelayError.unreachable(timedOut: false))
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id

        store.send("give up", to: agent.id)
        try await Task.sleep(for: .milliseconds(100))
        let bubble = try #require(store.pending[agent.id]?.first)

        store.discardFailed(bubble.id, from: agent.id)
        #expect(store.pending[agent.id] == nil)
        #expect(!store.failedPending.contains(bubble.id))
    }

    // MARK: - Errors: no raw text, ever

    @Test func unauthorizedSetsStickyRePairingBanner() async throws {
        let backend = ProgrammableBackend()
        await backend.setAgentsError(RelayError.unauthorized)
        let store = AppStore(backend: backend, hostLabel: "test")
        await store.refresh()

        #expect(store.needsRePairing)
        #expect(store.errorMessage == RelayError.unauthorized.errorDescription)

        await backend.setAgentsError(nil)
        await backend.setAgents([testAgent()])
        await store.refresh()
        #expect(!store.needsRePairing, "a successful refresh clears the sticky banner")
    }

    // MARK: - Memory warning

    @Test func memoryWarningKeepsOnlyTheOpenChat() async throws {
        let backend = ProgrammableBackend()
        let a = testAgent(id: "w1:p1")
        let b = testAgent(id: "w1:p2")
        await backend.setAgents([a, b])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = a.id
        await store.refresh()
        await store.loadMessages(b.id)
        #expect(store.isLoaded(a.id) && store.isLoaded(b.id))

        store.handleMemoryWarning()

        #expect(store.isLoaded(a.id), "the open chat survives a memory warning")
        #expect(!store.isLoaded(b.id), "a background chat is dropped; it's refetched if reopened")
    }
}

private extension ProgrammableBackend {
    func setAgents(_ agents: [Agent]) { agentsToReturn = agents }
    func setAgentsError(_ error: (any Error)?) { agentsError = error }
    func setMessagesResponses(_ responses: [() async throws -> MessagePage]) { messagesResponses = responses }
    func setPromptError(_ error: (any Error)?) { promptError = error }
    /// Injects a delay before `agents()` returns, to make one `refresh()` call finish after another
    /// that started later.
    func setAgentsDelay(_ delay: Duration?) { agentsDelay = delay }
}

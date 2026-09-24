import Foundation
import RelayKit
import Testing
@testable import Relay

/// Records every call the store makes.
actor RecordingBackend: Backend {
    enum Call: Equatable {
        case keys([String])
        case text(String, submit: Bool)
        case control(ControlRequest)
        case prompt(String, attachments: [String])
    }

    private(set) var calls: [Call] = []
    let agent: Agent
    let pendingApproval: Approval

    init(agent: Agent, approval: Approval) {
        self.agent = agent
        self.pendingApproval = approval
    }

    func workspaces() async throws -> [Workspace] { [] }
    func agents() async throws -> [Agent] { [agent] }
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage { MessagePage(messages: [], hasMore: false) }
    func prompt(agentId: String, text: String, attachments: [String]) async throws {
        calls.append(.prompt(text, attachments: attachments))
    }
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> RelayKit.Attachment {
        progress(1)
        return RelayKit.Attachment(id: "a\(calls.count)", name: filename, kind: contentType.hasPrefix("image/") ? .image : .file, size: data.count)
    }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
    func machine() async throws -> Machine { try FixtureFiles.decode(Machine.self, "machine.json") }
    func sendKeys(agentId: String, keys: [String]) async throws { calls.append(.keys(keys)) }
    func sendText(agentId: String, text: String, submit: Bool) async throws { calls.append(.text(text, submit: submit)) }
    /// What `/approval` returns once something was answered (nil = 204).
    var afterAnswer: Approval?
    func setAfterAnswer(_ approval: Approval?) { afterAnswer = approval }
    func approval(agentId: String) async throws -> Approval? { calls.isEmpty ? pendingApproval : afterAnswer }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        created.append(request)
        return agent
    }
    func controls() async throws -> ControlsCatalog { try FixtureFiles.decode(ControlsCatalog.self, "controls.json") }
    /// nil = an older bridge without the per-agent route (404).
    var perAgentControls: AgentControlsInfo?
    private(set) var agentControlsFetches = 0
    func setPerAgentControls(_ info: AgentControlsInfo?) { perAgentControls = info }
    func kindControls(kind: String) async throws -> AgentControlsInfo {
        kindControlsFetches += 1
        return AgentControlsInfo(
            models: [ControlOption(id: "m1", label: "M1"), ControlOption(id: "m2", label: "M2")],
            efforts: [ControlOption(id: "low", label: "Low")], modes: [],
            supports: .init(model: true, effort: true, mode: false, compact: false, clear: false),
            defaultModel: "m1", defaultEffort: "low",
            effortsByModel: ["m2": [ControlOption(id: "max", label: "Max")]]
        )
    }
    private(set) var kindControlsFetches = 0
    private(set) var created: [CreateAgentRequest] = []

    func agentControls(agentId: String) async throws -> AgentControlsInfo {
        agentControlsFetches += 1
        guard let perAgentControls else { throw RelayError.http(status: 404, code: "not_found", message: "Not Found") }
        return perAgentControls
    }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        calls.append(.control(request))
        if let controlError { throw controlError }
        var updated = agent
        switch request {
        case .model(let alias): updated.model = "claude-\(alias)-5"; updated.modelLabel = alias.capitalized
        case .permissionMode(let mode): updated.permissionMode = mode
        case .effort(let effort): updated.effort = effort
        case .command(.clear): updated.sessionId = "new-session"
        case .command(.compact): break
        }
        return updated
    }
    var controlError: RelayError?
    func failControls(with error: RelayError) { controlError = error }
    nonisolated func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }
}

@MainActor
@Suite("AppStore approvals")
struct StoreTests {
    private func blockedAgent() throws -> Agent {
        try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.status == .blocked })
    }

    private func approval(for agent: Agent) -> Approval {
        Approval(agentId: agent.id, question: "Which cart?", options: [
            ApprovalOption(label: "Shared", keys: ["1"]),
            ApprovalOption(label: "Type something.", keys: ["2"], freeText: true),
        ])
    }

    private func loadedStore(_ backend: RecordingBackend, agent: Agent) async -> AppStore {
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id
        await store.refresh()
        return store
    }

    @Test func freeTextAnswerSendsKeysThenText() async throws {
        let agent = try blockedAgent()
        let backend = RecordingBackend(agent: agent, approval: approval(for: agent))
        let store = await loadedStore(backend, agent: agent)
        let option = try #require(store.approval?.options.last)
        #expect(store.isApprovalSheetPresented)

        store.answer(option, text: "  a fresh one  ")
        #expect(!store.isApprovalSheetPresented)
        try await waitFor { await backend.calls.count == 2 }
        #expect(await backend.calls == [.keys(["2"]), .text("a fresh one", submit: true)])
    }

    @Test func plainAnswerSendsOnlyKeys() async throws {
        let agent = try blockedAgent()
        let backend = RecordingBackend(agent: agent, approval: approval(for: agent))
        let store = await loadedStore(backend, agent: agent)
        store.answer(try #require(store.approval?.options.first))
        try await waitFor { await backend.calls.count == 1 }
        try await Task.sleep(for: .milliseconds(500))
        #expect(await backend.calls == [.keys(["1"])])
    }

    /// A stop marker in the transcript is Claude's, so it must never stand in for a prompt sent from the phone.
    @Test func stopMarkerDoesNotResolvePendingPrompt() async throws {
        let agent = try blockedAgent()
        let store = await loadedStore(RecordingBackend(agent: agent, approval: approval(for: agent)), agent: agent)
        let marker = "[Request interrupted by user]"
        store.send(marker, to: agent.id)
        #expect(store.pending[agent.id]?.count == 1)

        store.apply(.messageUpserted(agentId: agent.id, message: Message(id: "m1", role: .user, createdAt: .now, blocks: [.text(marker)])))
        #expect(store.pending[agent.id]?.count == 1)

        store.send("Carry on", to: agent.id)
        store.apply(.messageUpserted(agentId: agent.id, message: Message(id: "m2", role: .user, createdAt: .now, blocks: [.text("Carry on")])))
        #expect(store.pending[agent.id]?.map(\.plainText) == [marker])
    }

    /// Right after an answer the same dialog can still be on screen with the cursor moved, so the bridge
    /// returns the same question with different keys. That must not bring the sheet back.
    @Test func answeredQuestionDoesNotComeBack() async throws {
        let agent = try blockedAgent()
        let asked = approval(for: agent)
        let backend = RecordingBackend(agent: agent, approval: asked)
        let store = await loadedStore(backend, agent: agent)
        #expect(store.isApprovalSheetPresented)
        var lingering = asked
        lingering.options = [ApprovalOption(label: "Shared", keys: ["up", "enter"]), ApprovalOption(label: "Type something.", keys: ["enter"], freeText: true)]
        await backend.setAfterAnswer(lingering)

        store.answer(try #require(store.approval?.options.first))
        try await Task.sleep(for: .milliseconds(1500))
        #expect(!store.isApprovalSheetPresented, "followUp re-showed the answered question")
        await store.refreshApproval()
        #expect(store.approval == nil && !store.isApprovalSheetPresented, "refresh re-showed it")

        var next = asked
        next.question = "And the second question?"
        await backend.setAfterAnswer(next)
        await store.refreshApproval()
        #expect(store.isApprovalSheetPresented && store.approval?.question == "And the second question?")
    }

    private func waitFor(_ condition: @escaping () async -> Bool) async throws {
        for _ in 0..<50 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        Issue.record("timed out")
    }
}

import Foundation
import HerdKit
import Testing
@testable import Herd

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
    ) async throws -> HerdKit.Attachment {
        progress(1)
        return HerdKit.Attachment(id: "a\(calls.count)", name: filename, kind: contentType.hasPrefix("image/") ? .image : .file, size: data.count)
    }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
    func machine() async throws -> Machine { try FixtureFiles.decode(Machine.self, "machine.json") }
    func sendKeys(agentId: String, keys: [String]) async throws { calls.append(.keys(keys)) }
    func sendText(agentId: String, text: String, submit: Bool) async throws { calls.append(.text(text, submit: submit)) }
    func approval(agentId: String) async throws -> Approval? { calls.isEmpty ? pendingApproval : nil }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent { agent }
    func controls() async throws -> ControlsCatalog { try FixtureFiles.decode(ControlsCatalog.self, "controls.json") }
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
    var controlError: HerdError?
    func failControls(with error: HerdError) { controlError = error }
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

    private func waitFor(_ condition: @escaping () async -> Bool) async throws {
        for _ in 0..<50 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        Issue.record("timed out")
    }
}

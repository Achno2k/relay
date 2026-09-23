import Foundation
import HerdKit
import Testing
@testable import Herd

/// Records every call the store makes.
actor RecordingBackend: Backend {
    enum Call: Equatable {
        case keys([String])
        case text(String, submit: Bool)
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
    func prompt(agentId: String, text: String) async throws {}
    func sendKeys(agentId: String, keys: [String]) async throws { calls.append(.keys(keys)) }
    func sendText(agentId: String, text: String, submit: Bool) async throws { calls.append(.text(text, submit: submit)) }
    func approval(agentId: String) async throws -> Approval? { calls.isEmpty ? pendingApproval : nil }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent { agent }
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

    private func waitFor(_ condition: @escaping () async -> Bool) async throws {
        for _ in 0..<50 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        Issue.record("timed out")
    }
}

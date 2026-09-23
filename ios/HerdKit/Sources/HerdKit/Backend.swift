import Foundation

/// Everything the app needs from a bridge. `LiveBackend` talks HTTP + WebSocket; the app ships a mock for previews.
public protocol Backend: Sendable {
    func workspaces() async throws -> [Workspace]
    func agents() async throws -> [Agent]
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage
    func prompt(agentId: String, text: String) async throws
    func sendKeys(agentId: String, keys: [String]) async throws
    /// `nil` when the agent isn't blocked (204).
    func approval(agentId: String) async throws -> Approval?
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent
    /// Socket state and deltas. Reconnects by itself until the stream is dropped.
    func events() -> AsyncStream<ConnectionEvent>
}

public struct LiveBackend: Backend {
    public let client: APIClient
    public let socket: WSClient

    public init(pairing: Pairing, session: URLSession = .shared) {
        client = APIClient(baseURL: pairing.url, token: pairing.token, session: session)
        socket = WSClient(baseURL: pairing.url, token: pairing.token, session: session)
    }

    public func workspaces() async throws -> [Workspace] { try await client.workspaces() }
    public func agents() async throws -> [Agent] { try await client.agents() }
    public func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try await client.messages(agentId: agentId, before: before, limit: limit)
    }
    public func prompt(agentId: String, text: String) async throws { try await client.prompt(agentId: agentId, text: text) }
    public func sendKeys(agentId: String, keys: [String]) async throws { try await client.sendKeys(agentId: agentId, keys: keys) }
    public func approval(agentId: String) async throws -> Approval? { try await client.approval(agentId: agentId) }
    public func createAgent(_ request: CreateAgentRequest) async throws -> Agent { try await client.createAgent(request) }
    public func events() -> AsyncStream<ConnectionEvent> { socket.events() }
}

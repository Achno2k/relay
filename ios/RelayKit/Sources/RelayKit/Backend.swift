import Foundation

/// Everything the app needs from a bridge. `LiveBackend` talks HTTP + WebSocket; the app ships a mock for previews.
public protocol Backend: Sendable {
    func workspaces() async throws -> [Workspace]
    func agents() async throws -> [Agent]
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage
    func prompt(agentId: String, text: String, attachments: [String]) async throws
    /// Raw upload; `progress` reports 0...1 as the body is sent.
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data
    /// `GET /agents/:id/tool-images/:toolCallId/:index`: an image a tool returned, as raw bytes.
    func toolImage(agentId: String, toolCallId: String, index: Int) async throws -> Data
    /// `GET /agents/:id/file`: a file inside the agent's project, by cwd-relative path.
    func file(agentId: String, path: String) async throws -> AgentFile
    func machine() async throws -> Machine
    /// `GET /health`: version and herdr state.
    func health() async throws -> Health
    func sendKeys(agentId: String, keys: [String]) async throws
    /// Types `text` literally into the agent's current input (no clearing), then Enter if `submit`.
    func sendText(agentId: String, text: String, submit: Bool) async throws
    /// `nil` when the agent isn't blocked (204).
    func approval(agentId: String) async throws -> Approval?
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent
    /// `GET /kinds`: which agent kinds are installed and signed in on this machine.
    func kinds() async throws -> [KindStatus]
    /// `GET /controls` (Claude's list; kept for older bridges).
    func controls() async throws -> ControlsCatalog
    /// `GET /controls?kind=`: choices and defaults for a new agent of that kind (the New chat sheet).
    func kindControls(kind: String) async throws -> AgentControlsInfo
    /// `GET /agents/:id/controls`: this agent's own choices and supported controls.
    func agentControls(agentId: String) async throws -> AgentControlsInfo
    /// `POST /agents/:id/control`. Returns the updated agent once the bridge has confirmed the change.
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent
    /// Socket state and deltas. Reconnects by itself until the stream is dropped.
    func events() -> AsyncStream<ConnectionEvent>
    /// `GET /usage`: the bridge's cache, never a live fetch.
    func usage() async throws -> UsageSnapshot
    /// `POST /usage/refresh`: throws `RelayError.http(status: 429, ...)` if throttled.
    func refreshUsage() async throws
}

public extension Backend {
    /// Test doubles that don't care about `/health`.
    func health() async throws -> Health { Health(ok: true) }
    /// Bridges from before `/kinds`, and test doubles: unknown, so the app lets every kind be picked.
    func kinds() async throws -> [KindStatus] {
        throw RelayError.http(status: 404, code: "not_found", message: nil)
    }
    /// Test doubles without tool images (and what an older bridge answers).
    func toolImage(agentId: String, toolCallId: String, index: Int) async throws -> Data {
        throw RelayError.http(status: 404, code: "not_found", message: nil)
    }
    /// Test doubles that don't serve files.
    func file(agentId: String, path: String) async throws -> AgentFile {
        throw RelayError.http(status: 404, code: "not_found", message: nil)
    }
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
    public func prompt(agentId: String, text: String, attachments: [String]) async throws {
        try await client.prompt(agentId: agentId, text: text, attachments: attachments)
    }
    public func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment {
        try await client.uploadAttachment(agentId: agentId, data: data, filename: filename, contentType: contentType, progress: progress)
    }
    public func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        try await client.attachmentData(agentId: agentId, attachmentId: attachmentId)
    }
    public func toolImage(agentId: String, toolCallId: String, index: Int) async throws -> Data {
        try await client.toolImage(agentId: agentId, toolCallId: toolCallId, index: index)
    }
    public func file(agentId: String, path: String) async throws -> AgentFile {
        try await client.file(agentId: agentId, path: path)
    }
    public func machine() async throws -> Machine { try await client.machine() }
    public func health() async throws -> Health { try await client.health() }
    public func sendKeys(agentId: String, keys: [String]) async throws { try await client.sendKeys(agentId: agentId, keys: keys) }
    public func sendText(agentId: String, text: String, submit: Bool) async throws {
        try await client.sendText(agentId: agentId, text: text, submit: submit)
    }
    public func approval(agentId: String) async throws -> Approval? { try await client.approval(agentId: agentId) }
    public func createAgent(_ request: CreateAgentRequest) async throws -> Agent { try await client.createAgent(request) }
    public func kinds() async throws -> [KindStatus] { try await client.kinds() }
    public func controls() async throws -> ControlsCatalog { try await client.controls() }
    public func agentControls(agentId: String) async throws -> AgentControlsInfo { try await client.agentControls(agentId: agentId) }
    public func kindControls(kind: String) async throws -> AgentControlsInfo { try await client.kindControls(kind: kind) }
    public func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        try await client.control(agentId: agentId, request)
    }
    public func events() -> AsyncStream<ConnectionEvent> { socket.events() }
    public func usage() async throws -> UsageSnapshot { try await client.usage() }
    public func refreshUsage() async throws { try await client.refreshUsage() }
}

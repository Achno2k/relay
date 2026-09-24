import Foundation

public enum HerdrError: Error, Sendable, CustomStringConvertible {
    case unavailable(String)
    case io(String)
    case timeout
    case remote(code: String, message: String)
    case decoding(String)

    public var description: String {
        switch self {
        case .unavailable(let m), .io(let m), .decoding(let m): m
        case .timeout: "herdr did not answer in time"
        case .remote(let code, let message): "\(code): \(message)"
        }
    }
}

// MARK: herdr wire types (snake_case on the wire)

public struct HerdrAgentSession: Decodable, Sendable, Equatable {
    public var source: String
    public var agent: String
    public var kind: String  // "id" | "path"
    public var value: String
}

public struct HerdrAgent: Decodable, Sendable, Equatable {
    public var paneId: String
    public var workspaceId: String
    public var tabId: String
    public var name: String?
    public var agent: String?
    public var displayAgent: String?
    public var agentStatus: AgentStatus
    public var title: String?
    public var terminalTitleStripped: String?
    public var cwd: String?
    public var foregroundCwd: String?
    public var agentSession: HerdrAgentSession?
    public var stateChangeSeq: UInt64?
    public var revision: UInt64?
}

public struct HerdrWorkspace: Decodable, Sendable, Equatable {
    public var workspaceId: String
    public var label: String
    public var number: Int?
}

public struct HerdrPane: Decodable, Sendable, Equatable {
    public var paneId: String
    public var workspaceId: String
    public var tabId: String
    public var cwd: String?
    public var foregroundCwd: String?
    public var agent: String?
}

public struct HerdrRead: Decodable, Sendable, Equatable {
    public var text: String
    public var revision: UInt64?
    public var truncated: Bool?
}

public struct HerdrTab: Decodable, Sendable, Equatable {
    public var tabId: String
    public var workspaceId: String
}

public enum HerdrReadSource: String, Sendable {
    case visible, recent, detection
    case recentUnwrapped = "recent_unwrapped"
}

/// Talks to herdr's unix socket: one JSON request per line, one response per line.
/// Each request uses its own short-lived connection so a slow call (e.g. `agent.start`) never blocks others.
public actor HerdrClient {
    public let socketPath: String
    private var nextId = 0

    public init(socketPath: String = HerdrClient.defaultSocketPath) {
        self.socketPath = socketPath
    }

    public static var defaultSocketPath: String {
        if let p = ProcessInfo.processInfo.environment["HERDR_SOCKET_PATH"], !p.isEmpty { return p }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".config/herdr/herdr.sock").path
    }

    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()

    // MARK: Methods

    public func agents() async throws -> [HerdrAgent] {
        struct R: Decodable { var agents: [HerdrAgent] }
        return try await call("agent.list", [:], as: R.self).agents
    }

    public func agent(_ target: String) async throws -> HerdrAgent {
        struct R: Decodable { var agent: HerdrAgent }
        return try await call("agent.get", ["target": target], as: R.self).agent
    }

    public func workspaces() async throws -> [HerdrWorkspace] {
        struct R: Decodable { var workspaces: [HerdrWorkspace] }
        return try await call("workspace.list", [:], as: R.self).workspaces
    }

    public func panes(workspaceId: String?) async throws -> [HerdrPane] {
        struct R: Decodable { var panes: [HerdrPane] }
        var params: [String: Any] = [:]
        if let workspaceId { params["workspace_id"] = workspaceId }
        return try await call("pane.list", params, as: R.self).panes
    }

    public func read(_ target: String, source: HerdrReadSource, lines: Int? = nil, ansi: Bool = false) async throws -> HerdrRead {
        struct R: Decodable { var read: HerdrRead }
        var params: [String: Any] = ["target": target, "source": source.rawValue, "strip_ansi": !ansi, "format": ansi ? "ansi" : "text"]
        if let lines { params["lines"] = lines }
        return try await call("agent.read", params, as: R.self).read
    }

    public func prompt(_ target: String, text: String) async throws {
        _ = try await call("agent.prompt", ["target": target, "text": text], as: Ignored.self)
    }

    public func sendKeys(_ target: String, keys: [String]) async throws {
        _ = try await call("agent.send_keys", ["target": target, "keys": keys], as: Ignored.self)
    }

    /// Types `text` literally into the pane, without Enter.
    public func sendText(paneId: String, text: String) async throws {
        _ = try await call("pane.send_text", ["pane_id": paneId, "text": text], as: Ignored.self)
    }

    /// Creates a tab in `workspaceId` and returns its root pane.
    public func createTab(workspaceId: String, cwd: String?, label: String?) async throws -> HerdrPane {
        struct R: Decodable { var tab: HerdrTab; var rootPane: HerdrPane }
        var params: [String: Any] = ["workspace_id": workspaceId, "focus": false]
        if let cwd { params["cwd"] = cwd }
        if let label { params["label"] = label }
        return try await call("tab.create", params, as: R.self).rootPane
    }

    public func startAgent(name: String, kind: String, paneId: String, timeoutMs: Int = 30000) async throws -> HerdrAgent {
        struct R: Decodable { var agent: HerdrAgent }
        let params: [String: Any] = ["name": name, "kind": kind, "pane_id": paneId, "timeout_ms": timeoutMs]
        return try await call("agent.start", params, as: R.self, timeout: Double(timeoutMs) / 1000 + 10).agent
    }

    // MARK: Transport

    struct Ignored: Decodable {}

    private struct Envelope<R: Decodable>: Decodable {
        struct Err: Decodable { var code: String; var message: String }
        var result: R?
        var error: Err?
    }

    private func call<R: Decodable>(_ method: String, _ params: [String: Any], as: R.Type, timeout: TimeInterval = 15) async throws -> R {
        nextId += 1
        let request: [String: Any] = ["id": "relay-\(nextId)", "method": method, "params": params]
        let body = try JSONSerialization.data(withJSONObject: request)
        let line = try await Self.roundTrip(socketPath: socketPath, body: body, timeout: timeout)
        let env: Envelope<R>
        do {
            env = try Self.decoder.decode(Envelope<R>.self, from: line)
        } catch {
            throw HerdrError.decoding("\(method): \(error)")
        }
        if let e = env.error { throw HerdrError.remote(code: e.code, message: e.message) }
        guard let result = env.result else { throw HerdrError.decoding("\(method): empty response") }
        return result
    }

    private static func roundTrip(socketPath: String, body: Data, timeout: TimeInterval) async throws -> Data {
        try await withCheckedThrowingContinuation { cont in
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    let sock = try UnixSocket(path: socketPath, readTimeout: timeout)
                    defer { sock.close() }
                    try sock.writeLine(body)
                    cont.resume(returning: try sock.readLine())
                } catch {
                    cont.resume(throwing: error)
                }
            }
        }
    }
}

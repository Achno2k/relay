import Foundation

public enum HerdError: LocalizedError, Equatable, Sendable {
    case unauthorized
    case http(status: Int, code: String?, message: String?)
    case badResponse
    case invalidPairingLink

    public var errorDescription: String? {
        switch self {
        case .unauthorized: "The bridge rejected the token. Pair again."
        case .http(let status, _, let message): message ?? "The bridge returned HTTP \(status)."
        case .badResponse: "The bridge sent something unexpected."
        case .invalidPairingLink: "That isn't a Herd pairing code."
        }
    }
}

/// REST half of docs/api.md.
public struct APIClient: Sendable {
    public let baseURL: URL
    let token: String
    let session: URLSession

    public init(baseURL: URL, token: String, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.token = token
        self.session = session
    }

    public func health() async throws -> Bool {
        struct Health: Decodable { var ok: Bool }
        let h: Health = try await send("GET", "/health")
        return h.ok
    }

    public func workspaces() async throws -> [Workspace] { try await send("GET", "/workspaces") }

    public func agents() async throws -> [Agent] { try await send("GET", "/agents") }

    public func agent(id: String) async throws -> Agent { try await send("GET", "/agents/\(Self.encode(id))") }

    public func messages(agentId: String, before: String? = nil, limit: Int = 50) async throws -> MessagePage {
        var query = [URLQueryItem(name: "limit", value: String(limit))]
        if let before { query.append(URLQueryItem(name: "before", value: before)) }
        return try await send("GET", "/agents/\(Self.encode(agentId))/messages", query: query)
    }

    public func prompt(agentId: String, text: String, attachments: [String] = []) async throws {
        struct Body: Encodable { var text: String; var attachments: [String]? }
        let body = Body(text: text, attachments: attachments.isEmpty ? nil : attachments)
        try await sendIgnoringBody("POST", "/agents/\(Self.encode(agentId))/prompt", body: body)
    }

    public func machine() async throws -> Machine { try await send("GET", "/machine") }

    public func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment {
        var request = URLRequest(url: url("/agents/\(Self.encode(agentId))/attachments"))
        request.httpMethod = "POST"
        request.timeoutInterval = 120
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue(contentType, forHTTPHeaderField: "Content-Type")
        request.setValue(
            filename.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-._~"))) ?? "file",
            forHTTPHeaderField: "X-Filename"
        )
        let (body, response) = try await session.upload(for: request, from: data, delegate: UploadProgress(progress))
        return try HerdJSON.decoder().decode(Attachment.self, from: try check(body, response).0)
    }

    public func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        let (data, _) = try await raw("GET", "/agents/\(Self.encode(agentId))/attachments/\(Self.encode(attachmentId))", timeout: 60)
        return data
    }

    public func sendKeys(agentId: String, keys: [String]) async throws {
        struct Body: Encodable { var keys: [String] }
        try await sendIgnoringBody("POST", "/agents/\(Self.encode(agentId))/keys", body: Body(keys: keys))
    }

    public func sendText(agentId: String, text: String, submit: Bool = true) async throws {
        struct Body: Encodable { var text: String; var submit: Bool }
        try await sendIgnoringBody("POST", "/agents/\(Self.encode(agentId))/text", body: Body(text: text, submit: submit))
    }

    public func approval(agentId: String) async throws -> Approval? {
        let (data, status) = try await raw("GET", "/agents/\(Self.encode(agentId))/approval")
        if status == 204 || data.isEmpty { return nil }
        return try HerdJSON.decoder().decode(Approval.self, from: data)
    }

    public func controls() async throws -> ControlsCatalog { try await send("GET", "/controls") }

    public func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        let (data, _) = try await raw(
            "POST", "/agents/\(Self.encode(agentId))/control",
            body: try HerdJSON.encoder().encode(request), timeout: request.timeout
        )
        return try HerdJSON.decoder().decode(Agent.self, from: data)
    }

    public func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        try await send("POST", "/agents", body: request)
    }

    // MARK: - Plumbing

    /// Agent ids contain `:`; encode everything outside the unreserved set.
    public static func encode(_ id: String) -> String {
        id.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-._~"))) ?? id
    }

    public func url(_ path: String, query: [URLQueryItem] = []) -> URL {
        var comps = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        let basePath = comps.percentEncodedPath.hasSuffix("/") ? String(comps.percentEncodedPath.dropLast()) : comps.percentEncodedPath
        comps.percentEncodedPath = basePath + path
        comps.queryItems = query.isEmpty ? nil : query
        return comps.url!
    }

    private func send<T: Decodable>(_ method: String, _ path: String, query: [URLQueryItem] = []) async throws -> T {
        let (data, _) = try await raw(method, path, query: query, body: nil)
        return try HerdJSON.decoder().decode(T.self, from: data)
    }

    private func send<T: Decodable, B: Encodable>(_ method: String, _ path: String, body: B) async throws -> T {
        let (data, _) = try await raw(method, path, body: try HerdJSON.encoder().encode(body))
        return try HerdJSON.decoder().decode(T.self, from: data)
    }

    private func sendIgnoringBody<B: Encodable>(_ method: String, _ path: String, body: B) async throws {
        _ = try await raw(method, path, body: try HerdJSON.encoder().encode(body))
    }

    private func raw(
        _ method: String, _ path: String, query: [URLQueryItem] = [], body: Data? = nil, timeout: TimeInterval = 15
    ) async throws -> (Data, Int) {
        var request = URLRequest(url: url(path, query: query))
        request.httpMethod = method
        request.timeoutInterval = timeout
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, response) = try await session.data(for: request)
        return try check(data, response)
    }

    private func check(_ data: Data, _ response: URLResponse) throws -> (Data, Int) {
        guard let http = response as? HTTPURLResponse else { throw HerdError.badResponse }
        switch http.statusCode {
        case 200..<300:
            return (data, http.statusCode)
        case 401, 403:
            throw HerdError.unauthorized
        default:
            let detail = try? HerdJSON.decoder().decode(APIErrorBody.self, from: data)
            throw HerdError.http(status: http.statusCode, code: detail?.error.code, message: detail?.error.message)
        }
    }
}

/// Reports upload progress for one task.
private final class UploadProgress: NSObject, URLSessionTaskDelegate, Sendable {
    let onProgress: @Sendable (Double) -> Void

    init(_ onProgress: @escaping @Sendable (Double) -> Void) {
        self.onProgress = onProgress
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64, totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
        guard totalBytesExpectedToSend > 0 else { return }
        onProgress(Double(totalBytesSent) / Double(totalBytesExpectedToSend))
    }
}

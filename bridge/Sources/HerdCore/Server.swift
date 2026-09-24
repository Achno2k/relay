import Foundation
import Hummingbird
import NIOCore
import NIOFoundationCompat
import HummingbirdWebSocket

public typealias HerdContext = BasicWebSocketRequestContext

/// Bearer auth on everything but `/health`; `/ws` takes `?token=`.
struct AuthMiddleware: RouterMiddleware {
    typealias Context = HerdContext
    let token: String

    func handle(_ request: Request, context: Context, next: (Request, Context) async throws -> Response) async throws -> Response {
        let path = request.uri.path
        if path == "/health" { return try await next(request, context) }
        let presented: String?
        if path == "/ws" {
            presented = request.uri.queryParameters.get("token")
        } else if let h = request.headers[.authorization], h.lowercased().hasPrefix("bearer ") {
            presented = String(h.dropFirst(7)).trimmingCharacters(in: .whitespaces)
        } else {
            presented = nil
        }
        guard let presented, Self.constantTimeEqual(presented, token) else { throw APIError.unauthorized }
        return try await next(request, context)
    }

    static func constantTimeEqual(_ a: String, _ b: String) -> Bool {
        let x = Array(a.utf8), y = Array(b.utf8)
        guard x.count == y.count else { return false }
        var diff: UInt8 = 0
        for i in 0..<x.count { diff |= x[i] ^ y[i] }
        return diff == 0
    }
}

/// Renders every failure as `{"error": {"code", "message"}}`.
struct ErrorMiddleware: RouterMiddleware {
    typealias Context = HerdContext

    func handle(_ request: Request, context: Context, next: (Request, Context) async throws -> Response) async throws -> Response {
        do {
            return try await next(request, context)
        } catch let e as APIError {
            throw e
        } catch let e as HerdrError {
            throw APIError.from(e)
        } catch let e as HTTPError {
            let code = switch e.status {
            case .notFound: "not_found"
            case .badRequest: "bad_request"
            case .methodNotAllowed: "method_not_allowed"
            case .unauthorized: "unauthorized"
            default: "error"
            }
            throw APIError(e.status, code, e.body ?? e.status.reasonPhrase)
        } catch is DecodingError {
            throw APIError.badRequest("invalid JSON body")
        }
    }
}

public enum HerdRoutes {
    struct PromptBody: Decodable { var text: String?; var attachments: [String]? }
    struct KeysBody: Decodable { var keys: [String] }
    struct TextBody: Decodable { var text: String; var submit: Bool? }
    struct ControlBody: Decodable {
        var model: String?
        var permissionMode: String?
        var effort: String?
        var command: String?

        func request() throws -> ControlRequest {
            let set = [model, permissionMode, effort, command].compactMap { $0 }
            guard set.count == 1 else { throw APIError.badRequest("send exactly one of model, permissionMode, effort, command") }
            if let model { return .model(model) }
            if let permissionMode { return .permissionMode(permissionMode) }
            if let effort { return .effort(effort) }
            switch command {
            case "compact": return .compact
            case "clear": return .clear
            default: throw APIError.badRequest("command must be compact or clear")
            }
        }
    }
    struct CreateBody: Decodable {
        var workspaceId: String
        var kind: String
        var name: String?
        var prompt: String?
    }
    struct Health: Encodable { var ok = true; var version = Herd.version }
    struct Empty: Encodable {}

    public static func router(service: AgentService, hub: EventHub, token: String) -> Router<HerdContext> {
        let router = Router(context: HerdContext.self)
        router.add(middleware: ErrorMiddleware())
        router.add(middleware: AuthMiddleware(token: token))

        router.get("/health") { _, _ in try JSONResponse.make(Health()) }

        router.get("/workspaces") { _, _ in try JSONResponse.make(try await service.workspaces()) }

        router.get("/agents") { _, _ in try JSONResponse.make(try await service.agents()) }

        router.post("/agents") { request, context in
            let body = try await decode(CreateBody.self, request, context)
            guard !body.kind.isEmpty else { throw APIError.badRequest("kind is required") }
            if let n = body.name, !AgentService.isValidName(n) {
                throw APIError.badRequest("name must match [a-z][a-z0-9_-]{0,31}")
            }
            let agent = try await service.create(workspaceId: body.workspaceId, kind: body.kind, name: body.name, prompt: body.prompt)
            return try JSONResponse.make(agent, status: .created)
        }

        router.get("/agents/:id") { _, context in
            try JSONResponse.make(try await service.agent(id: try agentId(context)))
        }

        router.get("/agents/:id/messages") { request, context in
            let q = request.uri.queryParameters
            let limit = min(max(q.get("limit", as: Int.self) ?? 50, 1), 500)
            let page = try await service.messages(id: try agentId(context), before: q.get("before"), limit: limit)
            return try JSONResponse.make(page)
        }

        router.post("/agents/:id/prompt") { request, context in
            let body = try await decode(PromptBody.self, request, context)
            let text = body.text ?? ""
            let attachments = body.attachments ?? []
            guard !text.isEmpty || !attachments.isEmpty else { throw APIError.badRequest("text or attachments is required") }
            try await service.prompt(id: try agentId(context), text: text, attachments: attachments)
            return try JSONResponse.make(Empty(), status: .accepted)
        }

        router.post("/agents/:id/keys") { request, context in
            let body = try await decode(KeysBody.self, request, context)
            guard !body.keys.isEmpty else { throw APIError.badRequest("keys is required") }
            try await service.sendKeys(id: try agentId(context), keys: body.keys)
            return try JSONResponse.make(Empty(), status: .accepted)
        }

        router.post("/agents/:id/text") { request, context in
            let body = try await decode(TextBody.self, request, context)
            guard !body.text.isEmpty else { throw APIError.badRequest("text is required") }
            try await service.text(id: try agentId(context), text: body.text, submit: body.submit ?? true)
            return try JSONResponse.make(Empty(), status: .accepted)
        }

        router.get("/machine") { _, _ in try JSONResponse.make(Machine.current()) }

        router.post("/agents/:id/attachments") { request, context in
            let raw = request.headers[.init("X-Filename")!] ?? "file"
            let filename = raw.removingPercentEncoding ?? raw
            let buffer: ByteBuffer
            do {
                buffer = try await request.body.collect(upTo: UploadStore.maxBytes)
            } catch is NIOTooManyBytesError {
                throw APIError(.contentTooLarge, "too_large", "files are limited to 20 MB")
            }
            guard buffer.readableBytes > 0 else { throw APIError.badRequest("empty file") }
            let att = try await service.upload(id: try agentId(context), data: Data(buffer: buffer), filename: filename)
            return try JSONResponse.make(att, status: .created)
        }

        router.get("/agents/:id/attachments/:attachmentId") { _, context in
            _ = try agentId(context)
            guard let id = context.parameters.get("attachmentId"), let url = service.uploads.find(id),
                  let data = try? Data(contentsOf: url)
            else { throw APIError.notFound("no attachment (uploads expire after 7 days)") }
            return Response(
                status: .ok,
                headers: [.contentType: UploadStore.mimeType(for: url), .contentLength: String(data.count), .cacheControl: "private, max-age=604800"],
                body: .init(byteBuffer: ByteBuffer(bytes: data)))
        }

        router.get("/controls") { _, _ in try JSONResponse.make(ClaudeControls.catalog) }

        router.get("/agents/:id/controls") { _, context in
            try JSONResponse.make(try await service.controls(id: try agentId(context)))
        }

        router.post("/agents/:id/control") { request, context in
            let body = try await decode(ControlBody.self, request, context)
            let agent = try await service.control(id: try agentId(context), try body.request())
            return try JSONResponse.make(agent, status: .accepted)
        }

        router.get("/agents/:id/approval") { _, context in
            guard let approval = try await service.approval(id: try agentId(context)) else {
                return Response(status: .noContent)
            }
            return try JSONResponse.make(approval)
        }

        router.ws("/ws") { inbound, outbound, _ in
            let (id, events) = hub.subscribe()
            defer { hub.unsubscribe(id) }
            try await outbound.write(.text(String(decoding: try JSONResponse.encode(ServerEvent.hello), as: UTF8.self)))
            try await withThrowingTaskGroup(of: Void.self) { group in
                // Server -> client only; draining inbound notices when the client goes away.
                group.addTask { for try await _ in inbound {} }
                group.addTask {
                    for await e in events {
                        try await outbound.write(.text(String(decoding: try JSONResponse.encode(e), as: UTF8.self)))
                    }
                }
                try await group.next()
                group.cancelAll()
            }
        }

        return router
    }

    static func agentId(_ context: HerdContext) throws -> String {
        guard let raw = context.parameters.get("id") else { throw APIError.badRequest("missing agent id") }
        return raw.removingPercentEncoding ?? raw
    }

    static func decode<T: Decodable>(_ type: T.Type, _ request: Request, _ context: HerdContext) async throws -> T {
        do {
            return try await request.decode(as: T.self, context: context)
        } catch {
            throw APIError.badRequest("invalid JSON body")
        }
    }
}

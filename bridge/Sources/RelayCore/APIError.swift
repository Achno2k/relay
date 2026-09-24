import Foundation
import Hummingbird

/// `{"error": {"code": "...", "message": "..."}}` with the matching status.
public struct APIError: Error, HTTPResponseError, Sendable {
    public var status: HTTPResponse.Status
    public var code: String
    public var message: String

    public init(_ status: HTTPResponse.Status, _ code: String, _ message: String) {
        self.status = status
        self.code = code
        self.message = message
    }

    public static func notFound(_ m: String) -> APIError { .init(.notFound, "not_found", m) }
    public static func badRequest(_ m: String) -> APIError { .init(.badRequest, "bad_request", m) }
    public static let unauthorized = APIError(.unauthorized, "unauthorized", "missing or invalid token")

    public func response(from request: Request, context: some RequestContext) throws -> Response {
        try JSONResponse.make(ErrorBody(error: .init(code: code, message: message)), status: status)
    }

    /// Maps herdr failures onto HTTP.
    public static func from(_ error: HerdrError) -> APIError {
        switch error {
        case .remote(let code, let message):
            if code.contains("not_found") { return .init(.notFound, "not_found", message) }
            if code == "agent_blocked" { return .init(.conflict, "agent_blocked", message) }
            if code.hasPrefix("invalid") { return .init(.badRequest, code, message) }
            return .init(.badGateway, code, message)
        case .unavailable(let m):
            return .init(.serviceUnavailable, "herdr_unavailable", m)
        case .timeout:
            return .init(.gatewayTimeout, "herdr_timeout", error.description)
        case .io(let m), .decoding(let m):
            return .init(.badGateway, "herdr_error", m)
        }
    }
}

struct ErrorBody: Codable {
    struct Inner: Codable { var code: String; var message: String }
    var error: Inner
}

public enum JSONResponse {
    static let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.outputFormatting = [.withoutEscapingSlashes]
        return e
    }()

    public static func encode(_ value: some Encodable) throws -> Data {
        try encoder.encode(value)
    }

    public static func make(_ value: some Encodable, status: HTTPResponse.Status = .ok) throws -> Response {
        let data = try encode(value)
        return Response(
            status: status,
            headers: [.contentType: "application/json; charset=utf-8"],
            body: .init(byteBuffer: ByteBuffer(bytes: data)))
    }
}

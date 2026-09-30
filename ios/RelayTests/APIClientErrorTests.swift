import Foundation
import RelayKit
import Testing
@testable import Relay

/// Stubs `URLProtocol` so `APIClient` can be driven against canned responses and transport errors
/// without a real bridge. Stubs are keyed by absolute URL and guarded by a lock: Swift Testing may
/// run these concurrently.
final class StubURLProtocol: URLProtocol, @unchecked Sendable {
    struct Stub: Sendable {
        var data = Data()
        var statusCode = 200
        var transportError: URLError.Code?
        var contentType = "application/json"
    }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var stubs: [String: Stub] = [:]

    static func stub(_ url: URL, _ stub: Stub) {
        lock.lock(); defer { lock.unlock() }
        stubs[url.absoluteString] = stub
    }

    static func session() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let key = request.url?.absoluteString ?? ""
        Self.lock.lock()
        let stub = Self.stubs[key]
        Self.lock.unlock()
        guard let stub else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        if let code = stub.transportError {
            client?.urlProtocol(self, didFailWithError: URLError(code))
            return
        }
        let response = HTTPURLResponse(
            url: request.url!, statusCode: stub.statusCode, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": stub.contentType]
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

/// Every failure `APIClient` can hand back must be a curated `RelayError`, never Swift/Foundation's
/// own error text (docs/tasks/round-5/ios-harden.md: "no raw JSON or Swift error text in the UI").
@Suite("APIClient error mapping")
struct APIClientErrorTests {
    private func client(baseURL: URL = URL(string: "http://stub.local")!) -> APIClient {
        APIClient(baseURL: baseURL, token: "t", session: StubURLProtocol.session())
    }

    @Test func malformedBodyBecomesBadResponseNotADecodingError() async throws {
        let c = client()
        StubURLProtocol.stub(c.url("/workspaces"), .init(data: Data("not json".utf8), statusCode: 200))
        await #expect(throws: RelayError.badResponse) {
            _ = try await c.workspaces()
        }
    }

    @Test func truncatedJSONBecomesBadResponse() async throws {
        let c = client()
        StubURLProtocol.stub(c.url("/agents"), .init(data: Data(#"[{"id":"w1:p1""#.utf8), statusCode: 200))
        await #expect(throws: RelayError.badResponse) {
            _ = try await c.agents()
        }
    }

    @Test func offlineBecomesUnreachableNotTimedOut() async throws {
        let c = client()
        StubURLProtocol.stub(c.url("/agents"), .init(transportError: .notConnectedToInternet))
        do {
            _ = try await c.agents()
            Issue.record("expected an error")
        } catch RelayError.unreachable(let timedOut) {
            #expect(!timedOut)
        }
    }

    @Test func timeoutBecomesUnreachableTimedOut() async throws {
        let c = client()
        StubURLProtocol.stub(c.url("/agents"), .init(transportError: .timedOut))
        do {
            _ = try await c.agents()
            Issue.record("expected an error")
        } catch RelayError.unreachable(let timedOut) {
            #expect(timedOut)
        }
    }

    @Test func unauthorizedStatusBecomesUnauthorized() async throws {
        let c = client()
        StubURLProtocol.stub(c.url("/agents"), .init(statusCode: 401))
        await #expect(throws: RelayError.unauthorized) {
            _ = try await c.agents()
        }
    }

    @Test func errorMessagesAreNeverEmptyOrRaw() throws {
        // A DecodingError's own `localizedDescription` (what `badResponse` replaces) looks like
        // "The data couldn't be read because it is missing." or worse; assert the curated copy
        // instead so a future edit can't quietly go back to leaking it.
        for error: RelayError in [
            .unauthorized, .badResponse, .invalidPairingLink,
            .unreachable(timedOut: true), .unreachable(timedOut: false),
            .http(status: 500, code: "internal", message: nil),
        ] {
            let text = try #require(error.errorDescription)
            #expect(!text.isEmpty)
            #expect(!text.contains("Swift.DecodingError"))
            #expect(!text.contains("NSError"))
        }
    }
}

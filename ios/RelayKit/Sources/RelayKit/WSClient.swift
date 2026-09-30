import Foundation

/// `/ws?token=`. Yields `.connected` on `hello`, `.disconnected` when the socket drops
/// (after `.rejected` if the bridge refused the upgrade), then reconnects with exponential backoff (1s, 2s, 4s … capped at 30s).
public struct WSClient: Sendable {
    public let url: URL
    let session: URLSession

    public init(baseURL: URL, token: String, session: URLSession = .shared) {
        var comps = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        comps.scheme = comps.scheme == "https" ? "wss" : "ws"
        let basePath = comps.path.hasSuffix("/") ? String(comps.path.dropLast()) : comps.path
        comps.path = basePath + "/ws"
        comps.queryItems = [URLQueryItem(name: "token", value: token)]
        self.url = comps.url!
        self.session = session
    }

    public static func backoff(attempt: Int) -> Duration {
        .seconds(min(30, 1 << min(attempt, 5)))
    }

    public func events() -> AsyncStream<ConnectionEvent> {
        let url = url
        let session = session
        return AsyncStream { continuation in
            let loop = Task {
                var attempt = 0
                while !Task.isCancelled {
                    let socket = session.webSocketTask(with: url)
                    socket.resume()
                    let pinger = Task { await Self.ping(socket) }
                    var greeted = false
                    do {
                        while !Task.isCancelled {
                            let frame = try await socket.receive()
                            let data: Data
                            switch frame {
                            case .string(let s): data = Data(s.utf8)
                            case .data(let d): data = d
                            @unknown default: continue
                            }
                            guard let event = try? RelayJSON.decoder().decode(ServerEvent.self, from: data) else { continue }
                            if !greeted {
                                // Any frame proves the socket is up; `hello` is just the usual first one.
                                greeted = true
                                attempt = 0
                                continuation.yield(.connected)
                            }
                            if event != .hello { continuation.yield(.event(event)) }
                        }
                    } catch {
                        // Fall through to reconnect.
                    }
                    pinger.cancel()
                    socket.cancel(with: .goingAway, reason: nil)
                    if Task.isCancelled { break }
                    if !greeted, let status = Self.rejection(socket.response) {
                        continuation.yield(.rejected(status: status))
                    }
                    continuation.yield(.disconnected)
                    try? await Task.sleep(for: Self.backoff(attempt: attempt))
                    attempt += 1
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in loop.cancel() }
        }
    }

    /// The status of an upgrade the bridge answered but refused; nil if it never answered or switched.
    public static func rejection(_ response: URLResponse?) -> Int? {
        guard let http = response as? HTTPURLResponse, http.statusCode != 101 else { return nil }
        return http.statusCode
    }

    /// A socket can die silently while the phone sleeps. Pings surface that as a receive error.
    /// A pong that never comes (a dead route, not a closed one) counts as dead too.
    private static func ping(_ socket: URLSessionWebSocketTask) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(15))
            if Task.isCancelled { return }
            let deadline = Task {
                try await Task.sleep(for: .seconds(10))
                socket.cancel(with: .abnormalClosure, reason: nil)
            }
            let ok = await pingResult(socket.sendPing)
            deadline.cancel()
            if !ok {
                socket.cancel(with: .abnormalClosure, reason: nil)
                return
            }
        }
    }

    /// Whether one ping got its pong. `sendPing` can call its handler twice when the socket is cancelled
    /// with a ping in flight (a bridge restart); a checked continuation traps on the second resume (R10-1).
    static func pingResult(_ sendPing: (@escaping @Sendable ((any Error)?) -> Void) -> Void) async -> Bool {
        let once = Once()
        return await withCheckedContinuation { (c: CheckedContinuation<Bool, Never>) in
            sendPing { error in
                if once.claim() { c.resume(returning: error == nil) }
            }
        }
    }
}

/// True for the first caller only.
final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var claimed = false

    func claim() -> Bool {
        lock.withLock {
            defer { claimed = true }
            return !claimed
        }
    }
}

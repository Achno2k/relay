import Foundation
import Synchronization

/// A herdr event, reduced to what the bridge needs: which pane something happened to.
/// The payload is only a hint to refresh; status is always re-read with `agent.list`/`agent.get`.
public struct HerdrEvent: Sendable, Equatable {
    public var kind: String
    public var paneId: String?

    public init(kind: String, paneId: String?) {
        self.kind = kind
        self.paneId = paneId
    }

    /// Parses one `{"event": ..., "data": {...}}` line. Returns nil for responses and junk.
    public static func parse(_ line: Data) -> HerdrEvent? {
        guard let o = (try? JSONSerialization.jsonObject(with: line)) as? [String: Any],
              let kind = o["event"] as? String
        else { return nil }
        let data = o["data"] as? [String: Any]
        let pane = (data?["pane_id"] as? String) ?? ((data?["pane"] as? [String: Any])?["pane_id"] as? String)
        return HerdrEvent(kind: kind.replacingOccurrences(of: "_", with: "."), paneId: pane)
    }
}

/// Long-lived `events.subscribe` connection with reconnect.
///
/// herdr needs one `pane.agent_status_changed` subscription per pane, so when the set of
/// agent panes changes the stream reconnects with a new subscription list.
public final class HerdrEventStream: Sendable {
    public let events: AsyncStream<HerdrEvent>
    private let continuation: AsyncStream<HerdrEvent>.Continuation
    private let socketPath: String
    private let state = Mutex(State())

    private struct State {
        var panes: Set<String> = []
        var socket: UnixSocket?
        var stopped = false
        var started = false
    }

    static let paneEvents = ["pane.created", "pane.closed", "pane.updated", "pane.moved", "pane.exited", "pane.agent_detected"]

    public init(socketPath: String) {
        self.socketPath = socketPath
        (events, continuation) = AsyncStream.makeStream(of: HerdrEvent.self, bufferingPolicy: .bufferingNewest(256))
    }

    public func start() {
        let shouldStart = state.withLock { s -> Bool in
            defer { s.started = true }
            return !s.started
        }
        guard shouldStart else { return }
        let thread = Thread { [self] in loop() }
        thread.name = "relay.events"
        thread.start()
    }

    public func stop() {
        let sock = state.withLock { s -> UnixSocket? in
            s.stopped = true
            return s.socket
        }
        sock?.close()
        continuation.finish()
    }

    /// Updates the panes to watch for status changes; reconnects if the set changed.
    public func watch(panes: Set<String>) {
        let sock = state.withLock { s -> UnixSocket? in
            guard s.panes != panes else { return nil }
            s.panes = panes
            return s.socket
        }
        sock?.close()
    }

    private func loop() {
        var backoff: TimeInterval = 0.5
        while !state.withLock({ $0.stopped }) {
            let panes = state.withLock { $0.panes }
            var subs: [[String: Any]] = Self.paneEvents.map { ["type": $0] }
            subs += panes.sorted().map { ["type": "pane.agent_status_changed", "pane_id": $0] }
            do {
                let sock = try UnixSocket(path: socketPath, readTimeout: nil)
                state.withLock { $0.socket = sock }
                let req: [String: Any] = ["id": "relay-events", "method": "events.subscribe", "params": ["subscriptions": subs]]
                try sock.writeLine(try JSONSerialization.data(withJSONObject: req))
                let first = try sock.readLine()
                if let o = (try? JSONSerialization.jsonObject(with: first)) as? [String: Any], o["error"] != nil {
                    // Usually a pane closed between listing and subscribing: ask for a resync and retry.
                    continuation.yield(HerdrEvent(kind: "resync", paneId: nil))
                    throw HerdrError.io("subscribe rejected")
                }
                backoff = 0.5
                // Anything could have changed while we were disconnected.
                continuation.yield(HerdrEvent(kind: "resync", paneId: nil))
                while true {
                    let line = try sock.readLine()
                    if let e = HerdrEvent.parse(line) { continuation.yield(e) }
                }
            } catch {
                state.withLock { $0.socket = nil }
                if state.withLock({ $0.stopped }) { break }
                // A deliberate resubscribe closes the socket; reconnect quickly in that case.
                Thread.sleep(forTimeInterval: backoff)
                backoff = min(backoff * 2, 5)
            }
        }
    }
}

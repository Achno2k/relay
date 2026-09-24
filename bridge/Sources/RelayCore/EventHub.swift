import Foundation
import Synchronization

/// Fans server events out to every connected WebSocket client. Deltas only; nothing is replayed.
public final class EventHub: Sendable {
    private let subscribers = Mutex<[UUID: AsyncStream<ServerEvent>.Continuation]>([:])

    public init() {}

    public func subscribe() -> (UUID, AsyncStream<ServerEvent>) {
        let (stream, cont) = AsyncStream.makeStream(of: ServerEvent.self, bufferingPolicy: .bufferingNewest(1000))
        let id = UUID()
        subscribers.withLock { $0[id] = cont }
        return (id, stream)
    }

    public func unsubscribe(_ id: UUID) {
        let cont = subscribers.withLock { $0.removeValue(forKey: id) }
        cont?.finish()
    }

    public func broadcast(_ event: ServerEvent) {
        let conts = subscribers.withLock { Array($0.values) }
        for c in conts { c.yield(event) }
    }

    public var count: Int { subscribers.withLock { $0.count } }
}

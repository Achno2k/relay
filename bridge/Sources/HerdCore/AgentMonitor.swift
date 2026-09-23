import Foundation
import ServiceLifecycle

/// Turns herdr events into WebSocket deltas and keeps one transcript tailer per live agent.
///
/// Every event (and a slow timer, as a safety net) triggers a fresh `agent.list`; the event
/// payload is never trusted for status.
public actor AgentMonitor: Service {
    let service: AgentService
    let hub: EventHub
    let stream: HerdrEventStream
    let interval: Duration

    private var known: [String: Agent] = [:]
    private var tailers: [String: TranscriptTailer] = [:]
    private var initialized = false
    private var refreshing = false
    private var pending = false

    public init(service: AgentService, hub: EventHub, stream: HerdrEventStream, interval: Duration = .seconds(5)) {
        self.service = service
        self.hub = hub
        self.stream = stream
        self.interval = interval
    }

    public func run() async throws {
        stream.start()
        await cancelWhenGracefulShutdown {
            await withTaskGroup(of: Void.self) { group in
                group.addTask {
                    for await _ in self.stream.events { await self.trigger() }
                }
                group.addTask {
                    while !Task.isCancelled {
                        try? await Task.sleep(for: self.interval)
                        await self.trigger()
                    }
                }
                await self.trigger()
                await group.waitForAll()
            }
        }
        stream.stop()
        for t in tailers.values { t.stop() }
        tailers.removeAll()
    }

    /// Coalesces bursts of events into back-to-back refreshes.
    public func trigger() async {
        if refreshing {
            pending = true
            return
        }
        refreshing = true
        repeat {
            pending = false
            await refresh()
        } while pending
        refreshing = false
    }

    private func refresh() async {
        guard let snaps = try? await service.snapshots() else { return }
        var next: [String: Agent] = [:]
        for s in snaps {
            next[s.agent.id] = s.agent
            if initialized {
                if let old = known[s.agent.id] {
                    if !Self.sameExceptTime(old, s.agent) { hub.broadcast(.agentUpdated(s.agent)) }
                } else {
                    hub.broadcast(.agentCreated(s.agent))
                }
            }
            syncTailer(s)
        }
        for id in known.keys where next[id] == nil {
            hub.broadcast(.agentClosed(id))
        }
        for id in tailers.keys where next[id] == nil {
            tailers.removeValue(forKey: id)?.stop()
        }
        known = next
        initialized = true
        stream.watch(panes: Set(next.keys))
    }

    private func syncTailer(_ s: AgentService.Snapshot) {
        let id = s.agent.id
        guard let ref = s.transcript else {
            tailers.removeValue(forKey: id)?.stop()
            return
        }
        if let t = tailers[id], t.url == ref.url, !t.isDead { return }
        tailers.removeValue(forKey: id)?.stop()
        let hub = self.hub
        let t = TranscriptTailer(url: ref.url, format: ref.format, cwd: ref.cwd) { message in
            hub.broadcast(.messageUpserted(agentId: id, message: message))
        }
        t.start()
        tailers[id] = t
    }

    static func sameExceptTime(_ a: Agent, _ b: Agent) -> Bool {
        var b = b
        b.updatedAt = a.updatedAt
        return a == b
    }

    public var tailerCount: Int { tailers.count }
}

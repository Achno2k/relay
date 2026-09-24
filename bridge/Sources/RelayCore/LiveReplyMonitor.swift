import Foundation
import ServiceLifecycle

/// Streams `reply.live` previews for every working agent, while at least one `/ws` client is
/// connected. `AgentMonitor` feeds it the agent list it already refreshes and the messages its
/// transcript tailers already parse, so this adds exactly one `agent.read` per working agent per
/// tick and no other herdr calls.
public actor LiveReplyMonitor: Service {
    private let herdr: HerdrClient
    private let hub: EventHub
    private let tracker: LiveReplyTracker
    private let interval: Duration

    private struct Tracked {
        var agent: Agent
        var cwd: String?
    }
    private var agents: [String: Tracked] = [:]

    public init(herdr: HerdrClient, hub: EventHub, tracker: LiveReplyTracker = LiveReplyTracker(),
                interval: Duration = .milliseconds(250)) {
        self.herdr = herdr
        self.hub = hub
        self.tracker = tracker
        self.interval = interval
    }

    /// Fed by `AgentMonitor` on every refresh with the current live agents.
    public func update(_ snapshots: [AgentService.Snapshot]) async {
        var next: [String: Tracked] = [:]
        for s in snapshots {
            next[s.agent.id] = Tracked(agent: s.agent, cwd: s.raw.cwd ?? s.raw.foregroundCwd)
        }
        for (id, was) in agents where was.agent.status == .working && next[id]?.agent.status != .working {
            if let ev = await tracker.stopped(agentId: id) { hub.broadcast(ev) }
        }
        for id in agents.keys where next[id] == nil {
            await tracker.remove(agentId: id)
        }
        agents = next
    }

    /// Fed by `AgentMonitor` whenever a transcript message grows, so the preview never repeats it.
    public func landed(agentId: String, message: Message) async {
        guard message.role == .assistant else { return }
        let text = message.blocks.compactMap { if case .text(let t) = $0 { t } else { nil } }.joined(separator: "\n\n")
        if let ev = await tracker.landed(agentId: agentId, text: text) { hub.broadcast(ev) }
    }

    public func run() async throws {
        await cancelWhenGracefulShutdown { [self] in
            while !Task.isCancelled {
                await self.tick()
                try? await Task.sleep(for: self.interval)
            }
        }
    }

    private func tick() async {
        guard hub.count > 0 else { return }
        let working = agents.values.filter { $0.agent.status == .working && TranscriptState.parsedKinds.contains($0.agent.kind) }
        guard !working.isEmpty else { return }
        await withTaskGroup(of: Void.self) { group in
            for t in working {
                group.addTask { await self.poll(t) }
            }
        }
    }

    /// `.visible`, not `.recentUnwrapped`: herdr refuses to scroll an alternate-screen pane's
    /// history while it's actively being written to (`agent_not_idle`), which a working agent
    /// always is. The viewport is all a live poll can read, and it's enough for a tail preview.
    private func poll(_ t: Tracked) async {
        guard let read = try? await herdr.read(t.agent.id, source: .visible) else { return }
        let parsed = LiveReplyParser.extract(screen: read.text, kind: t.agent.kind)
        let scrubbed = parsed.map { PathScrubber(cwd: t.cwd).scrub($0) }
        if let ev = await tracker.offer(agentId: t.agent.id, text: scrubbed) { hub.broadcast(ev) }
    }
}

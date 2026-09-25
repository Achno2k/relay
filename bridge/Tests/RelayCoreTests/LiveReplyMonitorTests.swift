import Foundation
import Synchronization
import Testing
@testable import RelayCore

/// The next `reply.live` frame for `agentId` matching `nonNil` (whether its text is present),
/// ignoring any other event or a `reply.live` for a different agent, or `nil` if none arrives
/// within `timeout`. A fresh iterator each call is fine: `EventHub`'s `AsyncStream` buffer is
/// shared storage, not per-iterator state, and these tests only ever read from one place at a time.
private func nextReplyLiveText(_ events: AsyncStream<ServerEvent>, agentId: String, nonNil: Bool, timeout: Duration = .seconds(2)) async -> String?? {
    await withTaskGroup(of: String???.self) { group in
        group.addTask {
            for await e in events {
                if case .replyLive(let id, let text, _, _) = e, id == agentId, (text != nil) == nonNil {
                    return .some(text)
                }
            }
            return .some(nil)
        }
        group.addTask {
            try? await Task.sleep(for: timeout)
            return .some(nil)
        }
        let result = (await group.next() ?? nil) ?? nil
        group.cancelAll()
        return result
    }
}

/// End-to-end: `AgentMonitor` feeds `LiveReplyMonitor` from a fake herdr, and a working claude
/// agent's screen produces a `reply.live` frame on the hub, same wiring as `RelayApp`.
@Suite(.serialized) struct LiveReplyMonitorTests {
    @Test func workingAgentProducesALiveFrame() async throws {
        let status = Mutex("idle")
        let screen = Mutex("❯ hi\n\n⏺ Not yet")

        let fake = try FakeHerdr { method, params in
            let agent = World.agent("w1:p1", name: "chat", status: status.withLock { $0 }, cwd: "/Users/dev/shop-api", session: nil)
            switch method {
            case "agent.list": return ["type": "agent_list", "agents": [agent]]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.read":
                let text = (params["source"] as? String) == "detection" ? "" : screen.withLock { $0 }
                return ["type": "pane_read", "read": ["pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1",
                                                       "source": params["source"] as? String ?? "", "format": "text", "text": text, "revision": 1, "truncated": false]]
            default: return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }

        let herdr = HerdrClient(socketPath: fake.socketPath)
        let service = AgentService(herdr: herdr, locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
        let hub = EventHub()
        let liveReply = LiveReplyMonitor(herdr: herdr, hub: hub, interval: .milliseconds(20))
        let monitor = AgentMonitor(
            service: service, hub: hub, stream: HerdrEventStream(socketPath: fake.socketPath), interval: .seconds(3600),
            onSnapshots: { snaps in Task { await liveReply.update(snaps) } })

        let (sid, events) = hub.subscribe()
        defer { hub.unsubscribe(sid) }

        // Idle: no working agents yet, no frame even once the monitor is polling.
        await monitor.trigger()
        let liveTask = Task { try await liveReply.run() }
        defer { liveTask.cancel() }
        try await Task.sleep(for: .milliseconds(80))

        // The agent starts working with an in-progress `⏺` block on screen.
        status.withLock { $0 = "working" }
        screen.withLock { $0 = "❯ hi\n\n⏺ Hello there" }
        await monitor.trigger()

        let event = await nextReplyLiveText(events, agentId: "w1:p1", nonNil: true)
        #expect(event == .some("Hello there"))
    }

    @Test func agentGoingIdleClearsTheLiveFrame() async throws {
        let status = Mutex("working")
        let screen = Mutex("❯ hi\n\n⏺ Growing reply")

        let fake = try FakeHerdr { method, params in
            let agent = World.agent("w1:p1", name: "chat", status: status.withLock { $0 }, cwd: "/Users/dev/shop-api", session: nil)
            switch method {
            case "agent.list": return ["type": "agent_list", "agents": [agent]]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.read":
                let text = (params["source"] as? String) == "detection" ? "" : screen.withLock { $0 }
                return ["type": "pane_read", "read": ["pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1",
                                                       "source": params["source"] as? String ?? "", "format": "text", "text": text, "revision": 1, "truncated": false]]
            default: return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }

        let herdr = HerdrClient(socketPath: fake.socketPath)
        let service = AgentService(herdr: herdr, locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
        let hub = EventHub()
        let liveReply = LiveReplyMonitor(herdr: herdr, hub: hub, interval: .milliseconds(20))
        let monitor = AgentMonitor(
            service: service, hub: hub, stream: HerdrEventStream(socketPath: fake.socketPath), interval: .seconds(3600),
            onSnapshots: { snaps in Task { await liveReply.update(snaps) } })

        let (sid, events) = hub.subscribe()
        defer { hub.unsubscribe(sid) }

        await monitor.trigger()
        let liveTask = Task { try await liveReply.run() }
        defer { liveTask.cancel() }

        let firstText = await nextReplyLiveText(events, agentId: "w1:p1", nonNil: true)
        #expect(firstText == .some("Growing reply"))

        status.withLock { $0 = "idle" }
        await monitor.trigger()

        let cleared = await nextReplyLiveText(events, agentId: "w1:p1", nonNil: false)
        #expect(cleared == .some(nil))
    }
}

import Foundation
import Hummingbird
import HummingbirdTesting
import HummingbirdWebSocket
import HummingbirdWSTesting
import Synchronization
import Testing
@testable import RelayCore

/// Reads the next event from `stream`, or nil if none arrives within `timeout`. A fresh iterator
/// each call is fine here: `AsyncStream`'s buffer is shared storage, not per-iterator state, and
/// these tests only ever read from one place at a time.
private func nextEvent(_ stream: HerdrEventStream, timeout: Duration) async -> HerdrEvent? {
    await withTaskGroup(of: HerdrEvent??.self) { group in
        group.addTask {
            for await e in stream.events { return e }
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

/// Polls `condition` (which may itself have side effects, e.g. `monitor.trigger()`) until it's true
/// or `timeout` elapses. A fresh `FakeHerdr`'s listener is bound synchronously before its initializer
/// returns, but under this machine's load (several concurrent `swift test` runs) the very first probe
/// right after a restart can still lose a scheduling race — this turns a single fragile check into a
/// bounded-time wait, matching how a real client would behave (retry, not assert instantly).
@discardableResult
private func poll(timeout: Duration = .seconds(2), _ condition: () async -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    repeat {
        if await condition() { return true }
        try? await Task.sleep(for: .milliseconds(50))
    } while ContinuousClock.now < deadline
    return await condition()
}

/// Simulates herdr disappearing and coming back (README: "Kill and restart herdr's server during
/// a test to prove it"). Uses `FakeHerdr` rather than the user's real herdr, as instructed.
///
/// `.serialized`: these tests open and close real raw sockets (`FakeHerdr`) alongside a real
/// Hummingbird/NIO server (`restRouteAnswers503FastWhenHerdrDies`). Running them concurrently (the
/// default) let one test's `close()`/`shutdown()` on a raw fd race another test's NIO event loop
/// registering that same fd number, which crashed NIO's kqueue with "Bad file descriptor" in a 20-run
/// loop — a real concurrency hazard, not a flaky assertion. `RoutesTests`, which does the same kind of
/// thing, is already `.serialized` for the same reason.
@Suite(.serialized) struct SocketResilienceTests {
    static func herdrHandler(_ method: String, _ params: [String: Any]) -> Any {
        switch method {
        case "agent.list": return ["type": "agent_list", "agents": []]
        case "workspace.list": return ["type": "workspace_list", "workspaces": []]
        case "events.subscribe": return ["type": "subscription_started"]
        default: return FakeError(code: "unknown_method", message: method)
        }
    }

    @Test func restIsFastNotHungWhenSocketIsGone() async throws {
        let socketPath = "/tmp/relay-missing-\(UUID().uuidString.prefix(8)).sock"
        let herdr = HerdrClient(socketPath: socketPath)
        let start = ContinuousClock.now
        do {
            _ = try await herdr.agents()
            Issue.record("expected a failure when the socket doesn't exist")
        } catch let e as HerdrError {
            guard case .unavailable = e else { Issue.record("expected .unavailable, got \(e)"); return }
        }
        #expect(ContinuousClock.now - start < .seconds(2))
    }

    @Test func restRouteAnswers503FastWhenHerdrDies() async throws {
        let fake = Mutex(try FakeHerdr(handler: Self.herdrHandler))
        let path = fake.withLock(\.socketPath)
        let service = AgentService(herdr: HerdrClient(socketPath: path), locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
        let router = RelayRoutes.router(service: service, hub: EventHub(), token: "t")
        let app = Application(router: router, server: .http1WebSocketUpgrade(webSocketRouter: router))
        try await app.test(.live) { client in
            try await client.execute(uri: "/agents", method: .get, headers: [.authorization: "Bearer t"]) { r throws in
                #expect(r.status == .ok)
            }
            fake.withLock { $0.stop() }
            let start = ContinuousClock.now
            try await client.execute(uri: "/agents", method: .get, headers: [.authorization: "Bearer t"]) { r throws in
                #expect(r.status == .serviceUnavailable)
                let body = try JSONDecoder().decode(ErrorBody.self, from: Data(buffer: r.body))
                #expect(body.error.code == "herdr_unavailable")
            }
            #expect(ContinuousClock.now - start < .seconds(3))

            // herdr comes back on the same socket path: the next call succeeds again.
            let restarted = try FakeHerdr(reusing: path, handler: Self.herdrHandler)
            fake.withLock { $0 = restarted }
            let backUp = await poll {
                var ok = false
                try? await client.execute(uri: "/agents", method: .get, headers: [.authorization: "Bearer t"]) { r throws in
                    ok = r.status == .ok
                }
                return ok
            }
            #expect(backUp)
        }
        fake.withLock { $0.stop() }
    }

    @Test func healthReflectsHerdrReachabilityAcrossARestart() async throws {
        var fake = try FakeHerdr(handler: Self.herdrHandler)
        let path = fake.socketPath
        let service = AgentService(herdr: HerdrClient(socketPath: path), locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
        let monitor = AgentMonitor(service: service, hub: EventHub(), stream: HerdrEventStream(socketPath: path), interval: .seconds(3600))

        let reachable = await poll { await monitor.trigger(); return await monitor.isHerdrReachable }
        #expect(reachable)

        fake.stop()
        let unreachable = await poll { await monitor.trigger(); return await monitor.isHerdrReachable == false }
        #expect(unreachable)

        fake = try FakeHerdr(reusing: path, handler: Self.herdrHandler)
        let reachableAgain = await poll { await monitor.trigger(); return await monitor.isHerdrReachable }
        #expect(reachableAgain)
        fake.stop()
    }

    @Test func eventStreamReconnectsAfterHerdrRestarts() async throws {
        var fake = try FakeHerdr(handler: Self.herdrHandler)
        let path = fake.socketPath
        let stream = HerdrEventStream(socketPath: path)
        stream.start()

        let first = await nextEvent(stream, timeout: .seconds(3))
        #expect(first?.kind == "resync")

        fake.stop()
        try await Task.sleep(for: .milliseconds(200))
        fake = try FakeHerdr(reusing: path, handler: Self.herdrHandler)

        let second = await nextEvent(stream, timeout: .seconds(8))
        #expect(second?.kind == "resync")

        stream.stop()
        fake.stop()
    }
}

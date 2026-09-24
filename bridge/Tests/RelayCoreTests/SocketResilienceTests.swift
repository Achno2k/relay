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

/// Simulates herdr disappearing and coming back (README: "Kill and restart herdr's server during
/// a test to prove it"). Uses `FakeHerdr` rather than the user's real herdr, as instructed.
@Suite struct SocketResilienceTests {
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
            try await client.execute(uri: "/agents", method: .get, headers: [.authorization: "Bearer t"]) { r throws in
                #expect(r.status == .ok)
            }
        }
        fake.withLock { $0.stop() }
    }

    @Test func healthReflectsHerdrReachabilityAcrossARestart() async throws {
        var fake = try FakeHerdr(handler: Self.herdrHandler)
        let path = fake.socketPath
        let service = AgentService(herdr: HerdrClient(socketPath: path), locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
        let monitor = AgentMonitor(service: service, hub: EventHub(), stream: HerdrEventStream(socketPath: path), interval: .seconds(3600))

        await monitor.trigger()
        #expect(await monitor.isHerdrReachable)

        fake.stop()
        await monitor.trigger()
        #expect(await monitor.isHerdrReachable == false)

        fake = try FakeHerdr(reusing: path, handler: Self.herdrHandler)
        await monitor.trigger()
        #expect(await monitor.isHerdrReachable)
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

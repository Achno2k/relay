import Foundation
import Hummingbird
import HummingbirdTesting
import Synchronization
import Testing
@testable import RelayCore

@Suite struct UsageParsersTests {
    @Test func parsesCodexRateLimits() throws {
        let data = try Fixture.data("usage-codex-ratelimits.json")
        let now = Date()
        let provider = try #require(UsageParsers.parseCodex(data, now: now))
        #expect(provider.id == "codex")
        #expect(provider.label == "ChatGPT")
        #expect(provider.plan == "Plus")
        #expect(provider.stale == false)
        #expect(provider.usedBy == ["codex"])
        #expect(provider.windows.count == 2)
        let primary = try #require(provider.windows.first { $0.id == "primary" })
        #expect(primary.usedPercent == 39)
        #expect(primary.windowMinutes == 300)
        #expect(primary.label == "5-hour")
        let secondary = try #require(provider.windows.first { $0.id == "secondary" })
        #expect(secondary.usedPercent == 18)
        #expect(secondary.label == "Weekly")
    }

    @Test func parsesCodexWithPiJoined() throws {
        let data = try Fixture.data("usage-codex-ratelimits.json")
        let provider = try #require(UsageParsers.parseCodex(data, now: Date(), piReady: true))
        #expect(provider.usedBy == ["codex", "pi"])
    }

    @Test func codexMissingResultIsNil() {
        #expect(UsageParsers.parseCodex(Data("{}".utf8), now: Date()) == nil)
        #expect(UsageParsers.parseCodex(Data("not json".utf8), now: Date()) == nil)
    }

    @Test func parsesClaudeUsageReport() throws {
        let usage = try Fixture.data("usage-claude-stream.jsonl")
        let auth = try Fixture.data("usage-claude-auth-status.json")
        let now = Date()
        let provider = try #require(UsageParsers.parseClaude(usage: usage, auth: auth, now: now))
        #expect(provider.id == "claude")
        #expect(provider.label == "Claude")
        #expect(provider.plan == "Max")
        #expect(provider.usedBy == ["claude"])
        #expect(provider.windows.count == 2)
        let session = try #require(provider.windows.first { $0.id == "session" })
        #expect(session.usedPercent == 11)
        #expect(session.resetsAt == "2026-09-24T23:00:00+00:00")
        let week = try #require(provider.windows.first { $0.id == "weekly_all" })
        #expect(week.usedPercent == 11)
        // weekly_scoped (per-model, e.g. Fable) is intentionally left out of v1.
        #expect(provider.windows.contains { $0.id == "weekly_scoped" } == false)
    }

    @Test func claudeWorksWithoutAuthStatus() throws {
        let usage = try Fixture.data("usage-claude-stream.jsonl")
        let provider = try #require(UsageParsers.parseClaude(usage: usage, auth: nil, now: Date()))
        #expect(provider.plan == nil)
        #expect(provider.windows.count == 2)
    }

    @Test func claudeNilUsageIsNil() {
        #expect(UsageParsers.parseClaude(usage: nil, auth: nil, now: Date()) == nil)
    }

    @Test func claudeNoUsageReportLineIsNil() {
        let lines = "{\"type\":\"system\"}\n{\"type\":\"result\"}\n"
        #expect(UsageParsers.parseClaude(usage: Data(lines.utf8), auth: nil, now: Date()) == nil)
    }

    @Test func parsesClaudeWithPiJoined() throws {
        let usage = try Fixture.data("usage-claude-stream.jsonl")
        let provider = try #require(UsageParsers.parseClaude(usage: usage, auth: nil, now: Date(), piReady: true))
        #expect(provider.usedBy == ["claude", "pi"])
    }

    @Test func openCodeGoProviderNilWhenPiNotReady() {
        #expect(UsageParsers.openCodeGoProvider(piReady: false, now: Date()) == nil)
    }

    @Test func openCodeGoProviderShapeWhenPiReady() throws {
        let provider = try #require(UsageParsers.openCodeGoProvider(piReady: true, now: Date()))
        #expect(provider.id == "opencode-go")
        #expect(provider.label == "OpenCode Go")
        #expect(provider.plan == "OpenCode Go")
        #expect(provider.windows.isEmpty)
        #expect(provider.stale == false)
        #expect(provider.unavailableReason == "Usage not available from OpenCode")
        #expect(provider.usedBy == ["pi"])
    }
}

@Suite struct UsageMonitorTests {
    static func codexProbe(_ data: Data?) -> CodexUsageProbe { CodexUsageProbe(run: { data }) }
    static func claudeProbe(_ usage: Data?, _ auth: Data? = nil) -> ClaudeUsageProbe {
        ClaudeUsageProbe(runUsage: { _ in usage }, runAuthStatus: { auth })
    }
    /// `ready` names which providers `pi auth check` should report `status: "ready"` for; everything
    /// else (including a probe failure) reports not-ready. Defaults to none, matching "pi not installed".
    static func piProbe(ready: Set<String> = []) -> PiAuthProbe {
        PiAuthProbe(run: { provider in
            ready.contains(provider) ? Data(#"{"status":"ready"}"#.utf8) : Data(#"{"status":"invalid"}"#.utf8)
        })
    }

    @Test func seedsCacheAtStartupWithNoSubscribers() async throws {
        let hub = EventHub()
        let codexData = try Fixture.data("usage-codex-ratelimits.json")
        let claudeData = try Fixture.data("usage-claude-stream.jsonl")
        let monitor = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(codexData), claudeProbe: Self.claudeProbe(claudeData),
                                   piProbe: Self.piProbe())
        let task = Task { try await monitor.run() }
        try await Task.sleep(for: .milliseconds(200))
        let snap = await monitor.snapshot()
        #expect(snap.count == 2)
        #expect(snap.contains { $0.id == "codex" && !$0.windows.isEmpty })
        #expect(snap.contains { $0.id == "claude" && !$0.windows.isEmpty })
        task.cancel()
    }

    @Test func unavailableProbeMarksStaleWithReason() async throws {
        let hub = EventHub()
        let monitor = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(nil), claudeProbe: Self.claudeProbe(nil), piProbe: Self.piProbe())
        try await monitor.requestRefreshAndWait()
        let snap = await monitor.snapshot()
        let codex = try #require(snap.first { $0.id == "codex" })
        #expect(codex.stale == true)
        #expect(codex.unavailableReason != nil)
        #expect(codex.windows.isEmpty)
        #expect(codex.usedBy == ["codex"])
    }

    @Test func manualRefreshIsThrottled() async throws {
        let hub = EventHub()
        let monitor = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(nil), claudeProbe: Self.claudeProbe(nil),
                                   piProbe: Self.piProbe(), manualCooldown: 15)
        #expect(await monitor.requestRefresh() == true)
        #expect(await monitor.requestRefresh() == false)
    }

    @Test func staleAfterWindowMarksCachedDataStale() async throws {
        let hub = EventHub()
        let codexData = try Fixture.data("usage-codex-ratelimits.json")
        let clock = Mutex(Date(timeIntervalSince1970: 1_000_000))
        let monitor = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(codexData), claudeProbe: Self.claudeProbe(nil),
                                   piProbe: Self.piProbe(), staleAfter: 60, now: { clock.withLock { $0 } })
        try await monitor.requestRefreshAndWait()
        let fresh = try #require(await monitor.snapshot().first { $0.id == "codex" })
        #expect(fresh.stale == false)
        clock.withLock { $0 = $0.addingTimeInterval(120) }
        let stale = try #require(await monitor.snapshot().first { $0.id == "codex" })
        #expect(stale.stale == true)
    }

    @Test func onlyPollsWhileAClientIsConnected() async throws {
        let hub = EventHub()
        let box = Box(0)
        let probe = CodexUsageProbe(run: { box.count += 1; return nil })
        let monitor = UsageMonitor(hub: hub, codexProbe: probe, claudeProbe: Self.claudeProbe(nil), piProbe: Self.piProbe(),
                                   tick: .milliseconds(50), baseInterval: 0.01, maxInterval: 0.05)
        let task = Task { try await monitor.run() }
        try await Task.sleep(for: .milliseconds(120))
        // Startup seed = 1 call; the tick loop shouldn't add more with nobody subscribed.
        let afterIdle = box.count
        #expect(afterIdle >= 1)
        let (id, _) = hub.subscribe()
        try await Task.sleep(for: .milliseconds(150))
        #expect(box.count > afterIdle)
        hub.unsubscribe(id)
        task.cancel()
    }

    @Test func piJoinsUsedByWhenReady() async throws {
        let hub = EventHub()
        let codexData = try Fixture.data("usage-codex-ratelimits.json")
        let claudeData = try Fixture.data("usage-claude-stream.jsonl")
        let monitor = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(codexData), claudeProbe: Self.claudeProbe(claudeData),
                                   piProbe: Self.piProbe(ready: ["openai-codex", "anthropic"]))
        try await monitor.requestRefreshAndWait()
        let snap = await monitor.snapshot()
        #expect(snap.first { $0.id == "codex" }?.usedBy == ["codex", "pi"])
        #expect(snap.first { $0.id == "claude" }?.usedBy == ["claude", "pi"])
    }

    @Test func openCodeGoCardAppearsOnlyWhenPiIsReady() async throws {
        let hub = EventHub()
        let notReady = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(nil), claudeProbe: Self.claudeProbe(nil), piProbe: Self.piProbe())
        try await notReady.requestRefreshAndWait()
        #expect(await notReady.snapshot().contains { $0.id == "opencode-go" } == false)

        let ready = UsageMonitor(hub: hub, codexProbe: Self.codexProbe(nil), claudeProbe: Self.claudeProbe(nil),
                                 piProbe: Self.piProbe(ready: ["opencode-go"]))
        try await ready.requestRefreshAndWait()
        let card = try #require(await ready.snapshot().first { $0.id == "opencode-go" })
        #expect(card.label == "OpenCode Go")
        #expect(card.stale == false)
        #expect(card.unavailableReason == "Usage not available from OpenCode")
        #expect(card.usedBy == ["pi"])
    }
}

@Suite struct UsageRoutesTests {
    static let token = "test-token"
    static let auth: HTTPFields = [.authorization: "Bearer \(Self.token)"]

    func withApp(usage: UsageMonitor?, _ body: @escaping @Sendable (any TestClientProtocol) async throws -> Void) async throws {
        let fake = try FakeHerdr { method, _ in FakeError(code: "unused", message: method) }
        defer { fake.stop() }
        let projects = URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-usage-routes-\(UUID().uuidString.prefix(8))")
        defer { try? FileManager.default.removeItem(at: projects) }
        let service = AgentService(herdr: HerdrClient(socketPath: fake.socketPath), locator: TranscriptLocator(claudeProjects: projects))
        let hub = EventHub()
        let router = RelayRoutes.router(service: service, hub: hub, token: Self.token, usage: usage)
        let app = Application(router: router, server: .http1WebSocketUpgrade(webSocketRouter: router))
        try await app.test(.live) { client in try await body(client) }
    }

    @Test func getUsageServesTheCacheWithoutFetching() async throws {
        let hub = EventHub()
        let codexData = try Fixture.data("usage-codex-ratelimits.json")
        let monitor = UsageMonitor(hub: hub, codexProbe: UsageMonitorTests.codexProbe(codexData), claudeProbe: UsageMonitorTests.claudeProbe(nil),
                                   piProbe: UsageMonitorTests.piProbe())
        try await monitor.requestRefreshAndWait()
        try await withApp(usage: monitor) { client in
            try await client.execute(uri: "/usage", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                let snap = try JSONDecoder().decode(UsageSnapshot.self, from: Data(buffer: r.body))
                #expect(snap.providers.count == 2)
                #expect(snap.providers.first { $0.id == "codex" }?.windows.isEmpty == false)
            }
        }
    }

    @Test func getUsageWithNoMonitorReturnsEmptyProviders() async throws {
        try await withApp(usage: nil) { client in
            try await client.execute(uri: "/usage", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                let snap = try JSONDecoder().decode(UsageSnapshot.self, from: Data(buffer: r.body))
                #expect(snap.providers.isEmpty)
            }
        }
    }

    @Test func refreshIsThrottledAfterFirstCall() async throws {
        let hub = EventHub()
        let monitor = UsageMonitor(hub: hub, codexProbe: UsageMonitorTests.codexProbe(nil), claudeProbe: UsageMonitorTests.claudeProbe(nil),
                                   piProbe: UsageMonitorTests.piProbe())
        try await withApp(usage: monitor) { client in
            try await client.execute(uri: "/usage/refresh", method: .post, headers: Self.auth) { r throws in
                #expect(r.status == .accepted)
            }
            try await client.execute(uri: "/usage/refresh", method: .post, headers: Self.auth) { r throws in
                #expect(r.status == .tooManyRequests)
                let err = try JSONDecoder().decode(ErrorBody.self, from: Data(buffer: r.body))
                #expect(err.error.code == "rate_limited")
            }
        }
    }

    @Test func usageNeedsAuth() async throws {
        try await withApp(usage: nil) { client in
            try await client.execute(uri: "/usage", method: .get) { r throws in
                #expect(r.status == .unauthorized)
            }
        }
    }
}

private final class Box: @unchecked Sendable {
    var count: Int
    init(_ count: Int) { self.count = count }
}

private extension UsageMonitor {
    /// Forces an immediate poll and gives it a moment to land, bypassing the cooldown for tests that
    /// don't care about throttling.
    func requestRefreshAndWait() async throws {
        _ = requestRefresh()
        try await Task.sleep(for: .milliseconds(150))
    }
}

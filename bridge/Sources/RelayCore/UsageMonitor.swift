import Foundation
import ServiceLifecycle

/// Polls the Claude and Codex/pi usage probes, caches the result, and broadcasts `usage.updated`.
/// See api.md "Usage" for the gating/backoff rules this implements.
public actor UsageMonitor: Service {
    private var providers: [String: UsageProvider] = [:]
    private var nextEligiblePoll: [String: Date] = [:]
    private var backoff: [String: TimeInterval] = [:]
    private var lastManualRefresh: Date?

    let hub: EventHub
    let codexProbe: CodexUsageProbe
    let claudeProbe: ClaudeUsageProbe
    let piProbe: PiAuthProbe
    let tick: Duration
    let baseInterval: TimeInterval
    let maxInterval: TimeInterval
    let staleAfter: TimeInterval
    let manualCooldown: TimeInterval
    let now: @Sendable () -> Date

    public init(hub: EventHub, codexProbe: CodexUsageProbe = CodexUsageProbe(), claudeProbe: ClaudeUsageProbe = ClaudeUsageProbe(),
                piProbe: PiAuthProbe = PiAuthProbe(),
                tick: Duration = .seconds(15), baseInterval: TimeInterval = 60, maxInterval: TimeInterval = 600,
                staleAfter: TimeInterval = 900, manualCooldown: TimeInterval = 15, now: @escaping @Sendable () -> Date = Date.init) {
        self.hub = hub
        self.codexProbe = codexProbe
        self.claudeProbe = claudeProbe
        self.piProbe = piProbe
        self.tick = tick
        self.baseInterval = baseInterval
        self.maxInterval = maxInterval
        self.staleAfter = staleAfter
        self.manualCooldown = manualCooldown
        self.now = now
    }

    public func run() async throws {
        await pollAll(force: true)  // seed the cache so GET /usage never has to wait on a subscriber
        while !Task.isCancelled {
            try? await Task.sleep(for: tick)
            if hub.count > 0 { await pollAll(force: false) }
        }
    }

    /// `GET /usage`: the cache only, with `stale` re-derived against the current time. `opencode-go`
    /// is never marked stale (see api.md "Usage" — there's no fetch to go stale).
    public func snapshot() -> [UsageProvider] {
        let n = now()
        return ["claude", "codex", "opencode-go"].compactMap { id in
            guard let p = providers[id] else { return nil }
            return id == "opencode-go" ? p : p.markedStale(now: n, staleAfter: staleAfter)
        }
    }

    /// `POST /usage/refresh`. Returns false (throttled) within `manualCooldown` of the last refresh.
    @discardableResult
    public func requestRefresh() -> Bool {
        let n = now()
        if let last = lastManualRefresh, n.timeIntervalSince(last) < manualCooldown { return false }
        lastManualRefresh = n
        Task { await self.pollAll(force: true) }
        return true
    }

    private func pollAll(force: Bool) async {
        let piCodex = piProbe.isReady(provider: "openai-codex")
        let piClaude = piProbe.isReady(provider: "anthropic")
        let piOpenCodeGo = piProbe.isReady(provider: "opencode-go")
        await pollCodex(force: force, piReady: piCodex)
        await pollClaude(force: force, piReady: piClaude)
        pollOpenCodeGo(piReady: piOpenCodeGo)
    }

    private func pollCodex(force: Bool, piReady: Bool) async {
        guard force || isEligible("codex") else { return }
        let n = now()
        guard let raw = codexProbe.fetch(), let provider = UsageParsers.parseCodex(raw, now: n, piReady: piReady) else {
            recordFailure("codex", reason: "codex app-server didn't respond", now: n)
            return
        }
        record(provider, now: n)
    }

    private func pollClaude(force: Bool, piReady: Bool) async {
        guard force || isEligible("claude") else { return }
        let n = now()
        let (usage, auth) = claudeProbe.fetch()
        guard let provider = UsageParsers.parseClaude(usage: usage, auth: auth, now: n, piReady: piReady) else {
            recordFailure("claude", reason: "claude -p /usage didn't return usage data", now: n)
            return
        }
        record(provider, now: n)
    }

    /// No backoff/eligibility gating — this is just `pi auth check`, not a rate-limited fetch. When
    /// pi loses `opencode-go` auth, the card is dropped from the cache entirely (matches "omitted, not
    /// shown empty" in api.md).
    private func pollOpenCodeGo(piReady: Bool) {
        let n = now()
        guard let provider = UsageParsers.openCodeGoProvider(piReady: piReady, now: n) else {
            providers["opencode-go"] = nil
            return
        }
        record(provider, now: n)
    }

    private func isEligible(_ id: String) -> Bool {
        guard let next = nextEligiblePoll[id] else { return true }
        return now() >= next
    }

    private func record(_ provider: UsageProvider, now n: Date) {
        let changed = providers[provider.id].map { !$0.sameDataExcludingFreshness(as: provider) } ?? true
        let interval = changed ? baseInterval : min(maxInterval, (backoff[provider.id] ?? baseInterval) * 2)
        backoff[provider.id] = interval
        nextEligiblePoll[provider.id] = n.addingTimeInterval(interval)
        providers[provider.id] = provider
        if changed { hub.broadcast(.usageUpdated(provider)) }
    }

    private func recordFailure(_ id: String, reason: String, now n: Date) {
        let interval = min(maxInterval, (backoff[id] ?? baseInterval) * 2)
        backoff[id] = interval
        nextEligiblePoll[id] = n.addingTimeInterval(interval)
        let label = id == "claude" ? "Claude" : "ChatGPT"
        let source = id == "claude" ? "claude -p /usage" : "codex app-server"
        // Keep the last known windows/usedBy (dimmed via `stale`) rather than blanking them on one failure.
        let previousWindows = providers[id]?.windows ?? []
        let previousUsedBy = providers[id]?.usedBy ?? [id]
        let provider = UsageProvider(id: id, label: label, plan: providers[id]?.plan, windows: previousWindows,
                                     updatedAt: providers[id]?.updatedAt ?? Timestamps.format(n),
                                     source: source, stale: true, unavailableReason: reason, usedBy: previousUsedBy)
        let changed = providers[id]?.unavailableReason != reason
        providers[id] = provider
        if changed { hub.broadcast(.usageUpdated(provider)) }
    }
}

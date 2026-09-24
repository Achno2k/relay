import Foundation

/// See api.md "Usage".
public struct UsageWindow: Codable, Sendable, Equatable {
    public var id: String
    public var label: String
    public var usedPercent: Double?
    public var windowMinutes: Int?
    public var resetsAt: String?

    public init(id: String, label: String, usedPercent: Double?, windowMinutes: Int?, resetsAt: String?) {
        self.id = id
        self.label = label
        self.usedPercent = usedPercent
        self.windowMinutes = windowMinutes
        self.resetsAt = resetsAt
    }
}

public struct UsageProvider: Codable, Sendable, Equatable {
    public var id: String
    public var label: String
    public var plan: String?
    public var windows: [UsageWindow]
    public var updatedAt: String
    public var source: String
    public var stale: Bool
    public var unavailableReason: String?
    /// Which herdr-driven harnesses (`"claude"`, `"codex"`, `"pi"`) are authenticated against this
    /// subscription on this Mac, per `pi auth check`. Never guessed. See api.md "Usage".
    public var usedBy: [String]

    public init(id: String, label: String, plan: String?, windows: [UsageWindow], updatedAt: String,
                source: String, stale: Bool, unavailableReason: String? = nil, usedBy: [String] = []) {
        self.id = id
        self.label = label
        self.plan = plan
        self.windows = windows
        self.updatedAt = updatedAt
        self.source = source
        self.stale = stale
        self.unavailableReason = unavailableReason
        self.usedBy = usedBy
    }

    /// True after `updatedAt` for `stale`, without touching the fields a comparison should ignore.
    public func markedStale(now: Date, staleAfter: TimeInterval) -> UsageProvider {
        var p = self
        guard let at = Timestamps.parse(updatedAt) else { p.stale = true; return p }
        if now.timeIntervalSince(at) > staleAfter { p.stale = true }
        return p
    }

    /// Same provider, ignoring `updatedAt`/`stale` — used to decide whether to broadcast/reset backoff.
    public func sameDataExcludingFreshness(as other: UsageProvider) -> Bool {
        var a = self, b = other
        a.updatedAt = ""; b.updatedAt = ""
        a.stale = false; b.stale = false
        return a == b
    }
}

public struct UsageSnapshot: Codable, Sendable, Equatable {
    public var providers: [UsageProvider]

    public init(providers: [UsageProvider]) {
        self.providers = providers
    }
}

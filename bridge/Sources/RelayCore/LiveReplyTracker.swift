import Foundation

/// Throttles and dedupes the `reply.live` preview per agent. Pure logic, no I/O, so it's cheap to test.
public actor LiveReplyTracker {
    private struct State {
        var seq = 0
        /// The text last actually broadcast; `nil` means the client currently sees no preview.
        var lastSent: String?
        var lastSentAt = Date.distantPast
        /// The current assistant reply as the transcript already has it, so the preview never repeats it.
        var landedText = ""
    }

    private var state: [String: State] = [:]
    private let minInterval: TimeInterval
    private let clock: @Sendable () -> Date

    public init(minInterval: TimeInterval = 0.25, clock: @escaping @Sendable () -> Date = Date.init) {
        self.minInterval = minInterval
        self.clock = clock
    }

    /// A freshly parsed screen text for `agentId`. Returns the frame to broadcast, if throttling and
    /// dedupe against the landed transcript text allow one right now.
    public func offer(agentId: String, text: String?) -> ServerEvent? {
        var s = state[agentId] ?? State()
        defer { state[agentId] = s }

        let candidate: String? = text.flatMap { t in
            guard !t.isEmpty else { return nil }
            guard s.landedText.isEmpty || !(s.landedText == t || s.landedText.hasPrefix(t)) else { return nil }
            return t
        }
        guard candidate != s.lastSent else { return nil }

        let now = clock()
        if candidate != nil, now.timeIntervalSince(s.lastSentAt) < minInterval { return nil }

        s.seq += 1
        s.lastSent = candidate
        s.lastSentAt = now
        return .replyLive(agentId: agentId, text: candidate, seq: s.seq)
    }

    /// The transcript's assistant message grew (or landed) with `text`. Remembers it for dedupe and
    /// clears any live preview right away, bypassing the throttle.
    @discardableResult
    public func landed(agentId: String, text: String) -> ServerEvent? {
        var s = state[agentId] ?? State()
        s.landedText = text
        defer { state[agentId] = s }
        guard s.lastSent != nil else { return nil }
        s.seq += 1
        s.lastSent = nil
        s.lastSentAt = clock()
        return .replyLive(agentId: agentId, text: nil, seq: s.seq)
    }

    /// The agent stopped working (or closed): clear any live preview right away.
    @discardableResult
    public func stopped(agentId: String) -> ServerEvent? {
        var s = state[agentId] ?? State()
        defer { state[agentId] = s }
        guard s.lastSent != nil else { return nil }
        s.seq += 1
        s.lastSent = nil
        s.lastSentAt = clock()
        return .replyLive(agentId: agentId, text: nil, seq: s.seq)
    }

    public func remove(agentId: String) {
        state.removeValue(forKey: agentId)
    }
}

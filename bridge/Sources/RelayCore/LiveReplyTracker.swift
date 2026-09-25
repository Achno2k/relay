import Foundation

/// The `tool` part of a `reply.live` frame; see api.md "Live reply".
public struct LiveTool: Sendable, Equatable, Encodable {
    public var name: String
    public var summary: String
    public var state: String

    public init(name: String, summary: String, state: String = "running") {
        self.name = name
        self.summary = summary
        self.state = state
    }
}

/// Turns raw per-poll screen reads into a stable `reply.live` stream per agent. Pure logic, no I/O,
/// so it's cheap to test.
///
/// Screen reads flicker (claude blinks a running tool's `⏺`, spinners animate, a read can catch a
/// half-drawn frame), so nothing here trusts a single read:
/// - text only grows; a different text needs two reads in a row that agree, and a text that was
///   replaced or landed in the transcript is never sent again this turn;
/// - the tool appears, changes or clears only after two reads in a row agree, except a generic
///   summary being refined;
/// - text and tool clear at once when the transcript has them, and both clear when the agent stops.
public actor LiveReplyTracker {
    private struct State {
        var seq = 0
        var lastSentAt = Date.distantPast
        /// What the client currently shows.
        var text: String?
        var tool: LiveTool?
        var toolGeneric = false

        // Per turn, reset when the agent stops.
        /// Folded (see `fold`) text blocks the transcript already has.
        var landedTexts: Set<String> = []
        /// Folded texts that were shown and then replaced or cleared; never shown again.
        var retiredTexts: [String] = []
        var landedToolIds: Set<String> = []
        var landedSummaries: Set<String> = []
        /// Specific summaries already cleared this turn, so a re-read of the same block stays cleared.
        var clearedSummaries: Set<String> = []
        /// `landedToolIds.count` when the tool block with this summary first showed up.
        var toolSince: [String: Int] = [:]
        /// The previous read's tool, before any suppression.
        var lastRawTool: LiveTool?
        var lastRawGeneric = false
        /// A change seen once, waiting for a second read that agrees.
        var pendingText: String?
        var pendingTool: LiveTool??
    }

    private var state: [String: State] = [:]
    private let minInterval: TimeInterval
    private let clock: @Sendable () -> Date

    public init(minInterval: TimeInterval = 0.25, clock: @escaping @Sendable () -> Date = Date.init) {
        self.minInterval = minInterval
        self.clock = clock
    }

    /// A fresh screen read for `agentId`: its prose and its running tool (already scrubbed).
    /// `toolGeneric` means the screen didn't show the tool's arguments, so `tool.summary` is the
    /// generic one. Returns the frame to broadcast, if anything the client sees should change now.
    public func offer(agentId: String, text: String?, tool: LiveTool? = nil, toolGeneric: Bool = false) -> ServerEvent? {
        var s = state[agentId] ?? State()
        defer { state[agentId] = s }

        let candidateText = filterText(text, s)
        let candidateTool = filterTool(tool, generic: toolGeneric, &s)

        // Text: grows at once, a shorter or empty read keeps it, anything else waits for a second read.
        var newText = s.text
        var pendingText: String?
        if let t = candidateText {
            if let cur = s.text {
                let (ft, fc) = (Self.fold(t), Self.fold(cur))
                if t.hasPrefix(cur) || (ft.hasPrefix(fc) && ft.count > fc.count) {
                    newText = t
                } else if let merged = Self.extend(cur, with: t) {
                    newText = merged  // the reply scrolled: the read is a later window of it
                } else if fc.hasPrefix(ft) || (ft.count >= 12 && fc.contains(ft)) {
                    // A shorter, re-rendered or scrolled read of the same text: keep what's shown.
                } else if let p = s.pendingText, ft.hasPrefix(Self.fold(p)) || Self.extend(p, with: t) != nil {
                    newText = Self.extend(p, with: t) ?? t
                } else {
                    pendingText = t
                }
            } else {
                newText = t
            }
        }

        // Tool: a generic summary refines at once, anything else waits for a second read.
        var newTool = s.tool
        var newToolGeneric = s.toolGeneric
        var pendingTool: LiveTool??
        if candidateTool != s.tool {
            if let cur = s.tool, let next = candidateTool, s.toolGeneric, cur.name == next.name {
                newTool = next
                newToolGeneric = toolGeneric
            } else if let p = s.pendingTool, p == candidateTool {
                newTool = candidateTool
                newToolGeneric = candidateTool != nil && toolGeneric
            } else {
                pendingTool = .some(candidateTool)
            }
        }

        guard newText != s.text || newTool != s.tool else {
            s.pendingText = pendingText
            s.pendingTool = pendingTool
            return nil
        }
        let now = clock()
        if now.timeIntervalSince(s.lastSentAt) < minInterval {
            // Throttled: keep the accepted change pending so the next read confirms it again.
            s.pendingText = newText != s.text ? newText : pendingText
            s.pendingTool = newTool != s.tool ? .some(newTool) : pendingTool
            return nil
        }
        s.pendingText = pendingText
        s.pendingTool = pendingTool
        return send(&s, agentId: agentId, text: newText, tool: newTool, toolGeneric: newToolGeneric, at: now)
    }

    /// The transcript's assistant message grew (or landed) with these text blocks and tool calls.
    /// Remembers them so the preview never repeats them, and clears whichever part the client is
    /// showing that the transcript now has, right away, bypassing the throttle.
    @discardableResult
    public func landed(agentId: String, texts: [String], toolCalls: [(id: String, summary: String)]) -> ServerEvent? {
        var s = state[agentId] ?? State()
        defer { state[agentId] = s }
        for t in texts {
            let f = Self.fold(t)
            if !f.isEmpty { s.landedTexts.insert(f) }
        }
        for call in toolCalls {
            s.landedToolIds.insert(call.id)
            s.landedSummaries.insert(call.summary)
        }

        var text = s.text
        var tool = s.tool
        if let cur = text, Self.matchesLanded(Self.fold(cur), s.landedTexts) {
            text = nil
        }
        if let cur = tool, s.landedSummaries.contains(cur.summary) || s.landedToolIds.count > (s.toolSince[cur.summary] ?? s.landedToolIds.count) {
            if !s.toolGeneric { s.clearedSummaries.insert(cur.summary) }
            tool = nil
        }
        guard text != s.text || tool != s.tool else { return nil }
        return send(&s, agentId: agentId, text: text, tool: tool, toolGeneric: tool == nil ? false : s.toolGeneric, at: clock())
    }

    /// Convenience for a message's blocks.
    @discardableResult
    public func landed(agentId: String, blocks: [Block]) -> ServerEvent? {
        var texts: [String] = []
        var calls: [(id: String, summary: String)] = []
        for b in blocks {
            switch b {
            case .text(let t): texts.append(t)
            case .toolCall(let id, _, let summary, _): calls.append((id, summary))
            default: break
            }
        }
        return landed(agentId: agentId, texts: texts, toolCalls: calls)
    }

    /// The agent stopped working (or closed): clear the preview right away and forget the turn.
    @discardableResult
    public func stopped(agentId: String) -> ServerEvent? {
        let old = state[agentId] ?? State()
        var s = State()
        s.seq = old.seq
        s.lastSentAt = old.lastSentAt
        s.text = old.text
        s.tool = old.tool
        defer { state[agentId] = s }
        guard s.text != nil || s.tool != nil else { return nil }
        let ev = send(&s, agentId: agentId, text: nil, tool: nil, toolGeneric: false, at: clock())
        s.retiredTexts = []
        return ev
    }

    public func remove(agentId: String) {
        state.removeValue(forKey: agentId)
    }

    // MARK: -

    private func send(_ s: inout State, agentId: String, text: String?, tool: LiveTool?, toolGeneric: Bool, at now: Date) -> ServerEvent {
        if let old = s.text, text != old {
            let (fo, fn) = (Self.fold(old), Self.fold(text ?? ""))
            if text == nil || !fn.hasPrefix(fo) { s.retiredTexts.append(fo) }
        }
        s.text = text
        s.tool = tool
        s.toolGeneric = toolGeneric
        s.seq += 1
        s.lastSentAt = now
        return .replyLive(agentId: agentId, text: text, tool: tool, seq: s.seq)
    }

    /// `nil` for empty text, text the transcript already has, or text shown before and retired.
    private func filterText(_ text: String?, _ s: State) -> String? {
        guard let text, !text.isEmpty else { return nil }
        let f = Self.fold(text)
        if f.isEmpty { return nil }
        if Self.matchesLanded(f, s.landedTexts) { return nil }
        if s.retiredTexts.contains(where: { $0 == f || $0.hasPrefix(f) || (f.count >= 12 && $0.contains(f)) }) { return nil }
        return text
    }

    /// `nil` once the transcript has this tool call: same summary, or any tool call that landed after
    /// this tool block showed up. Tracks which block each read belongs to.
    private func filterTool(_ tool: LiveTool?, generic: Bool, _ s: inout State) -> LiveTool? {
        defer {
            s.lastRawTool = tool
            s.lastRawGeneric = generic
        }
        guard let tool else { return nil }
        let since: Int
        if let prev = s.lastRawTool, prev.summary == tool.summary || (s.lastRawGeneric && prev.name == tool.name),
           let known = s.toolSince[prev.summary] {
            since = known  // the same block as the previous read (possibly refined)
        } else if !generic, let known = s.toolSince[tool.summary] {
            since = known  // a specific block seen earlier this turn, back after a glitchy read
        } else {
            since = s.landedToolIds.count
        }
        s.toolSince[tool.summary] = since
        if s.landedSummaries.contains(tool.summary) || s.clearedSummaries.contains(tool.summary) || s.landedToolIds.count > since {
            if !generic { s.clearedSummaries.insert(tool.summary) }
            return nil
        }
        return tool
    }

    /// `cur` plus what `t` adds after it, when `t` is a later window of the same text: `t` starts
    /// somewhere inside `cur` and carries on past its end (the top of a long reply scrolled off the
    /// viewport as it grew).
    static func extend(_ cur: String, with t: String) -> String? {
        let head = String(t.prefix(24))
        guard head.count >= 12 else { return nil }
        var searchEnd = cur.endIndex
        while let r = cur.range(of: head, options: .backwards, range: cur.startIndex..<searchEnd) {
            let rest = cur[r.lowerBound...]
            if r.lowerBound > cur.startIndex, t.hasPrefix(rest), t.count > rest.count {
                return cur[..<r.lowerBound] + t
            }
            searchEnd = r.upperBound == cur.startIndex ? cur.startIndex : cur.index(before: r.upperBound)
            if searchEnd <= cur.startIndex { break }
        }
        return nil
    }

    /// Lowercased letters and digits only, so a rendered screen (no `**`, backticks, rewrapped lines,
    /// curly quotes) compares equal to the transcript's markdown.
    static func fold(_ s: String) -> String {
        String(String.UnicodeScalarView(s.lowercased().unicodeScalars.filter { CharacterSet.alphanumerics.contains($0) }))
    }

    /// The screen text is (part of) a landed block: equal, inside it (the visible tail of a scrolled
    /// reply), or sharing a long prefix with it (markdown the fold couldn't line up).
    static func matchesLanded(_ f: String, _ landed: Set<String>) -> Bool {
        landed.contains { l in
            l == f || (f.count >= 12 && l.contains(f)) || l.hasPrefix(f) || commonPrefix(l, f) >= 40
        }
    }

    private static func commonPrefix(_ a: String, _ b: String) -> Int {
        zip(a, b).prefix { $0 == $1 }.count
    }
}

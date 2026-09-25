import Foundation
import RelayKit

/// What's live for one agent right now: the on-screen preview of its reply and the tool it's running.
struct LiveReply: Equatable {
    var text: String?
    var tool: LiveToolRun?
}

/// A `reply.live` tool from the moment it shows up on screen until the transcript's toolCall for it
/// lands. The chat shows it as a "Running Bash…" row under `placeholderId`; once matched, the real
/// row takes that same id, so it swaps in place.
struct LiveToolRun: Equatable {
    var tool: LiveTool
    var placeholderId: String
    /// Tool calls already in this turn when the run started. Only exact name + summary matches can
    /// claim one of these (the transcript can beat the screen); the name-only fallback can't.
    var knownCallIds: Set<String>
    /// The transcript call it turned into. Set once; the placeholder is hidden from then on.
    var matchedCallId: String?
}

/// `reply.live` state for every agent, as a plain value so the lifecycle is testable without a view.
/// `seq` ordering stays with the caller (it changes on every frame and must not count as a change).
struct LiveReplies: Equatable {
    private(set) var byAgent: [String: LiveReply] = [:]
    /// Per agent: transcript tool-call id → the placeholder id it replaced.
    private(set) var aliases: [String: [String: String]] = [:]
    private var runs = 0

    subscript(agentId: String) -> LiveReply? { byAgent[agentId] }

    /// One `reply.live` frame. `transcript` is the agent's transcript (no pending prompts).
    mutating func apply(agentId: String, text: String?, tool: LiveTool?, transcript: [Message]) {
        var reply = byAgent[agentId] ?? LiveReply()
        let prose = Self.prose(text)
        // A redraw can briefly show less of the same reply; never step back to a shorter prefix.
        if let old = reply.text, let prose, old.hasPrefix(prose) {
            reply.text = old
        } else {
            reply.text = prose
        }
        if let tool {
            // Same call: repeated frames, or a generic summary ("Ran a command") refined once the
            // screen shows the arguments. Only a landed call with a new summary is a new call.
            if var run = reply.tool, run.tool.name == tool.name,
               run.matchedCallId == nil || Self.same(run.tool.summary, tool.summary) {
                run.tool = tool
                reply.tool = run
            } else {
                runs += 1
                let calls = Self.turnCalls(transcript)
                reply.tool = LiveToolRun(
                    tool: tool,
                    placeholderId: "live-tool-\(runs)",
                    knownCallIds: Set(calls.map(\.call.id))
                )
            }
        } else {
            reply.tool = nil
        }
        byAgent[agentId] = reply.text == nil && reply.tool == nil ? nil : reply
        match(agentId: agentId, transcript: transcript)
    }

    /// The transcript grew; a running tool may have landed.
    mutating func match(agentId: String, transcript: [Message]) {
        guard var run = byAgent[agentId]?.tool, run.matchedCallId == nil else { return }
        let claimed = aliases[agentId] ?? [:]
        let calls = Self.turnCalls(transcript).filter { claimed[$0.call.id] == nil }
        let exact = calls.last { c in
            c.call.name == run.tool.name && Self.same(c.call.summary, run.tool.summary)
                && (!run.knownCallIds.contains(c.call.id) || !c.finished)
        }
        let byName = calls.last { $0.call.name == run.tool.name && !run.knownCallIds.contains($0.call.id) }
        guard let call = (exact ?? byName)?.call else { return }
        run.matchedCallId = call.id
        byAgent[agentId]?.tool = run
        aliases[agentId, default: [:]][call.id] = run.placeholderId
    }

    /// The agent stopped working (or a stop landed): nothing is live any more.
    mutating func clear(agentId: String) {
        byAgent[agentId] = nil
    }

    mutating func remove(agentId: String) {
        byAgent[agentId] = nil
        aliases[agentId] = nil
    }

    /// After a reconnect: frames may have been missed, and a restarted bridge restarts `seq`.
    mutating func clearAll() {
        byAgent = [:]
    }

    // MARK: - Helpers

    /// Tool calls in the current turn (after the last prompt), oldest first, with whether a result landed.
    static func turnCalls(_ transcript: [Message]) -> [(call: ToolCall, finished: Bool)] {
        var calls: [(call: ToolCall, finished: Bool)] = []
        var results: Set<String> = []
        for message in transcript.reversed() {
            if message.role == .user {
                if !message.plainText.isEmpty || !message.attachments.isEmpty { break }
                continue
            }
            for block in message.blocks.reversed() {
                switch block {
                case .toolCall(let call): calls.append((call, results.contains(call.id)))
                case .toolResult(let result): results.insert(result.toolCallId)
                default: continue
                }
            }
        }
        return calls.reversed()
    }

    static func same(_ a: String, _ b: String) -> Bool {
        normalized(a) == normalized(b)
    }

    private static func normalized(_ s: String) -> String {
        s.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// Live text is assistant prose only. An older bridge could send the tool block it saw on screen
    /// (`⏺ Bash(ls)`, its `⎿` output, codex's `• Ran …`); drop those lines and their indented output.
    static func prose(_ text: String?) -> String? {
        guard let text else { return nil }
        var kept: [Substring] = []
        var inTool = false
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if isToolLine(line) {
                inTool = true
                continue
            }
            if inTool {
                if line.first?.isWhitespace == true, !line.allSatisfy(\.isWhitespace) { continue }
                inTool = false
            }
            // Dropping a tool block leaves the blank lines around it; keep one paragraph break.
            if line.allSatisfy(\.isWhitespace), kept.last?.allSatisfy(\.isWhitespace) == true { continue }
            kept.append(line)
        }
        let result = kept.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        return result.isEmpty ? nil : result
    }

    private static func isToolLine(_ line: Substring) -> Bool {
        let s = line.drop(while: \.isWhitespace)
        if s.hasPrefix("⎿") || s.hasPrefix("└") { return true }
        if s.hasPrefix("⏺") {
            let rest = s.dropFirst().drop(while: \.isWhitespace)
            return rest.prefixMatch(of: /[A-Za-z_][\w.:\-]*\(/) != nil
        }
        if s.hasPrefix("•") {
            let rest = s.dropFirst().drop(while: \.isWhitespace)
            return rest.prefixMatch(of: /(Ran|Running|Explored|Edited|Called)\b/) != nil
        }
        return false
    }
}

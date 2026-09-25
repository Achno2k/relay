import Foundation
import Synchronization
import Testing
@testable import RelayCore

/// A settable clock, safe to capture in the tracker's `@Sendable` closure.
final class TestClock: Sendable {
    private let value: Mutex<Date>
    init(_ date: Date = Date()) { value = Mutex(date) }
    func advance(_ seconds: TimeInterval) { value.withLock { $0 = $0.addingTimeInterval(seconds) } }
    func now() -> Date { value.withLock { $0 } }
}

private let ping = LiveTool(name: "Bash", summary: "Ran ping -c 8 127.0.0.1")
private let genericBash = LiveTool(name: "Bash", summary: "Ran a command")

private func live(_ text: String?, _ tool: LiveTool? = nil, seq: Int, agent: String = "a1") -> ServerEvent {
    .replyLive(agentId: agent, text: text, tool: tool, seq: seq)
}

/// The (text, tool) pairs a client ends up seeing, frame by frame.
private func states(_ events: [ServerEvent?]) -> [(String?, LiveTool?)] {
    events.compactMap { if case .replyLive(_, let text, let tool, _) = $0 { (text, tool) } else { nil } }
}

@Suite struct LiveReplyTrackerTests {
    @Test func firstOfferSendsRightAway() async {
        let tracker = LiveReplyTracker(minInterval: 0.25)
        let ev = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(ev == live("Hello", seq: 1))
    }

    @Test func unchangedTextIsNotResent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello")
        let ev = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(ev == nil)
    }

    @Test func throttlesWithinTheMinInterval() async {
        let clock = TestClock()
        let tracker = LiveReplyTracker(minInterval: 0.25, clock: clock.now)
        let first = await tracker.offer(agentId: "a1", text: "Hello")
        #expect(first != nil)
        clock.advance(0.1)
        let second = await tracker.offer(agentId: "a1", text: "Hello there")
        #expect(second == nil)  // throttled, even though the text changed
        clock.advance(0.2)
        let third = await tracker.offer(agentId: "a1", text: "Hello there")
        #expect(third == live("Hello there", seq: 2))
    }

    @Test func landedTextClearsAndSuppressesAMatchingOffer() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello there")
        let landedEvent = await tracker.landed(agentId: "a1", texts: ["Hello there, world"], toolCalls: [])
        #expect(landedEvent == live(nil, seq: 2))
        // Now that the transcript has caught up, offering the same (or a shorter) tail is suppressed.
        #expect(await tracker.offer(agentId: "a1", text: "Hello there") == nil)
        #expect(await tracker.offer(agentId: "a1", text: "Hello there, world") == nil)
    }

    @Test func landedMarkdownStillMatchesTheRenderedScreen() async {
        // codex/claude render markdown: the screen has no backticks or `**`, the transcript does.
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "The command waited six seconds, then printed relay-slow-ok.")
        let cleared = await tracker.landed(agentId: "a1", texts: ["The command waited six seconds, then printed `relay-slow-ok`."], toolCalls: [])
        #expect(cleared == live(nil, seq: 2))
        // The screen still shows it until the agent goes idle: it must not come back.
        #expect(await tracker.offer(agentId: "a1", text: "The command waited six seconds, then printed relay-slow-ok.") == nil)
    }

    @Test func aToolOnlyMessageDoesNotBringBackLandedText() async {
        // The grouped assistant message grows with a toolCall: the earlier landed text stays landed.
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Let me check.")
        _ = await tracker.landed(agentId: "a1", texts: ["Let me check."], toolCalls: [])
        _ = await tracker.landed(agentId: "a1", texts: ["Let me check."], toolCalls: [(id: "t1", summary: "Ran ls")])
        #expect(await tracker.offer(agentId: "a1", text: "Let me check.") == nil)
    }

    @Test func landedWithNothingLiveSendsNoEvent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let ev = await tracker.landed(agentId: "a1", texts: ["some text"], toolCalls: [])
        #expect(ev == nil)
    }

    @Test func stoppedClearsTextAndTool() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Hello", tool: ping)
        _ = await tracker.offer(agentId: "a1", text: "Hello", tool: ping)
        let ev = await tracker.stopped(agentId: "a1")
        #expect(ev == live(nil, nil, seq: 3))
        // A second stop is a no-op: nothing to clear.
        #expect(await tracker.stopped(agentId: "a1") == nil)
    }

    @Test func emptyTextNeverOffersAFrame() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        #expect(await tracker.offer(agentId: "a1", text: "") == nil)
    }

    @Test func sequenceKeepsIncreasingAcrossOffersAndClears() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let e1 = await tracker.offer(agentId: "a1", text: "One")
        let e2 = await tracker.offer(agentId: "a1", text: "One two")
        let e3 = await tracker.stopped(agentId: "a1")
        let e4 = await tracker.offer(agentId: "a1", text: "Next turn")
        #expect([e1, e2, e3, e4].compactMap { if case .replyLive(_, _, _, let seq) = $0 { seq } else { nil } } == [1, 2, 3, 4])
    }

    @Test func differentAgentsAreIndependent() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        let a = await tracker.offer(agentId: "a1", text: "Hello")
        let b = await tracker.offer(agentId: "a2", text: "Hello")
        #expect(a == live("Hello", seq: 1))
        #expect(b == live("Hello", seq: 1, agent: "a2"))
    }

    // MARK: Stability

    @Test func anEmptyOrShorterReadKeepsTheText() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "The answer starts here")
        #expect(await tracker.offer(agentId: "a1", text: nil) == nil)
        #expect(await tracker.offer(agentId: "a1", text: "The answer") == nil)
        #expect(await tracker.offer(agentId: "a1", text: "The answer starts here and grows") == live("The answer starts here and grows", seq: 2))
    }

    @Test func aDifferentTextNeedsTwoReadsThenNeverFlipsBack() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "First block of prose.")
        #expect(await tracker.offer(agentId: "a1", text: "Second block") == nil)  // seen once: pending
        #expect(await tracker.offer(agentId: "a1", text: "Second block grows") == live("Second block grows", seq: 2))
        // A glitchy read of the old block never brings it back, even twice in a row.
        #expect(await tracker.offer(agentId: "a1", text: "First block of prose.") == nil)
        #expect(await tracker.offer(agentId: "a1", text: "First block of prose.") == nil)
    }

    @Test func aOneOffDifferentReadIsIgnored() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Real reply text")
        #expect(await tracker.offer(agentId: "a1", text: "garbage from a half-drawn frame") == nil)
        #expect(await tracker.offer(agentId: "a1", text: "Real reply text, more") == live("Real reply text, more", seq: 2))
    }

    @Test func aScrolledReadExtendsTheText() async {
        // Once the reply is taller than the viewport, the read only has its tail.
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Paragraph one is here.\n\nParagraph two is being written")
        let ev = await tracker.offer(agentId: "a1", text: "Paragraph two is being written right now.\n\nThree")
        #expect(ev == live("Paragraph one is here.\n\nParagraph two is being written right now.\n\nThree", seq: 2))
    }

    @Test func toolNeedsTwoReadsToAppear() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == nil)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == live(nil, ping, seq: 1))
    }

    @Test func genericToolRefinesAtOnce() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: nil, tool: genericBash, toolGeneric: true)
        _ = await tracker.offer(agentId: "a1", text: nil, tool: genericBash, toolGeneric: true)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == live(nil, ping, seq: 2))
    }

    @Test func landedToolCallClearsTheToolAndItStaysCleared() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: nil, tool: ping)
        _ = await tracker.offer(agentId: "a1", text: nil, tool: ping)
        let cleared = await tracker.landed(agentId: "a1", texts: [], toolCalls: [(id: "toolu_1", summary: "Ran ping -c 8 127.0.0.1")])
        #expect(cleared == live(nil, nil, seq: 2))
        // The block is still on screen while the command runs: no frame, twice in a row or not.
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == nil)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == nil)
    }

    @Test func aToolCallWithADifferentSummaryStillClearsTheBlock() async {
        // The screen's summary can't always match (a wrapped command); any toolCall that lands
        // after the block showed up is its toolCall.
        let tracker = LiveReplyTracker(minInterval: 0)
        let partial = LiveTool(name: "Bash", summary: "Ran for i in 1 2 3; do echo")
        _ = await tracker.offer(agentId: "a1", text: nil, tool: partial)
        _ = await tracker.offer(agentId: "a1", text: nil, tool: partial)
        let cleared = await tracker.landed(agentId: "a1", texts: [], toolCalls: [(id: "toolu_1", summary: "Ran for i in 1 2 3; do echo tick $i; done")])
        #expect(cleared == live(nil, nil, seq: 2))
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: partial) == nil)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: partial) == nil)
    }

    @Test func toolCallsLandedBeforeTheBlockDontClearIt() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.landed(agentId: "a1", texts: [], toolCalls: [(id: "toolu_0", summary: "Read README.md")])
        _ = await tracker.offer(agentId: "a1", text: nil, tool: ping)
        #expect(await tracker.offer(agentId: "a1", text: nil, tool: ping) == live(nil, ping, seq: 1))
    }

    @Test func tracksTextAndToolIndependently() async {
        let tracker = LiveReplyTracker(minInterval: 0)
        _ = await tracker.offer(agentId: "a1", text: "Let me ping it.", tool: ping)
        _ = await tracker.offer(agentId: "a1", text: "Let me ping it.", tool: ping)
        // The text lands first; the tool stays.
        let ev = await tracker.landed(agentId: "a1", texts: ["Let me ping it."], toolCalls: [])
        #expect(ev == live(nil, ping, seq: 3))
    }

    // MARK: Wire shape

    @Test func encodesToolAndNulls() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let withTool = String(decoding: try encoder.encode(ServerEvent.replyLive(agentId: "w1:p1", text: nil, tool: ping, seq: 3)), as: UTF8.self)
        #expect(withTool == #"{"agentId":"w1:p1","seq":3,"text":null,"tool":{"name":"Bash","state":"running","summary":"Ran ping -c 8 127.0.0.1"},"type":"reply.live"}"#)
        let cleared = String(decoding: try encoder.encode(ServerEvent.replyLive(agentId: "w1:p1", text: "Hi", tool: nil, seq: 4)), as: UTF8.self)
        #expect(cleared == #"{"agentId":"w1:p1","seq":4,"text":"Hi","tool":null,"type":"reply.live"}"#)
    }
}

/// Real screen sequences, through the parser and the tracker together: whatever the screen does
/// between reads, the client never sees a state come back once it was replaced.
@Suite struct LiveReplySequenceTests {
    private static let bottom = """
    ─────────────────────────────────────────────────────────────
    ❯
    ─────────────────────────────────────────────────────────────
      82.9k/1.0M · in 82.9k out 148 · wk 11%(4d12h)
    """

    private static func screen(_ turn: String) -> String {
        """
        ⏺ You chose: Left.

        ✻ Cooked for 3s · done 2:15 AM

        ❯ Run ping -c 8 127.0.0.1 then say one sentence about it.

        \(turn)

        ✻ Crafting… (3s · ↓ 23 tokens)
        \(bottom)
        """
    }

    /// Replays `screens` through parser + tracker the way `LiveReplyMonitor` does, interleaving
    /// transcript landings at the given read indexes. Returns what the client saw.
    private func replay(_ screens: [String], landings: [Int: [Block]] = [:]) async -> [(String?, LiveTool?)] {
        let tracker = LiveReplyTracker(minInterval: 0)
        let scrubber = PathScrubber(cwd: nil)
        var events: [ServerEvent?] = []
        for (i, s) in screens.enumerated() {
            if let blocks = landings[i] { events.append(await tracker.landed(agentId: "a1", blocks: blocks)) }
            let parsed = LiveReplyParser.parse(screen: s, kind: "claude")
            let tool = parsed.tool.map { LiveTool(name: $0.name, summary: $0.summary(scrubber: scrubber)) }
            events.append(await tracker.offer(agentId: "a1", text: parsed.text, tool: tool, toolGeneric: parsed.tool?.generic ?? false))
        }
        events.append(await tracker.stopped(agentId: "a1"))
        return states(events)
    }

    /// No state (text or tool) shows up again after the client moved away from it.
    private func expectNoFlipFlop(_ seen: [(String?, LiveTool?)], sourceLocation: SourceLocation = #_sourceLocation) {
        for (label, values) in [("text", seen.map { $0.0 }), ("tool", seen.map { $0.1?.summary })] {
            var left: Set<String> = []
            var previous: String??
            for v in values {
                if let p = previous, p != v, let p { left.insert(p) }
                if let v { #expect(!left.contains(v), "\(label) came back: \(v)", sourceLocation: sourceLocation) }
                previous = .some(v)
            }
        }
        for (text, _) in seen {
            #expect(text?.contains("Running") != true, sourceLocation: sourceLocation)
            #expect(text?.contains("ping -c") != true, sourceLocation: sourceLocation)
            #expect(text != "You chose: Left.", sourceLocation: sourceLocation)
        }
    }

    @Test func claudeToolRunWithBlinkingDot() async {
        // What round 6 captured live: the grouped label, then the described block with a blinking
        // `⏺`, the tool_use landing ~5s in, then the reply.
        let dotOn = Self.screen("⏺ Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
        let dotOff = Self.screen("  Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
        let screens = [
            Self.screen(""),
            Self.screen("⏺ Running 1 shell command…"),
            Self.screen("  Running 1 shell command…"),
            Self.screen("⏺ Running 1 shell command…"),
            dotOff, dotOn, dotOff, dotOn, dotOff, dotOn,  // index 9: tool_use lands before this read
            dotOff, dotOn, dotOff,
            Self.screen("⏺ Ran 1 shell command\n  ⎿  $ ping -c 8 127.0.0.1\n\n⏺ All 8 packets got"),
            Self.screen("⏺ Ran 1 shell command\n  ⎿  $ ping -c 8 127.0.0.1\n\n⏺ All 8 packets got through with no loss."),
        ]
        let toolUse = Block.toolCall(id: "toolu_1", name: "Bash", summary: "Ran ping -c 8 127.0.0.1", input: "{}")
        let seen = await replay(screens, landings: [9: [toolUse]])
        expectNoFlipFlop(seen)

        #expect(seen.map { $0.1?.summary } == ["Ran a command", "Ran ping -c 8 127.0.0.1", nil, nil, nil, nil])
        #expect(seen.map { $0.0 } == [nil, nil, nil, "All 8 packets got", "All 8 packets got through with no loss.", nil])
    }

    @Test func claudeProseThenToolThenProse() async {
        let screens = [
            Self.screen("⏺ Let me check the"),
            Self.screen("⏺ Let me check the loopback first."),
            Self.screen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
            Self.screen("⏺ Let me check the loopback first.\n\n  Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
            Self.screen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
            Self.screen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works"),
            Self.screen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works fine."),
            Self.screen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works fine."),
        ]
        let landedFirst = [Block.text("Let me check the loopback first."),
                           Block.toolCall(id: "toolu_1", name: "Bash", summary: "Ran ping -c 8 127.0.0.1", input: "{}")]
        let seen = await replay(screens, landings: [4: landedFirst])
        expectNoFlipFlop(seen)
        #expect(seen.contains { $0.1 == ping })
        #expect(seen.last.map { $0.0 == nil && $0.1 == nil } == true)
        #expect(seen.contains { $0.0 == "It works fine." })
    }

    @Test func alternatingGlitchReadsNeverFlipTheText() async {
        // Every other read catches a half-drawn frame with nothing parseable, or an old block.
        var screens: [String] = []
        let words = ["The", "The answer", "The answer is", "The answer is forty", "The answer is forty-two."]
        for w in words {
            screens.append(Self.screen("⏺ \(w)"))
            screens.append(Self.screen(""))
            screens.append(Self.screen("⏺ Some older block that was on screen"))
        }
        let seen = await replay(screens)
        expectNoFlipFlop(seen)
        #expect(seen.compactMap { $0.0 } == words)
    }
}

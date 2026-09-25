import Foundation
import Testing
@testable import RelayCore

@Suite struct LiveReplyParserTests {
    // MARK: Claude

    @Test func claudeGrowingParagraph() throws {
        let screen = """
        ❯ Write a 200-word explanation of how TCP congestion control works, no tools, no headers, just flowing prose.

        ⏺ TCP congestion control manages how much data a sender pushes into the network so it doesn't overwhelm links or routers, causing packet loss
          and collapse. Each connection maintains a congestion window (cwnd) that caps how many unacknowledged bytes can be in flight. Connections

        ✢ Sock-hopping… (3s · ↓ 150 tokens · thinking with medium effort)
          ⎿  Tip: Dynamic workflows let Claude write a script that orchestrates many agents for you.
        ──────────────────────────────────────────────────────────────────────────
        ❯
        """
        let text = try #require(LiveReplyParser.extract(screen: screen, kind: "claude"))
        #expect(text == "TCP congestion control manages how much data a sender pushes into the network so it doesn't overwhelm links or routers, causing packet loss and collapse. Each connection maintains a congestion window (cwnd) that caps how many unacknowledged bytes can be in flight. Connections")
    }

    @Test func claudeMultiParagraphKeepsBreaks() throws {
        let screen = """
        ⏺ First paragraph line one
          continues here.

          Second paragraph starts after a blank line.

        ✻ Worked for 7s · done 1:37 AM
        """
        let text = try #require(LiveReplyParser.extract(screen: screen, kind: "claude"))
        #expect(text == "First paragraph line one continues here.\n\nSecond paragraph starts after a blank line.")
    }

    @Test func claudeToolCallInProgressIsNotText() throws {
        let screen = """
        ⏺ Write(answers.txt)

        ✻ Crunched for 3s · running
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "claude") == nil)
    }

    @Test func claudeToolResultBlockIsNotText() throws {
        let screen = """
        ⏺ User answered Claude's questions:
          ⎿  · Tea or coffee? → Tea

        ❯
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "claude") == nil)
    }

    @Test func claudeSpinnerOnlyIsNotText() throws {
        let screen = """
        ❯ Write a 200-word explanation of how TCP congestion control works.

        ✢ Thinking… (1s · esc to interrupt)
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "claude") == nil)
    }

    @Test func claudeFallsBackWhenTheMarkerScrolledOff() throws {
        // No `⏺` anywhere in this window: a long reply whose block start has scrolled past it.
        let screen = """
          instead reconstruct the whole picture themselves and reason about it directly.

          To keep this scalable, large networks are divided into areas, with a backbone area gluing the others together, so that detailed
          topology information stays contained within an area and only summarized reachability information crosses area boundaries. When
                                                                                 Update available! Run: brew upgrade claude-code@latest
        ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        ❯
        ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
          72.2k/1.0M · in 72.2k out 977 · 5h 10%(2h13m) · wk 11%(4d15h)
          ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent
        """
        let text = try #require(LiveReplyParser.extract(screen: screen, kind: "claude"))
        #expect(text == "instead reconstruct the whole picture themselves and reason about it directly.\n\nTo keep this scalable, large networks are divided into areas, with a backbone area gluing the others together, so that detailed topology information stays contained within an area and only summarized reachability information crosses area boundaries. When")
    }

    @Test func claudeFallbackIgnoresThePromptEcho() throws {
        // Only the user's own prompt and the empty input box are visible: nothing to show yet.
        let screen = """
        ❯ Write a 500-word explanation of how OSPF routing works end to end, no tools, no headers or lists, just flowing prose.

        ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        ❯
        ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
          69.1k/1.0M · in 69.1k out 1.2k · 5h 10%(2h19m)
          ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "claude") == nil)
    }

    @Test func claudeStripsTheUpdateBannerBleedingIntoAParagraph() throws {
        // Claude Code draws "Update available!" at a fixed row; once the alternate screen scrolls,
        // it can land concatenated onto a real content row instead of its own line.
        let screen = """
        ⏺ If there's no cached answer, the query goes out to a recursive resolver, typically operated by
          your ISP or a public service like 1.1.1.1 or 8.                                             Update available! Run: brew upgrade claude-code@latest
          down the answer.

        ✻ Crunched for 3s · running
        """
        let text = try #require(LiveReplyParser.extract(screen: screen, kind: "claude"))
        #expect(!text.contains("Update available"))
        #expect(text.contains("your ISP or a public service like 1.1.1.1 or 8."))
        #expect(text.contains("down the answer."))
    }

    @Test func claudeShortWordAnswerIsText() throws {
        let screen = """
        ⏺ HERDJZHQ

        ✻ Crunched for 3s · done 10:20 PM
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "claude") == "HERDJZHQ")
    }

    // MARK: codex

    @Test func codexWorkingPlaceholderIsNotText() throws {
        let screen = """
        › Write a 200-word explanation of how DNS resolution works, no tools, just flowing prose.

        • Working (14s • esc to interrupt)

        › Ask Codex to do anything
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "codex") == nil)
    }

    @Test func codexToolCallInProgressIsNotText() throws {
        let screen = """
        • Ran curl -sI https://example.com/herd-s2zsvs | head -1
          └ HTTP/2 404

        › Ask Codex to do anything
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "codex") == nil)
    }

    @Test func codexReplyBulletIsText() throws {
        let screen = """
        • I'll request network approval for that exact command.

        › Ask Codex to do anything
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "codex") == "I'll request network approval for that exact command.")
    }

    @Test func codexModelChangeConfirmationIsNotText() throws {
        let screen = """
        • Model changed to gpt-6-luna high for this session only

        › Ask Codex to do anything
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "codex") == nil)
    }

    // MARK: pi

    @Test func piWorkingSpinnerIsNotText() throws {
        let screen = """
         Write a 150-word explanation of how BGP routing works, no tools, flowing prose.




        ── ⠇ Working ──────────────────────────────────────────────

        ──────────────────────────────────────────────────────────
        ~/.herd/e2e (master)
        $0.000 (sub) 0.0%/272k (auto)
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "pi") == nil)
    }

    @Test func piTrailingParagraphIsText() throws {
        let screen = """
         Write a 150-word explanation of how BGP routing works, no tools, flowing prose.

         BGP routing lets independently operated networks exchange reachability
         information so packets can cross the wider internet.

        ──────────────────────────────────────────────────────────
        ~/.herd/e2e (master)
        $0.000 (sub) 0.0%/272k (auto)
        """
        let text = try #require(LiveReplyParser.extract(screen: screen, kind: "pi"))
        #expect(text == "BGP routing lets independently operated networks exchange reachability information so packets can cross the wider internet.")
    }

    @Test func piChromeOnlyIsNotText() throws {
        let screen = """
        Update Available
        New version 0.87.1 is available. Run pi update
        Changelog: https://pi.dev/changelog

        Model: claude-fable-5

        Warning: Anthropic subscription auth is active. Third-party harness usage draws from extra usage.

        Thinking level: high

        ──────────────────────────────────────────────────────────
        ~/.herd/e2e (master)
        $0.000 (sub) 0.0%/272k (auto)
        """
        #expect(LiveReplyParser.extract(screen: screen, kind: "pi") == nil)
    }

    // MARK: Tools (claude)

    /// The input box and footer claude draws under every screen.
    private static let claudeBottom = """
                                                                             Update available! Run: brew upgrade claude-code@latest
    ─────────────────────────────────────────────────────────────────────────────────────
    ❯
    ─────────────────────────────────────────────────────────────────────────────────────
      82.9k/1.0M · in 82.9k out 148 · wk 11%(4d12h)
      ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent
    """

    /// The previous turn's reply is above the prompt echo; the current turn only has a tool.
    private static func claudeMidTool(_ block: String, spinner: String = "✻ Crafting… (3s · ↓ 23 tokens)") -> String {
        """
        ⏺ You chose: Left.

        ✻ Cooked for 3s · done 2:15 AM

        ❯ Run this exact bash command and nothing else: ping -c 8 127.0.0.1 . Then say one short
          sentence about it.

        \(block)

        \(spinner)
          ⎿  Tip: Dynamic workflows let Claude write a script that orchestrates many agents for you.
        \(claudeBottom)
        """
    }

    @Test func claudeGroupedRunningLabelIsAToolNotText() throws {
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool("⏺ Running 1 shell command…"), kind: "claude")
        #expect(parsed.text == nil)  // not "Running 1 shell command…", and not the previous turn's reply
        let tool = try #require(parsed.tool)
        #expect(tool.name == "Bash")
        #expect(tool.generic)
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == "Ran a command")
    }

    @Test func claudeDescribedToolWithCommandLine() throws {
        let block = """
        ⏺ Pinging localhost 8 times · 2s
          ⎿  $ ping -c 8 127.0.0.1 (3s · 5 lines)
             (ctrl+b to run in background)
        """
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(block), kind: "claude")
        #expect(parsed.text == nil)
        let tool = try #require(parsed.tool)
        #expect(tool == LiveToolCall(name: "Bash", input: ["command": "ping -c 8 127.0.0.1"]))
        // Same summary the transcript's toolCall gets for `{"command": "ping -c 8 127.0.0.1"}`.
        let transcript = ToolSummary.summary(name: "Bash", input: ["command": "ping -c 8 127.0.0.1"], scrubber: PathScrubber(cwd: nil))
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == transcript)
    }

    @Test func claudeBlinkingToolDotParsesTheSame() throws {
        // A running tool's `⏺` blinks: every other frame the header has no marker at all.
        let on = Self.claudeMidTool("⏺ Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
        let off = Self.claudeMidTool("  Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
        #expect(LiveReplyParser.parse(screen: on, kind: "claude") == LiveReplyParser.parse(screen: off, kind: "claude"))
        #expect(LiveReplyParser.parse(screen: off, kind: "claude").text == nil)
    }

    @Test func claudeProseThenToolKeepsTheProse() throws {
        let block = """
        ⏺ Let me ping it first to check the loopback
          interface is up.

          Bash(ping -c 8 127.0.0.1)
          ⎿  Running…
        """
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(block), kind: "claude")
        #expect(parsed.text == "Let me ping it first to check the loopback interface is up.")
        #expect(parsed.tool == LiveToolCall(name: "Bash", input: ["command": "ping -c 8 127.0.0.1"]))
    }

    @Test func claudeWrappedCallSignature() throws {
        let block = """
        ⏺ Bash(for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1;
              done)
          ⎿  Running…
        """
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(block), kind: "claude")
        #expect(parsed.text == nil)
        #expect(parsed.tool?.input["command"] == "for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1; done")
    }

    @Test func claudeWrappedSignatureWithoutOutputYet() throws {
        // Before `⎿` shows up, a wrapped header must still not leak out as prose.
        let block = """
          Bash(for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1;
              done)
        """
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(block), kind: "claude")
        #expect(parsed.text == nil)
        #expect(parsed.tool?.name == "Bash")
    }

    @Test func claudePathToolsMapToTranscriptNames() throws {
        let cases: [(String, String, String)] = [
            ("⏺ Update(/Users/dev/shop-api/Sources/App.swift)", "Edit", "Edited Sources/App.swift"),
            ("⏺ Read(/Users/dev/shop-api/README.md)", "Read", "Read README.md"),
            ("⏺ Write(notes.txt)", "Write", "Wrote notes.txt"),
            ("⏺ Search(pattern: \"TODO\", path: \"Sources\")", "Grep", "Searched for TODO"),
            ("⏺ Fetch(https://example.com)", "WebFetch", "Fetched https://example.com"),
            ("⏺ Web Search(\"swift actors\")", "WebSearch", "Searched the web for swift actors"),
        ]
        let scrubber = PathScrubber(cwd: "/Users/dev/shop-api")
        for (header, name, summary) in cases {
            let tool = try #require(LiveReplyParser.parse(screen: Self.claudeMidTool(header + "\n  ⎿  Running…"), kind: "claude").tool)
            #expect(tool.name == name)
            #expect(tool.summary(scrubber: scrubber) == summary)
        }
    }

    @Test func claudeDotSpinnerIsNotText() throws {
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool("", spinner: "· Crafting… (2s · ↓ 10 tokens)"), kind: "claude")
        #expect(parsed == LiveScreen())
    }

    @Test func claudePreviousTurnNeverLeaks() throws {
        // Right after the prompt, before anything of the new turn is drawn.
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(""), kind: "claude")
        #expect(parsed.text == nil)
        #expect(parsed.tool == nil)
    }

    @Test func claudeTextAfterAFinishedToolIsTextOnly() throws {
        let block = """
        ⏺ Ran 1 shell command
          ⎿  $ ping -c 8 127.0.0.1

        ⏺ All 8 packets to localhost got through
        """
        let parsed = LiveReplyParser.parse(screen: Self.claudeMidTool(block), kind: "claude")
        #expect(parsed.text == "All 8 packets to localhost got through")
        #expect(parsed.tool == nil)
    }

    // MARK: Tools (codex)

    @Test func codexRunningCellIsATool() throws {
        let screen = """
        › Run the command `ping -c 8 127.0.0.1` exactly.

        • I'll run it now.

        • Running ping -c 8 127.0.0.1
          └ PING 127.0.0.1 (127.0.0.1): 56 data bytes
            64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.042 ms

        • Working (3s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close

        › Ask Codex to do anything

          GPT-6-Luna xhigh · ~/.relay/e2e · Run echo relay-ok
        """
        let parsed = LiveReplyParser.parse(screen: screen, kind: "codex")
        #expect(parsed.text == "I'll run it now.")
        let tool = try #require(parsed.tool)
        #expect(tool.name == "Shell")
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == "Ran ping -c 8 127.0.0.1")
    }

    @Test func codexBackgroundTerminalStatusIsNotText() throws {
        let screen = """
        › Run the command `ping -c 8 127.0.0.1` exactly.

        • The ping failed

          1 background terminal running · /ps to view · /stop to close

        › Ask Codex to do anything
        """
        let parsed = LiveReplyParser.parse(screen: screen, kind: "codex")
        #expect(parsed.text == "The ping failed")
        #expect(parsed.tool == nil)
    }

    @Test func codexPreviousTurnNeverLeaks() throws {
        let screen = """
        • relay-ok

          2:44 PM

        › Run the command `sleep 6 && echo relay-slow-ok` exactly.

        • Working (2s • esc to interrupt)

        › Ask Codex to do anything
        """
        #expect(LiveReplyParser.parse(screen: screen, kind: "codex") == LiveScreen())
    }

    @Test func codexEditedCell() throws {
        let screen = """
        • Edited Sources/App.swift (+2 -1)
            1 +import Foundation

        › Ask Codex to do anything
        """
        let tool = try #require(LiveReplyParser.parse(screen: screen, kind: "codex").tool)
        #expect(tool.name == "Edit")
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == "Edited Sources/App.swift")
    }

    // MARK: Tools (pi)

    private static let piBottom = """


    ── ⠸ Working ─────────────────────────────────────────────────────────────

    ──────────────────────────────────────────────────────────────────────────
    ~/.herd/e2e (master)
    ↑5.1k ↓4.8k R2.6k CH51.7% $0.024 1.5%/262k (auto)          (opencode-go) kimi-k2.6 • high
    """

    @Test func piRunningBashBoxIsATool() throws {
        let screen = """
         Run this exact shell command and nothing else: ping -c 8 127.0.0.1


         The user wants me to run the exact shell command ping -c 8 127.0.0.1.


         $ ping -c 8 127.0.0.1

         PING 127.0.0.1 (127.0.0.1): 56 data bytes
         64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.042 ms

         Elapsed 3.1s
        \(Self.piBottom)
        """
        let parsed = LiveReplyParser.parse(screen: screen, kind: "pi")
        #expect(parsed.text == nil)
        let tool = try #require(parsed.tool)
        #expect(tool.name == "bash")
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == "Ran ping -c 8 127.0.0.1")
    }

    @Test func piFinishedBashBoxIsNotATool() throws {
        let screen = """
         $ ping -c 8 127.0.0.1

         round-trip min/avg/max/stddev = 0.028/0.040/0.051/0.007 ms

         Took 7.1s

         All eight pings to the loopback address succeeded with zero packet loss.

        ──────────────────────────────────────────────────────────────────────────
        ~/.herd/e2e (master)
        ↑7.4k ↓4.8k R4.6k CH47.1% $0.027 1.7%/262k (auto)          (opencode-go) kimi-k2.6 • high
        """
        let parsed = LiveReplyParser.parse(screen: screen, kind: "pi")
        #expect(parsed.tool == nil)
        #expect(parsed.text == "All eight pings to the loopback address succeeded with zero packet loss.")
    }

    @Test func piReadBoxIsATool() throws {
        let screen = """
         read Sources/App.swift:1-40

         import Foundation
        \(Self.piBottom)
        """
        let tool = try #require(LiveReplyParser.parse(screen: screen, kind: "pi").tool)
        #expect(tool == LiveToolCall(name: "read", input: ["path": "Sources/App.swift"]))
        #expect(tool.summary(scrubber: PathScrubber(cwd: nil)) == "Read Sources/App.swift")
    }

    // MARK: other kinds

    @Test func unknownKindIsNotText() throws {
        #expect(LiveReplyParser.extract(screen: "⏺ hi", kind: "gemini") == nil)
    }
}

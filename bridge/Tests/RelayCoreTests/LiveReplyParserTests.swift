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
        #expect(text == "To keep this scalable, large networks are divided into areas, with a backbone area gluing the others together, so that detailed topology information stays contained within an area and only summarized reachability information crosses area boundaries. When")
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

    // MARK: other kinds

    @Test func unknownKindIsNotText() throws {
        #expect(LiveReplyParser.extract(screen: "⏺ hi", kind: "gemini") == nil)
    }
}

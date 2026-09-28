package live

import (
	"reflect"
	"strings"
	"testing"

	"relay/internal/transcript"
)

// Ported from LiveReplyParserTests.swift, same cases and names.

func textOf(t *testing.T, screen, kind string) string {
	t.Helper()
	text := Extract(screen, kind)
	if text == nil {
		t.Fatalf("no text in:\n%s", screen)
	}
	return *text
}

func expectNoText(t *testing.T, screen, kind string) {
	t.Helper()
	if text := Extract(screen, kind); text != nil {
		t.Fatalf("text = %q, want nil", *text)
	}
}

func expectEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func toolOf(t *testing.T, s Screen) *ToolCall {
	t.Helper()
	if s.Tool == nil {
		t.Fatal("no tool")
	}
	return s.Tool
}

var noCwd = transcript.NewScrubber("")

// MARK: Claude

func TestClaudeGrowingParagraph(t *testing.T) {
	screen := `❯ Write a 200-word explanation of how TCP congestion control works, no tools, no headers, just flowing prose.

⏺ TCP congestion control manages how much data a sender pushes into the network so it doesn't overwhelm links or routers, causing packet loss
  and collapse. Each connection maintains a congestion window (cwnd) that caps how many unacknowledged bytes can be in flight. Connections

✢ Sock-hopping… (3s · ↓ 150 tokens · thinking with medium effort)
  ⎿  Tip: Dynamic workflows let Claude write a script that orchestrates many agents for you.
──────────────────────────────────────────────────────────────────────────
❯`
	expectEqual(t, textOf(t, screen, "claude"), "TCP congestion control manages how much data a sender pushes into the network so it doesn't overwhelm links or routers, causing packet loss and collapse. Each connection maintains a congestion window (cwnd) that caps how many unacknowledged bytes can be in flight. Connections")
}

func TestClaudeMultiParagraphKeepsBreaks(t *testing.T) {
	screen := `⏺ First paragraph line one
  continues here.

  Second paragraph starts after a blank line.

✻ Worked for 7s · done 1:37 AM`
	expectEqual(t, textOf(t, screen, "claude"), "First paragraph line one continues here.\n\nSecond paragraph starts after a blank line.")
}

func TestClaudeToolCallInProgressIsNotText(t *testing.T) {
	expectNoText(t, "⏺ Write(answers.txt)\n\n✻ Crunched for 3s · running", "claude")
}

func TestClaudeToolResultBlockIsNotText(t *testing.T) {
	screen := `⏺ User answered Claude's questions:
  ⎿  · Tea or coffee? → Tea

❯`
	expectNoText(t, screen, "claude")
}

func TestClaudeSpinnerOnlyIsNotText(t *testing.T) {
	screen := `❯ Write a 200-word explanation of how TCP congestion control works.

✢ Thinking… (1s · esc to interrupt)`
	expectNoText(t, screen, "claude")
}

func TestClaudeFallsBackWhenTheMarkerScrolledOff(t *testing.T) {
	// No `⏺` anywhere in this window: a long reply whose block start has scrolled past it.
	screen := `  instead reconstruct the whole picture themselves and reason about it directly.

  To keep this scalable, large networks are divided into areas, with a backbone area gluing the others together, so that detailed
  topology information stays contained within an area and only summarized reachability information crosses area boundaries. When
                                                                         Update available! Run: brew upgrade claude-code@latest
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  72.2k/1.0M · in 72.2k out 977 · 5h 10%(2h13m) · wk 11%(4d15h)
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent`
	expectEqual(t, textOf(t, screen, "claude"), "instead reconstruct the whole picture themselves and reason about it directly.\n\nTo keep this scalable, large networks are divided into areas, with a backbone area gluing the others together, so that detailed topology information stays contained within an area and only summarized reachability information crosses area boundaries. When")
}

func TestClaudeFallbackIgnoresThePromptEcho(t *testing.T) {
	// Only the user's own prompt and the empty input box are visible: nothing to show yet.
	screen := `❯ Write a 500-word explanation of how OSPF routing works end to end, no tools, no headers or lists, just flowing prose.

──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  69.1k/1.0M · in 69.1k out 1.2k · 5h 10%(2h19m)
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent`
	expectNoText(t, screen, "claude")
}

func TestClaudeStripsTheUpdateBannerBleedingIntoAParagraph(t *testing.T) {
	// Claude Code draws "Update available!" at a fixed row; once the alternate screen scrolls,
	// it can land concatenated onto a real content row instead of its own line.
	screen := `⏺ If there's no cached answer, the query goes out to a recursive resolver, typically operated by
  your ISP or a public service like 1.1.1.1 or 8.                                             Update available! Run: brew upgrade claude-code@latest
  down the answer.

✻ Crunched for 3s · running`
	text := textOf(t, screen, "claude")
	if strings.Contains(text, "Update available") {
		t.Fatalf("banner in %q", text)
	}
	if !strings.Contains(text, "your ISP or a public service like 1.1.1.1 or 8.") || !strings.Contains(text, "down the answer.") {
		t.Fatalf("text lost: %q", text)
	}
}

func TestClaudeShortWordAnswerIsText(t *testing.T) {
	expectEqual(t, textOf(t, "⏺ HERDJZHQ\n\n✻ Crunched for 3s · done 10:20 PM", "claude"), "HERDJZHQ")
}

// MARK: codex

func TestCodexWorkingPlaceholderIsNotText(t *testing.T) {
	screen := `› Write a 200-word explanation of how DNS resolution works, no tools, just flowing prose.

• Working (14s • esc to interrupt)

› Ask Codex to do anything`
	expectNoText(t, screen, "codex")
}

func TestCodexToolCallInProgressIsNotText(t *testing.T) {
	screen := `• Ran curl -sI https://example.com/herd-s2zsvs | head -1
  └ HTTP/2 404

› Ask Codex to do anything`
	expectNoText(t, screen, "codex")
}

func TestCodexReplyBulletIsText(t *testing.T) {
	screen := "• I'll request network approval for that exact command.\n\n› Ask Codex to do anything"
	expectEqual(t, textOf(t, screen, "codex"), "I'll request network approval for that exact command.")
}

func TestCodexModelChangeConfirmationIsNotText(t *testing.T) {
	expectNoText(t, "• Model changed to gpt-6-luna high for this session only\n\n› Ask Codex to do anything", "codex")
}

// MARK: pi

func TestPiWorkingSpinnerIsNotText(t *testing.T) {
	screen := ` Write a 150-word explanation of how BGP routing works, no tools, flowing prose.




── ⠇ Working ──────────────────────────────────────────────

──────────────────────────────────────────────────────────
~/.herd/e2e (master)
$0.000 (sub) 0.0%/272k (auto)`
	expectNoText(t, screen, "pi")
}

func TestPiTrailingParagraphIsText(t *testing.T) {
	screen := ` Write a 150-word explanation of how BGP routing works, no tools, flowing prose.

 BGP routing lets independently operated networks exchange reachability
 information so packets can cross the wider internet.

──────────────────────────────────────────────────────────
~/.herd/e2e (master)
$0.000 (sub) 0.0%/272k (auto)`
	expectEqual(t, textOf(t, screen, "pi"), "BGP routing lets independently operated networks exchange reachability information so packets can cross the wider internet.")
}

func TestPiChromeOnlyIsNotText(t *testing.T) {
	screen := `Update Available
New version 0.87.1 is available. Run pi update
Changelog: https://pi.dev/changelog

Model: claude-fable-5

Warning: Anthropic subscription auth is active. Third-party harness usage draws from extra usage.

Thinking level: high

──────────────────────────────────────────────────────────
~/.herd/e2e (master)
$0.000 (sub) 0.0%/272k (auto)`
	expectNoText(t, screen, "pi")
}

// MARK: Tools (claude)

// claudeBottom is the input box and footer claude draws under every screen.
const claudeBottom = `                                                                         Update available! Run: brew upgrade claude-code@latest
─────────────────────────────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────────────────────────────
  82.9k/1.0M · in 82.9k out 148 · wk 11%(4d12h)
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent`

const defaultSpinner = "✻ Crafting… (3s · ↓ 23 tokens)"

// claudeMidTool: the previous turn's reply is above the prompt echo; the current turn only has a tool.
func claudeMidTool(block, spinner string) string {
	return `⏺ You chose: Left.

✻ Cooked for 3s · done 2:15 AM

❯ Run this exact bash command and nothing else: ping -c 8 127.0.0.1 . Then say one short
  sentence about it.

` + block + `

` + spinner + `
  ⎿  Tip: Dynamic workflows let Claude write a script that orchestrates many agents for you.
` + claudeBottom
}

func TestClaudeGroupedRunningLabelIsAToolNotText(t *testing.T) {
	parsed := Parse(claudeMidTool("⏺ Running 1 shell command…", defaultSpinner), "claude")
	expectEqual(t, parsed.Text, (*string)(nil)) // not "Running 1 shell command…", and not the previous turn's reply
	tool := toolOf(t, parsed)
	expectEqual(t, tool.Name, "Bash")
	expectEqual(t, tool.Generic, true)
	expectEqual(t, tool.Summary(noCwd), "Ran a command")
}

func TestClaudeDescribedToolWithCommandLine(t *testing.T) {
	block := `⏺ Pinging localhost 8 times · 2s
  ⎿  $ ping -c 8 127.0.0.1 (3s · 5 lines)
     (ctrl+b to run in background)`
	parsed := Parse(claudeMidTool(block, defaultSpinner), "claude")
	expectEqual(t, parsed.Text, (*string)(nil))
	tool := toolOf(t, parsed)
	expectEqual(t, tool, newTool("Bash", map[string]string{"command": "ping -c 8 127.0.0.1"}))
	// Same summary the transcript's toolCall gets for `{"command": "ping -c 8 127.0.0.1"}`.
	transcriptSummary := transcript.Summary("Bash", map[string]any{"command": "ping -c 8 127.0.0.1"}, noCwd)
	expectEqual(t, tool.Summary(noCwd), transcriptSummary)
}

func TestClaudeBlinkingToolDotParsesTheSame(t *testing.T) {
	// A running tool's `⏺` blinks: every other frame the header has no marker at all.
	on := claudeMidTool("⏺ Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1", defaultSpinner)
	off := claudeMidTool("  Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1", defaultSpinner)
	expectEqual(t, Parse(on, "claude"), Parse(off, "claude"))
	expectEqual(t, Parse(off, "claude").Text, (*string)(nil))
}

func TestClaudeProseThenToolKeepsTheProse(t *testing.T) {
	block := `⏺ Let me ping it first to check the loopback
  interface is up.

  Bash(ping -c 8 127.0.0.1)
  ⎿  Running…`
	parsed := Parse(claudeMidTool(block, defaultSpinner), "claude")
	expectEqual(t, parsed.Text, strPtr("Let me ping it first to check the loopback interface is up."))
	expectEqual(t, parsed.Tool, newTool("Bash", map[string]string{"command": "ping -c 8 127.0.0.1"}))
}

func TestClaudeWrappedCallSignature(t *testing.T) {
	block := `⏺ Bash(for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1;
      done)
  ⎿  Running…`
	parsed := Parse(claudeMidTool(block, defaultSpinner), "claude")
	expectEqual(t, parsed.Text, (*string)(nil))
	expectEqual(t, toolOf(t, parsed).Input["command"], "for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1; done")
}

func TestClaudeWrappedSignatureWithoutOutputYet(t *testing.T) {
	// Before `⎿` shows up, a wrapped header must still not leak out as prose.
	block := `  Bash(for i in 1 2 3 4 5 6 7 8 9 10; do echo tick $i; sleep 1;
      done)`
	parsed := Parse(claudeMidTool(block, defaultSpinner), "claude")
	expectEqual(t, parsed.Text, (*string)(nil))
	expectEqual(t, toolOf(t, parsed).Name, "Bash")
}

func TestClaudePathToolsMapToTranscriptNames(t *testing.T) {
	cases := []struct{ header, name, summary string }{
		{"⏺ Update(/Users/dev/shop-api/Sources/App.swift)", "Edit", "Edited Sources/App.swift"},
		{"⏺ Read(/Users/dev/shop-api/README.md)", "Read", "Read README.md"},
		{"⏺ Write(notes.txt)", "Write", "Wrote notes.txt"},
		{"⏺ Search(pattern: \"TODO\", path: \"Sources\")", "Grep", "Searched for TODO"},
		{"⏺ Fetch(https://example.com)", "WebFetch", "Fetched https://example.com"},
		{"⏺ Web Search(\"swift actors\")", "WebSearch", "Searched the web for swift actors"},
	}
	scrubber := transcript.NewScrubber("/Users/dev/shop-api")
	for _, c := range cases {
		tool := toolOf(t, Parse(claudeMidTool(c.header+"\n  ⎿  Running…", defaultSpinner), "claude"))
		expectEqual(t, tool.Name, c.name)
		expectEqual(t, tool.Summary(scrubber), c.summary)
	}
}

func TestClaudeDotSpinnerIsNotText(t *testing.T) {
	parsed := Parse(claudeMidTool("", "· Crafting… (2s · ↓ 10 tokens)"), "claude")
	expectEqual(t, parsed, Screen{})
}

func TestClaudePreviousTurnNeverLeaks(t *testing.T) {
	// Right after the prompt, before anything of the new turn is drawn.
	parsed := Parse(claudeMidTool("", defaultSpinner), "claude")
	expectEqual(t, parsed, Screen{})
}

func TestClaudeTextAfterAFinishedToolIsTextOnly(t *testing.T) {
	block := `⏺ Ran 1 shell command
  ⎿  $ ping -c 8 127.0.0.1

⏺ All 8 packets to localhost got through`
	parsed := Parse(claudeMidTool(block, defaultSpinner), "claude")
	expectEqual(t, parsed.Text, strPtr("All 8 packets to localhost got through"))
	expectEqual(t, parsed.Tool, (*ToolCall)(nil))
}

// MARK: Tools (codex)

func TestCodexRunningCellIsATool(t *testing.T) {
	screen := "› Run the command `ping -c 8 127.0.0.1` exactly." + `

• I'll run it now.

• Running ping -c 8 127.0.0.1
  └ PING 127.0.0.1 (127.0.0.1): 56 data bytes
    64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.042 ms

• Working (3s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close

› Ask Codex to do anything

  GPT-6-Luna xhigh · ~/.relay/e2e · Run echo relay-ok`
	parsed := Parse(screen, "codex")
	expectEqual(t, parsed.Text, strPtr("I'll run it now."))
	tool := toolOf(t, parsed)
	expectEqual(t, tool.Name, "Shell")
	expectEqual(t, tool.Summary(noCwd), "Ran ping -c 8 127.0.0.1")
}

func TestCodexBackgroundTerminalStatusIsNotText(t *testing.T) {
	screen := "› Run the command `ping -c 8 127.0.0.1` exactly." + `

• The ping failed

  1 background terminal running · /ps to view · /stop to close

› Ask Codex to do anything`
	parsed := Parse(screen, "codex")
	expectEqual(t, parsed.Text, strPtr("The ping failed"))
	expectEqual(t, parsed.Tool, (*ToolCall)(nil))
}

func TestCodexPreviousTurnNeverLeaks(t *testing.T) {
	screen := `• relay-ok

  2:44 PM

› Run the command ` + "`sleep 6 && echo relay-slow-ok`" + ` exactly.

• Working (2s • esc to interrupt)

› Ask Codex to do anything`
	expectEqual(t, Parse(screen, "codex"), Screen{})
}

func TestCodexEditedCell(t *testing.T) {
	screen := `• Edited Sources/App.swift (+2 -1)
    1 +import Foundation

› Ask Codex to do anything`
	tool := toolOf(t, Parse(screen, "codex"))
	expectEqual(t, tool.Name, "Edit")
	expectEqual(t, tool.Summary(noCwd), "Edited Sources/App.swift")
}

// MARK: Tools (pi)

const piBottom = `

── ⠸ Working ─────────────────────────────────────────────────────────────

──────────────────────────────────────────────────────────────────────────
~/.herd/e2e (master)
↑5.1k ↓4.8k R2.6k CH51.7% $0.024 1.5%/262k (auto)          (opencode-go) kimi-k2.6 • high`

func TestPiRunningBashBoxIsATool(t *testing.T) {
	screen := ` Run this exact shell command and nothing else: ping -c 8 127.0.0.1


 The user wants me to run the exact shell command ping -c 8 127.0.0.1.


 $ ping -c 8 127.0.0.1

 PING 127.0.0.1 (127.0.0.1): 56 data bytes
 64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.042 ms

 Elapsed 3.1s
` + piBottom
	parsed := Parse(screen, "pi")
	expectEqual(t, parsed.Text, (*string)(nil))
	tool := toolOf(t, parsed)
	expectEqual(t, tool.Name, "bash")
	expectEqual(t, tool.Summary(noCwd), "Ran ping -c 8 127.0.0.1")
}

func TestPiFinishedBashBoxIsNotATool(t *testing.T) {
	screen := ` $ ping -c 8 127.0.0.1

 round-trip min/avg/max/stddev = 0.028/0.040/0.051/0.007 ms

 Took 7.1s

 All eight pings to the loopback address succeeded with zero packet loss.

──────────────────────────────────────────────────────────────────────────
~/.herd/e2e (master)
↑7.4k ↓4.8k R4.6k CH47.1% $0.027 1.7%/262k (auto)          (opencode-go) kimi-k2.6 • high`
	parsed := Parse(screen, "pi")
	expectEqual(t, parsed.Tool, (*ToolCall)(nil))
	expectEqual(t, parsed.Text, strPtr("All eight pings to the loopback address succeeded with zero packet loss."))
}

func TestPiReadBoxIsATool(t *testing.T) {
	screen := ` read Sources/App.swift:1-40

 import Foundation
` + piBottom
	tool := toolOf(t, Parse(screen, "pi"))
	expectEqual(t, tool, newTool("read", map[string]string{"path": "Sources/App.swift"}))
	expectEqual(t, tool.Summary(noCwd), "Read Sources/App.swift")
}

// MARK: other kinds

func TestUnknownKindIsNotText(t *testing.T) {
	expectNoText(t, "⏺ hi", "gemini")
}

// MARK: Round 8 fixes

// claudeDialog is Claude's permission dialog for an edit, drawn where the input box was.
const claudeDialog = `────────────────────────────────────────────────────────────────
 Edit file
 docs/api.md
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
  12 - old line
  12 + new line
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to make this edit to api.md?
 ❯ 1. Yes
   2. Yes, allow all edits during this session (shift+tab)
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend`

func TestClaudePermissionDialogIsNotText(t *testing.T) {
	// R8-10: the dialog's `❯ 1. Yes` isn't the prompt echo, so its footer isn't the turn's text.
	screen := "❯ Change the api doc.\n\n⏺ I'll update the doc.\n\n⏺ Update(docs/api.md)\n\n" + claudeDialog
	parsed := Parse(screen, "claude")
	expectEqual(t, parsed.Text, strPtr("I'll update the doc."))
	expectEqual(t, toolOf(t, parsed).Name, "Edit")

	// Echo scrolled off: still nothing from the dialog.
	parsed = Parse("⏺ Update(docs/api.md)\n\n"+claudeDialog, "claude")
	expectEqual(t, parsed.Text, (*string)(nil))
	expectEqual(t, toolOf(t, parsed).Name, "Edit")
}

func TestClaudeScrolledOffToolOutputIsNotText(t *testing.T) {
	// R8-22: the tool's header and `⎿` line are above the viewport; only its output is left.
	screen := `     b = '''the picker reopens
     wherever it was last. Browse
     root… (24s)
     (ctrl+b to run in background)

✶ Whirring… (23m 37s)
──────────────────────────────────
❯
──────────────────────────────────`
	expectEqual(t, Parse(screen, "claude"), Screen{})
}

func TestClaudeScrolledOffDiffIsNotAToolSummary(t *testing.T) {
	// R8-22: an Edit's diff lines, header scrolled off, must not become the tool's summary.
	screen := `      96  ### R8-12: SIGTER
          M ignored
      97  - Repro: start a
          bridge (temp home
  ⎿  Allowed by auto mode

✳ Pontificating… (17s)
──────────────────────────────────
❯
──────────────────────────────────`
	parsed := Parse(screen, "claude")
	expectEqual(t, parsed.Text, (*string)(nil))
	expectEqual(t, toolOf(t, parsed).Summary(noCwd), "Tool")

	// Prose after the orphan output still shows.
	screen = "     (ctrl+b to run in background)\n\n⏺ The tests pass.\n\n" + claudeBottom
	expectEqual(t, Parse(screen, "claude").Text, strPtr("The tests pass."))
}

func TestClaudeScrollOverlayIsNotText(t *testing.T) {
	// R8-23: fullscreen Claude's "scrolled up" hint lands on a content row.
	screen := `⏺ The loopback interface answered every ping, so
  the network stack is fine. 1 new message (click) ↓

✻ Crafting… (3s)
` + claudeBottom
	expectEqual(t, Parse(screen, "claude").Text, strPtr("The loopback interface answered every ping, so the network stack is fine."))
	screen = "⏺ Short answer.   Jump to bottom (click) ↓\n\n" + claudeBottom
	expectEqual(t, Parse(screen, "claude").Text, strPtr("Short answer."))
}

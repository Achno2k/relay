# go-live report (round 8)

Owns `bridge-go/internal/live` and `bridge-go/internal/approval`.

## What changed
- **`internal/live`**: port of LiveReplyParser, LiveReplyTracker and LiveReplyMonitor.
  - `Parse`/`Extract` (claude, codex, pi), `Tracker` (Offer/Landed/LandedBlocks/Stopped/Remove), `Monitor` (`NewMonitor(h, hub)`, `Update([]Agent)`, `Landed`, `Run(ctx)`).
  - Swift semantics kept: Character (grapheme) counts via `transcript.GraphemeCount`, ICU `\s`/`\S`/`\d` mapped to Unicode RE2 classes, `.whitespaces` trimming (Zs + tab).
- **`internal/approval`**: port of ApprovalParser, InputBox, and the Picker from ControlDrivers.
  - The Picker lives here because controls imports approval (agreed with go-drivers).
  - `Parse`, `Fallback`, `Step`, `ParsePicker`, `CleanPickerLabel`, `InputBoxContent`, `LastPromptLine`, `ClearKeys`.
- **Round 5 open item, "Update available!" banner bleeding into live text:**
  - The monitor now reads the visible screen with ANSI. It's still one `agent.read` per working agent per tick.
  - `live.ScreenText` blanks the cells drawn in the banner's colour on the banner's row, then produces exactly herdr's text read (CRLF→LF, trailing blanks trimmed).
  - The colour is learned from an intact "Update available", with Claude's dark-theme amber as the default.
  - This handles a banner that text partly overwrote, and cells interleaved with the reply. The old text-only strip missed those; it stays as a fallback.
- **Fixed bugs from `docs/qa/round8.md`:**
  - R8-10: a dialog footer ("Esc to cancel · Tab to amend") was sent as live text.
  - R8-14: wrapped approval labels were cut at the line break.
  - R8-22 (found live by me, filed by qa-bridge): tool output whose header had scrolled off leaked as text or as a junk "Tool" summary.
  - R8-23 (same): fullscreen Claude's "N new messages (click) ↓" / "Jump to bottom (click)" leaked into text.
  - R8-24: Claude's single-file label (`Reading notes.txt`, no count) went out as tool `Tool`; now `Read`/`Write`/`Edit` with the transcript's summary.
  - Found by go-parity: Claude's startup logo went out as live text on a fresh session (Swift has the same gap). The turn now starts below the logo.
    - The same report exposed a regression in my R8-10 fix: a user prompt starting "1. " was skipped as if it were a dialog's cursor. It's now skipped only while a dialog footer is on screen.
  - R8-11 helpers for go-server: `approval.InputFor(kind, screen, ansi)` and `ClearKeysFor`. pi's box is between its last two rules. codex's is the `›` composer; a composer whose text is all dim is the empty placeholder.
- **Race fixed, not filed:** a screen read still in flight when the agent stopped could reopen the preview after the stop had cleared it, and nothing would clear it again. The monitor now drops a read unless the agent is still working. This is the same lock `Update` uses.

## Tests: Swift → Go
| Swift suite | Swift | Go (same names) | Go extra |
|---|---|---|---|
| LiveReplyParserTests | 34 | 34 | 7 (R8-10, R8-22 ×2, R8-23, R8-24, logo, numbered prompt) |
| LiveReplyTrackerTests + LiveReplySequenceTests | 25 | 25 | – |
| LiveReplyMonitorTests | 2 | 2 | 2 (in-flight read after stop, ANSI read params) |
| – (banner) | – | – | 8 (`ScreenText`) |
| ApprovalParserTests | 18 | 18 | 3 (R8-14) |
| InputBoxTests | 3 | 3 | 3 (R8-11: pi, codex, others) |
| FuzzTests (approval, picker, input box) | 3 | 3 | – |
| **Total** | **85** | **85** | **23** (108 Go tests) |

Notes:
- The monitor tests drive `Monitor.Update` directly with a `herdrtest` fake. Swift went through AgentMonitor, which is go-server's.
- Two wire-shape tests compare against api.md key order instead of Swift's sorted keys.
- Fixtures were copied to `internal/approval/testdata`. The R8-11 screens are go-server's synthetic ones.

## How it was verified
- `gofmt -l .` is clean across the whole module. `go vet ./...` and `GOOS=linux go vet` pass for both packages. `go test -race` passes, and the approval fuzz tests pass with `-count=3`.
- Each fix's new tests fail on the pre-fix code and pass after.
- **Against real herdr (read-only):**
  - `ScreenText(ansi read)` matched herdr's text read line for line on 11 idle panes; the only differences were the blanked banner rows.
  - I ran the Go monitor for 25 s and then 40 s against working panes, and parsed 80+ captured screens.
  - Those runs found R8-22 and R8-23. After the fixes, a 40 s run showed no banner, dialog, overlay or tool-output text.
  - The captures stayed in the scratchpad and were not committed.
- `internal/service` builds against both packages and wires `Update`/`Landed`/`Run`.

## What's left
- qa-bridge verified R8-10 and R8-14 live. R8-22, R8-23 and R8-24 are waiting on its re-check.
- go-parity saw fewer `reply.live` frames from Go than from Swift (22 vs 75 in 40 s). It then ran controlled e2e turns (claude, pi and codex) and found nothing missing.
  - The claude turn's frames were identical to Swift's, in the same order.
  - Swift's extra frames were R8-22's per-second timer text, which Go no longer sends.
- The banner's default colour covers Claude's dark themes only. Other themes rely on an intact "Update available" somewhere on screen to learn the colour.
- Known Swift-vs-Go gaps, all in inputs herdr doesn't produce:
  - ICU `$` also matches before a trailing `\r`; RE2's doesn't.
  - Swift's `hasPrefix` treats a base character plus a combining mark as one Character; the Go port compares bytes.
- A command wrapped across several `⎿  $` lines only uses its first line for the live summary, the same as Swift. The tracker still clears it when the real toolCall lands.

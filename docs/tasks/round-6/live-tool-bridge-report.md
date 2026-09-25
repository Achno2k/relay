# live-tool-bridge report

## Repro (before the fix)

Recorded `/ws` frames next to 150 ms `agent.read --source visible` captures on the three test agents.

- **claude (w14:p2), `ping -c 8`:** `reply.live` went `"You chose: Left."` → `null` → `"You chose: Left."` → … about every 600 ms for the whole 10 s run. That text was the **previous turn's** reply. It also sent `"Running 1 shell command…"` as text once.
  - Cause 1: Claude Code (2.1.280) blinks a running tool's `⏺`. With the dot off, the parser's "last `⏺` block" was the previous turn's reply above the prompt.
  - Cause 2: the tool header is no longer `Bash(args)`. It's `⏺ Running 1 shell command…`, then a description (`⏺ Pinging localhost 8 times · 2s`) with `⎿  $ ping -c 8 127.0.0.1` under it. Only the second form was excluded.
  - Cause 3: `landed()` compared against all text blocks of the grouped message joined together. A toolCall-only upsert reset that, so a text that had already landed came back.
- **codex (w14:p5):**
  - Text flipped `text` → `null` (landed) → the same text → `null`, because the transcript has markdown (`` `relay-slow-ok` ``) and the screen doesn't.
  - The status line `1 background terminal running · /ps to view · /stop to close` leaked into the text.
  - This codex version draws no `• Running` cell. It shows `• Working`, then `• Ran …` once the command is done, when the toolCall has already landed.
- **pi (w14:p4):** no leak. Its `Working` spinner stays up the whole turn, so no text is sent. Its toolCall reaches the transcript when the tool starts.

## What changed

- **`docs/api.md` "Live reply"** (changed first): `reply.live` now carries `{agentId, seq, text, tool}`, with full state on every frame. `tool` is `{name, summary, state: "running"} | null`. The section also covers the stability rules, how each part clears, current-turn scoping and the per-kind tool rules.
- **`LiveReplyParser.swift`:** `parse(screen:kind:) -> LiveScreen {text, tool: LiveToolCall?}`. `extract` is kept as a text-only shim.
  - claude:
    - Only reads the current turn: after the last prompt echo, before the spinner and the input box.
    - Splits the turn into blocks. A tool block is one with a `⎿` line, a (wrapped) `Name(args)` call, or a grouped label (`Running 1 shell command…`, `Read 3 files`).
    - A block without its `⏺` still counts as a tool block, so the blink doesn't change the parse.
    - Arguments come from `⎿ $ cmd` or from the call's arguments. Display names are mapped to transcript names (`Update`→`Edit`, `Search`→`Grep`, …).
    - `·` spinner lines count as chrome.
  - codex: current turn only (between the last two `›` lines). Recognises `Running/Ran/Explored/Edited/Called/…` cells, and the background-terminal status line counts as chrome.
  - pi: tool boxes (`$ cmd`, `read/write/edit/ls path`, `grep`, `find`). A box is running while no `Took …` line has closed it. Its output is kept out of the text.
  - `LiveToolCall.summary(scrubber:)` goes through `ToolSummary` (codex: `Ran <cmd>`), so it matches the transcript's `toolCall.summary`.
- **`LiveReplyTracker.swift`:** a state machine per agent.
  - Text only grows, including a later scrolled window being merged onto the text already shown. A shorter or empty read keeps it. A different text needs two reads in a row that agree, and a text that was replaced or landed is never sent again this turn.
  - Tool: appears, changes or clears only after two reads in a row agree. A generic summary being refined goes out at once.
  - Landed matching works per text block, and compares letters and digits only, so markdown and curly quotes don't matter. A tool clears when a toolCall with the same summary lands, or when any toolCall lands after the tool first showed up. Once cleared, it stays cleared.
  - `stopped()` clears both and resets the turn. `seq` keeps counting.
- **`LiveReplyMonitor.swift`:** passes text and tool to the tracker. `landed` now passes the message's blocks.
- **`Models.swift`** (shared, one-line hook like round 5): `ServerEvent.replyLive(agentId:text:tool:seq:)` and the encoder writes `"tool": null|{…}`.

## Verification

- Tests: `swift test` passes, 247/247. The 61 live-reply tests passed 5 more runs in a row. New tests:
  - Parser: synthetic captures modelled on the real screens.
    - claude: grouped label, described block with `$ cmd`, blink on and off, prose then a tool, a wrapped call with and without output yet, path tools mapped to their names, `·` spinner, no leak from the previous turn, prose after a finished tool.
    - codex: running cell, background-terminal line, previous turn, edited cell.
    - pi: running bash box, finished box, read box.
  - Tracker: growth, keeping a shorter read, replacement only after two reads, no revert to a replaced text, a one-off bad read ignored, scroll merge, tool debounce and refine, the toolCall clear staying cleared, a wrapped command whose summary doesn't match, text and tool clearing independently, the wire JSON.
  - Sequences (parser and tracker together): a blinking tool run with the toolCall landing mid-run, prose → tool → prose, and alternating glitchy reads. Each asserts that no text or tool comes back and that no tool text or old-turn text is sent.
- Live, after the rebuild and kickstart (announced first):
  - claude ping: `tool: Ran a command` → 0.6 s later `Ran ping -c 8 127.0.0.1` → `tool: null` in the same ms as the toolCall upsert → text → `null` on landing. No flip, no old-turn text.
  - claude, 300 words → `sleep 4` → paragraph: text grew in 15 frames to 1878 chars while scrolling. The tool showed next to the prose that hadn't landed. Both cleared when Claude wrote the text and the tool_use, then the last paragraph showed and cleared.
  - codex `sleep 6 && echo`: text grew (`It printed \`` → `It printed relay-slow-ok …`) and cleared once on landing. No flip, no status line in the text.
  - pi ping: no frames at all, which is correct because the toolCall landed at tool start. The parser for pi's box is covered by unit tests only.
- Bridge rebuilt (release, includes tests-cleanup's UnixSocket fix `3ef9c59`) and kickstarted. `/health` ok, herdr connected. live-tool-ios was told the shape and that the bridge is live.

## What's left

- codex's running state never shows as a tool, because this codex version doesn't draw a `Running` cell (it's a background terminal). It's handled if a future version draws one.
- pi's prose isn't streamed live (spinner gate, same as round 5), even though pi does render it bit by bit. That was out of scope here.
- If a command wraps on claude's `⎿ $` line, the screen summary is only its first row. The row still clears when any toolCall lands after it showed up, but its `summary` won't match the transcript's until then.
- The same specific tool summary run twice in one turn: the second run is suppressed as already landed. That's harmless, since the transcript row shows it.
- `Models.swift` is a shared file. The change is a single case plus its encoder, and herd-native should know about it.

# live-tool-ios report

## What changed

App commit `670e203`.

- **RelayKit `Models.swift`**: `reply.live` decodes the optional `tool` (`LiveTool`: name, summary, state).
  A missing or malformed `tool` decodes as `nil` rather than dropping the frame, and `state` stays a
  string, so a new value still decodes.
- **`Stores/LiveReply.swift`** (new): `LiveReplies`, a plain value reducer holding per-agent live text
  plus a tool "run".
  - A run gets a placeholder row id when it starts. It remembers which tool calls the turn already had.
  - Matching a landed transcript call: same name + summary first (it can also claim a call that's
    still running from before the run, for when the transcript beats the screen). Otherwise the
    latest call of that name that's new since the run started. Earlier turns and calls another run
    already claimed never match.
  - On a match, the call id aliases to the placeholder id, so the real tool row renders under the same
    SwiftUI identity. It keeps its spot and its expanded state, and there's no duplicate.
  - Same name, not yet matched, new summary: the bridge's generic → refined summary ("Ran a command"
    → "Ran ping -c 8 127.0.0.1"). The run and row are updated in place.
  - Live text: tool lines are dropped (`⏺ Name(`, `⎿`, `└`, `• Ran/Running/Explored/Edited/Called`),
    along with their indented output. A frame that's a shorter prefix of the current text is ignored.
- **`AppStore`**:
  - Holds `live: LiveReplies`. It publishes only on a real change, so ~4 identical frames a second
    don't re-render the chat, and `seq` is `@ObservationIgnored`.
  - Re-matches on `message.upserted` and after a messages load.
  - Clears an agent's live state when it stops working (not on `blocked`, so a Bash waiting on approval
    keeps its row), and on `agent.closed`.
  - Clears all live state and resets `seq` on reconnect. Before this, a restarted bridge starts `seq`
    at 1 and the app silently dropped every frame until the old count was passed.
- **`ChatItems` / `ChatView`**: `ChatItem.withLive` (pure) adds live rows to the built transcript.
  - Live text becomes a `.live` row. The running tool joins the last tool group if the chat ends with
    one, otherwise it's a new group, which is where the transcript call will land.
  - Both go before pending prompt bubbles, so their order doesn't change when real rows replace them.
  - "Last row" (for "Running …" vs "Worked for") now ignores pending bubbles.
  - Live prose after a finished tool group now shows. Round 5 hid it, because the preview could be
    tool text back then.
- **`ChatRows`**: the bridge's unknown-tool name `Tool` reads "Running a tool…".
- **`Mock/MockBackend.swift`**: a prompt starting "slow bash" plays a live tool turn:
  - a generic summary, then the refined one;
  - old-bridge tool text in some frames;
  - the transcript call landing at ~5.3s, then `tool: null`, the result, then text.

## Verified

- Unit tests: 115/115 (`RelayTests`). The new `LiveToolTests.swift` covers:
  - start → land → clear;
  - refinement keeps the row;
  - name fallback;
  - never matching earlier calls;
  - transcript beating the screen;
  - a second identical call getting its own row;
  - agents staying independent;
  - tool-text filtering and no prefix step-back;
  - row ids unchanged across the swap, including before a pending prompt;
  - joining a running group;
  - live text ordered before the tool;
  - nothing live once stopped.

  Store level:
  - stop mid-tool clears;
  - approval mid-tool keeps the row;
  - switching agents mid-tool;
  - stale `seq`;
  - an unchanged frame (or one with only tool text added) doesn't trigger observation;
  - decoding with, without, `null` and malformed `tool`.
- Mock UI test `LiveToolUITests.testRunningToolShowsImmediatelyAndSwapsInPlace`:
  - "Running Bash…" shows within 4.5s (the transcript lands at ~5.3s; XCUITest tap overhead is ~2s);
  - the row is expanded before the swap, and for 5s across it the step count stays constant, there's
    never a second Running row, and no `Bash(`/`⎿` text appears;
  - it's still expanded after the result lands.
- The other mock UI suites still pass (19/19 including the new one).
- **Live, simulator, rebuilt bridge, w14:p2** (`LiveToolE2ETests`, `ping -c 8 127.0.0.1`): passed.
  `/ws` probe for the same run, seconds from probe start:
  ```
  32.14 upsert user
  35.02 live tool={Bash, "Ran a command"}
  35.31 live tool={Bash, "Ran ping -c 8 127.0.0.1"}
  38.99 upsert assistant [toolCall Bash "Ran ping -c 8 127.0.0.1"]
  38.99 live tool=None
  43.07 upsert assistant [toolCall, toolResult]
  44.31 upsert assistant [toolCall, toolResult, text]
  44.50 status done
  ```
  - The upsert arrives just before `tool: null`, so the alias is set before the placeholder goes. The
    row doesn't flash.
  - The recording (`live-tool.mov`, re-encoded to 590px wide) shows "Running Bash…" about 3s after
    send, steady in one place through the transcript landing at ~39s, then "Worked for 10s" and "done".
  - No command text shows at any point.

## Left open

- If a future bridge ever sent `tool: null` *before* the matching upsert, the row would disappear for
  a frame and come back under a new id. Today's bridge sends them in the other order by construction
  (it clears on the upsert). A short grace period for an unmatched run would cover it, if that ever
  changes.
- Codex and pi: per live-tool-bridge, codex draws no Running cell and pi's toolCall lands at tool
  start, so they normally send no `tool`. I didn't run the app live against them this round. The same
  code path handles them, and the filter drops codex `• Ran`/`└` lines.
- No device run (not needed for this change; I didn't ask for the iPhone).

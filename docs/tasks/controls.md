# Feature: agent controls (model, permission mode, effort, compact/clear)

Goal: from the phone, see and change a Claude agent's **model**, **permission mode** and **effort**, and run **/compact** and **/clear**. Test only on `w14:p2` (herd-e2e).

## Contract (herd-bridge writes it into api.md first, then both build)
- `Agent` gains optional fields (null for non-claude agents or when unknown):
  - `model`: the last assistant message's `message.model` in the transcript, e.g. `"claude-opus-5-5"`.
  - `permissionMode`: the last transcript line of `{"type":"permission-mode","permissionMode":...}`. Values: `default | acceptEdits | plan | auto | bypassPermissions`. Cross-check against the footer if the transcript lags.
  - `effort`: from the last assistant line's `effort`, or from the footer.
  - Changes emit `agent.updated`.
- `POST /agents/:id/control` with body `{"model":"sonnet"} | {"permissionMode":"plan"} | {"effort":"high"} | {"command":"compact"|"clear"}`. One key per call.
  - `409 agent_busy` while the agent is working, and `409 agent_blocked` while it's blocked.
  - Returns `202` once the change is confirmed on screen/transcript (timeout about 10 s, then `504`).
- Model aliases offered to the app: `opus`, `sonnet`, `haiku`, `fable`. Put `GET /controls` → `{"models":[{"id":"opus","label":"Opus 5.5"},...],"modes":[...],"efforts":[...]}` in the bridge, so the list lives in one place.

## Bridge (herd-bridge)
- Model: send the text `/model <alias>` + Enter into an empty input (reuse the clear-input logic). If Claude opens a picker instead, drive it. Confirm via the footer, or via the next assistant message's model.
- Mode: Claude cycles modes with Shift+Tab. Find herdr's key name for it (it isn't obvious in the schema; try `shift+tab`, `backtab`, `btab`, and escape sequences through `pane.send_input`). Press it until the footer shows the target mode, with at most one full cycle. If a mode isn't in this Claude's cycle (e.g. bypass), return `400 unsupported`.
- Effort: find how this Claude version sets it (`/effort`? a `/model` picker option?). Check `claude --help` and the `/help` output in a throwaway pane. If there's no reliable way, say so in the report and return `501`.
- `/compact` and `/clear` are sent as slash commands. `/clear` starts a new session id, so make sure the transcript locator follows herdr's new `agent_session` and the app gets fresh messages (emit `agent.updated`; maybe add a `sessionId` field so the app knows to refetch).
- Tests: synthetic transcripts/footers. Verify every control live on w14:p2, and put the results in the bridge report ("Controls" section).

## iOS (herd-ios)
- Title pill menu (ChatGPT's model picker style): **Model ▸** (checkmark on current), **Mode ▸**, **Effort ▸**, a divider, **Compact conversation**, **Clear conversation** (destructive, with confirmation). Disabled with a caption "Agent is working" while working or blocked.
- Under the title in the pill: a small secondary line with the model label, e.g. "Opus 5.5 · Auto".
- A mode chip on the composer's leading side when the mode isn't default (e.g. a "Plan" chip; tapping it opens the Mode menu). Match ChatGPT's tool chips.
- Optimistic UI: show a spinner in the menu row, and revert with an error toast on failure.
- Mock backend + fixtures updated, unit tests for decoding and the reducer, and a live UI test on w14:p2: switch to Sonnet, then plan mode, then back to auto, and assert the pill label follows.
- Screenshots in `docs/screenshots/controls-*.png`.

Same rules as before: commit only your own paths, and add a "Controls" section to your report.

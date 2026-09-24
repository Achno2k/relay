# Bridge report

`bridge/` builds and tests clean: `swift build` and `swift test` give 0 warnings, and all 44 tests pass on three consecutive runs.

## What works (verified live against this Mac's herdr, port 7979)
- `GET /health` returns `{"ok":true,"version":"0.1.0"}` without auth. Every other route returns 401 plus the JSON error body when the token is missing or wrong.
- `GET /agents` returns all 30 live agents (claude + pi) in about 4 ms, with no `/Users/` anywhere in the payload. `hasTranscript` resolves for 29 of 30. The one miss is a Claude session whose JSONL doesn't exist on disk.
- `GET /workspaces` returns agent counts per workspace.
- `GET /agents/:id/messages` works for real Claude and pi transcripts, including paging (`before`, `limit`, `hasMore`). A large transcript takes about 13 ms and is cached by size+mtime. Paths come out cwd-relative or as the last component only.
- `GET /agents/:id/approval` returns 204 on a non-blocked agent. Unknown agent, unknown `before` id and unknown route all return 404 with the JSON error body. Bad bodies return 400.
- `/ws?token=` sends `hello`, then live `message.upserted` events from the tailed transcripts. I watched my own transcript grow over the socket.
- herdr event stream: `events.subscribe` works. I confirmed the frame shape live (`{"event":"pane_created","data":{"pane":{...}}}`) with a throwaway split pane, then closed it.
- `herd pair` prints the QR code (black on white, half blocks) and the URL. The Tailscale IP was detected. A test decodes the QR with `CIDetector`. `herd token [--rotate]` and `herd install-launchd` work (install-launchd writes the plist and prints the `launchctl bootstrap` command without running it).
- Idle CPU is about 0.2%, RSS about 15 MB.

## Not verified live
- **Approval on a real blocked agent.** None was blocked during the run. The parser is tested against synthetic captures (new borderless prompt, old boxed prompt, AskUserQuestion) and against a fake herdr end to end.
- **`POST /agents` and `POST /agents/:id/prompt|keys` against real agents.** I didn't want to spawn tabs or type into someone else's agent. Prompt, keys and error mapping are tested against the fake herdr socket. `POST /agents` is untested beyond validation.
- **`agent.updated` / `agent.created` / `agent.closed` over the live socket.** No status changed during a 2-minute watch. The diff logic is tested with the fake herdr.

## How it works (the parts not spelled out in api.md)
- **herdr envelope.** Each request is one line, `{"id","method","params"}`, and the reply is `{"id","result"}` or `{"id","error":{code,message}}`. Every request gets its own short-lived socket connection, so a slow `agent.start` doesn't block other calls.
- **Status subscriptions.** herdr wants one `pane.agent_status_changed` subscription per `pane_id` (a bare subscription is rejected). The stream subscribes to pane.created/closed/updated/moved/exited/agent_detected plus one status subscription per agent pane, and reconnects whenever the set of agent panes changes.
- **Refresh.** Any herdr event, plus a 5 s timer as a safety net, triggers a fresh `agent.list`. The bridge diffs that against the last emitted snapshot and broadcasts the difference. Status never comes from an event payload. `agent.updated` fires on real changes only, not when `updatedAt` moves.
- **`updatedAt`.** herdr has no timestamp, so this is the transcript mtime, or the time the bridge saw `state_change_seq` change.
- **Transcript lookup.** The project dir is the cwd with every non-alphanumeric character replaced by `-`, which is how Claude actually names them (api.md mentions only `/` and `.`). It tries `cwd`, then `foreground_cwd`, then scans every project dir for `<sessionId>.jsonl`. pi uses `agent_session.value` as the path.
- **WebSocket pings.** Hummingbird pings every 30 s and closes clients that never pong. `URLSessionWebSocketTask` pongs automatically. My raw test client didn't, so it got dropped after about 2 min.
- **Merging.** Assistant lines keep merging across tool results until a real user message arrives. One user turn becomes one assistant bubble, as in `messages.json`.

## Deviations and additions (api.md unchanged)
- **Screen fallback.** When there's no transcript, messages is a single assistant message with id `screen:<paneId>`. Its one text block is the last 200 screen lines in a code fence, with paths scrubbed.
- **Blocked, but no numbered options on screen.** The response is an `Approval` with `options: []` and the last meaningful line as `question`, not a 204.
- **Extra error codes.** `agent_blocked` (409, prompting a blocked agent), `herdr_unavailable` (503), `herdr_timeout` (504), and herdr's own code with 502 for anything else.
- **Limits and flags.** `limit` is clamped to 1...500. `herd serve --local-only` skips the Tailscale bind.
- **Scrubbing text.** Scrubbing also applies to text and thinking blocks and to agent titles, not only `summary`/`input`/`preview`. URLs and relative paths are left alone.

## Round 1

All three items are fixed and checked live on port 7979. `swift test` passes 60 tests with 0 warnings.

1. **Stop no longer leaves the prompt behind.** When a stop comes early, Claude puts the interrupted prompt back in its input box, sometimes several lines of it. After `POST /keys ["esc"]`, the bridge reads the screen for up to 1.5 s and sends `ctrl+u` until the box between the two rules is empty. Each `ctrl+u` clears one line, and pressing it on an empty line deletes the line break. `POST /prompt` clears the box the same way before calling `agent.prompt`. A blocked agent is never cleared, because keys there would answer the dialog.
   - herdr's valid keys aren't in the schema. I found them by sending each candidate followed by a bogus key: herdr rejects the whole call without typing anything and names the first bad key. Valid: `ctrl+u`, `ctrl+a`, `ctrl+e`, `ctrl+k`, `up`, `down`, `enter`, `backspace`, `esc`. Invalid: `C-u`, `ctrl-u`, `home`, `end`.
   - Live on w14:p2: I sent a two-line prompt, stopped it after 1 s, and the box was empty. I then prompted "Reply with exactly: pong". The transcript's last user message is exactly that text.
   - A stop now takes about 1.5 to 2 s to return 202.
2. **Unnumbered cursor menus.** The folder-trust prompt now returns `question: "Trust this folder? <cwdName>"` with options `No, exit` (`["enter"]`) and `Yes, I trust this folder` (`["down","enter"]`). Other cursor menus use the nearest line with a `?` above them. Footer lines (`Enter to confirm · Esc to cancel`) and a multi-line input box are never read as menus.
   - Live: `POST /agents` into a fresh untrusted scratch dir, then `GET /approval`, then `POST /keys ["down","enter"]`. The agent went idle. I closed the throwaway workspace afterwards.
   - `POST /agents` now reports the requested `kind` when herdr hasn't classified the new agent yet. It used to return `unknown`.
3. **Multi-question progress.** `Approval.step = {index, count, title}` is 1-based, and `count` includes the Submit tab, as in the brief's example. The active tab only shows as a background highlight, so when a tab bar is on screen the bridge does one extra `visible` read with ANSI. If that fails, it takes the first `☐` tab. Documented in api.md.
   - Live, on a two-question prompt: Drink 1/3, then Time 2/3, then Submit 3/3.

**Free text, agreed with herd-ios through api.md.** The bridge sets `freeText: true` on "Type something.". Its keys are arrow moves from the cursor, not the row number. Pressing the number typed a stray digit: the answer was recorded as "3Carrier pigeon". `POST /agents/:id/text` sends the text with `pane.send_text`, waits 150 ms, then presses Enter.
- Live: `/keys ["down","down"]` then `/text "Green tea, please"`. The file Claude wrote contains exactly "Green tea, please".

## Controls

`GET /controls` and `POST /agents/:id/control` are live. `Agent` now carries `model`, `modelLabel`, `permissionMode`, `effort` and `sessionId`. `swift test` passes 80 tests with 0 warnings.

**Where the values come from**
- **model / effort:** the last 1 MB of the transcript, from assistant `message.model` / `effort` and from Claude's own `Set model to …` / `Set effort level to …` output. Right after `/clear`, when the new transcript is still empty, they come from the session banner ("Sonnet 5 with medium effort"). If neither is available, the last known value is used.
- **permissionMode:** Claude's footer ("⏸ plan mode on"). The transcript's `permission-mode` lines lag: one said `auto` while the footer showed plan.

**How each control works** (all run live on w14:p2)
- **Model** (`opus`, `sonnet`, `haiku`, `fable`): sends `/model <alias>` through herdr `agent.prompt`, into an empty input box. It's confirmed when "Set model to <label>" shows up in the transcript or on screen. Takes 0.7–2.3 s. No picker opened for a model name.
- **Effort** (`low` … `max`): `/effort <level>`, confirmed the same way. Takes about 0.7–1 s.
- **Mode:** Shift+Tab, herdr key `shift+tab`, one press per call: two presses in one call only moved one step. The bridge reads the footer after each press and stops at the target. It returns `400 unsupported` after one full cycle and ends back where it started. On this Claude the cycle is auto → manual (`default`) → acceptEdits → plan, so `bypassPermissions` returns 400. Takes 0.2–0.5 s.
- **/compact:** confirmed by a `compact_boundary` transcript line or "Compacted" on screen. Took 10–22 s live; the bridge waits up to 120 s.
- **/clear:** confirmed when herdr reports a new `agent_session` id. This took 0.7 s in one run and over 10 s in another, so the wait is 30 s. The locator and tailer follow the new file, and messages come back empty for the new session.
- A plan-mode agent that finishes a plan sits at "ready to execute?" (`blocked`). Controls return `409 agent_blocked` until it's answered.
- Stress run: 3 rounds of opus → high → sonnet → medium → plan → auto, back to back, gave 18 of 18 × 202 and left `settings.json` byte-identical.

**Things I had to work around**
- **Saved defaults.** Claude's `/model` and `/effort` save the choice as the default for new sessions in `~/.claude/settings.json` (top-level `model`, and `modelSettings.<model>.effortLevel`). The bridge snapshots the file before the command and writes the exact bytes back afterwards, in place so it keeps its 0600 permissions. The running session keeps its new model; I checked that the next reply was on the new model.
- **Your settings.** My first manual tests, before the guard existed, set `"model": "sonnet"` and added `modelSettings.claude-sonnet-5.effortLevel: "low"`. I reverted both by hand, with `"model"` set to `"opus"`. I don't know what the value was before, so check it's what you want.
- **Lost Enter.** Right after a `/model`, Claude sometimes drops the Enter while it re-renders, and the command sits in the input box. Typing the text with `pane.send_text` and then pressing Enter lost commands this way. So the bridge now uses `agent.prompt`, checks the last `❯` line, and presses Enter again if the command is still there.
- **Late transcript writes.** Claude sometimes writes `/model` output to the transcript a few seconds after the screen shows it. A confirmed value is held for up to 15 s so the response and `agent.updated` don't show the old value.
- **Transcript parser.** It now drops `isCompactSummary` lines, the summary `/compact` injects as a user message. The plain `/compact` line still shows as a user message.

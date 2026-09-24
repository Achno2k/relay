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

**Controls fix (reported by herd-ios: `/model` returned 504 although the switch applied)**
- **Cause:** on a long, cached conversation, `/model` first asks "Switch model? … ❯ 1. Yes, switch to Sonnet 5 / 2. No, go back". herdr doesn't report the agent as `blocked` there, so the bridge waited 10 s and left the dialog open. I reproduced it live: essay streaming, stop, `/model sonnet`.
- **Fix:**
  - When a numbered menu replaces the input box during a control, the bridge presses its "Yes…" option once.
  - A menu without a "Yes" option gets Esc and `400 unsupported`.
  - On timeout, the bridge presses Esc so no dialog is left open.
- **Related fixes:**
  - The "command still in the input box" check ignored indented dialog lines (`   ❯ 1. Yes…`) and kept pressing Enter. A visible dialog now counts as the command being taken.
  - "Kept model as Opus 5.5" (the dialog answered No) now returns `409 control_refused` instead of timing out. Only output written after the command counts: my first version matched an old "Kept" line still on screen and refused two switches that had worked.
- **Tests:**
  - A fixture copied from the real dialog screen, plus fake-Claude tests for the Yes path and the refusal. 83 tests pass.
  - Live on 7878: 12 of 12 back-to-back controls, and the stop-then-switch sequence, all return 202.
  - The dialog itself didn't come up again in my live runs; it seems to need the cached conversation to sit for a while. So the Yes-press is only tested against the fixture copied from the real screen.
- **Follow-up (reported by herd-ios):** a false refusal, 'Claude asked "⎿ Interrupted…"'.
  - Cause: the dialog check accepted any numbered list on screen whenever the input box was briefly missing during a redraw, so a list in the agent's output looked like a dialog.
  - Fix: a dialog now needs Claude's `❯` cursor on a numbered option in the last 8 non-empty lines, and only those lines are parsed.
  - Tests: a fixture of a numbered list on screen during a redraw. Live: stream a numbered list, stop, switch model, three times; all returned 202.

## Round 2 (bridge: attachments and GET /machine)

The contract went into api.md first (`6d6daa4`: Attachments and Machine sections, plus fixtures `attachment.json`, `machine.json` and `messages-attachments.json`). `swift test` passes 99 tests with 0 warnings.

**What's built**
- `POST /agents/:id/attachments`:
  - Takes the raw body, `Content-Type`, and `X-Filename` percent-encoded. Names are sanitised.
  - Files are stored as `~/.herd/uploads/<pane>/<id>-<name>` with mode 0600, and the call returns `201 Attachment`.
  - Errors: `413 too_large` over 20 MB (Hummingbird's `collect(upTo:)` limit), `400` for an empty body, `404` for an unknown agent.
- `GET /agents/:id/attachments/:attachmentId` looks the id up across all agents and serves the bytes with the MIME type from the extension. It returns `404` once the file has expired.
- `POST /agents/:id/prompt` accepts `attachments` (at most 10; unknown ids give 400). `text` can be empty when there are attachments. The bridge adds `\n\nAttached files: <abs paths>` to the text Claude receives.
- `UploadCleaner` deletes uploads older than 7 days at startup and then daily, along with their `sent.jsonl` entries.
- `GET /machine` returns `{id, name, kind, model, os}`:
  - `id` is the hardware UUID (`gethostuuid`), `name` is the computer name, `model` is `sysctl hw.model`.
  - `kind` is laptop when there's an internal battery (IOKit power sources). This Mac reports `Mac17,2`, which has no "Book" in it, so the name-based rule in the brief would call it a desktop.

**What Claude Code does with the marker** (seen live on w14:p2)
- Images: Claude Code spots the image path in the pasted prompt and embeds the image inline as `[Image #N]`. So Claude sees the image without a file-read permission.
- PDFs: the path stays in the text, and Claude opens it with its Read tool (auto mode, no prompt).
- The stored user message doesn't keep our text as sent. It has `<pasted_content id=…>` tags, hard-wrapped lines, and an `Attached files:` line left empty or broken over two lines. The marker alone isn't enough, so the bridge logs each send in `sent.jsonl` and matches it back with a whitespace- and tag-insensitive fingerprint (within 10 minutes of the send). History then shows the attachment blocks and the original, unwrapped text.
- `<pasted_content>` tags are now stripped from every user message. Before, multi-line prompts would have shown them in the app.

**Live results** (isolated bridge on 7979 with the real `~/.herd`, agent w14:p2)
- A 480×200 PNG with the word "HERD": the reply was `HERD`. History showed `[attachment herd.png, text]`, the file served back byte-identical as `image/png`, and no paths appeared anywhere in the JSON.
- PDF ("The secret code word is PELICAN-42.") plus the PNG: the reply was `PELICAN-42 / HERD`. History showed `[attachment codeword.pdf, attachment herd.png, text]`.
- The PDF alone with no text: the reply quoted the code word, and history showed `[attachment codeword.pdf]` with no text block.

**Not covered**
- In the default permission mode, Claude may ask before reading a PDF outside the project folder. That shows up as a normal approval.
- An upload directory whose path has spaces would break the marker. `~/.herd/uploads` normally has none.

## Round 3: pi and codex controls

### Findings from live throwaway panes (herd-e2e: pi at w14:p4, codex at w14:p5)

**pi 0.85.1**
- **Model:** `/model <provider>/<id>` switches directly, with no picker. It prints `Model: <id>` and the footer's right side becomes `(<provider>) <id> • <thinking>`. A bare `/model` opens a picker; Ctrl+L and Ctrl+P also select or cycle models.
- **Thinking:** `/thinking <level>` works directly. It prints `Thinking level: <level>`, and the footer's `• <level>` updates. Levels are `off minimal low medium high xhigh max`; `shift+tab` cycles them.
- **Defaults untouched:** neither command touches `~/.pi/agent/settings.json`. pi only saves a default on Ctrl+S in the picker.
- **Model switch resets thinking:** switching model sets thinking to that model's default (seen: low → high).
- **Session file:** records `model_change {provider, modelId}` and `thinking_level_change {thinkingLevel}`. pi only writes the file after the first message, so for a fresh session the footer is the only source.
- **Model list:** `pi --list-models` prints a table (provider, model, context, max-out, thinking yes/no, images): 57 models here. `settings.json` has `defaultProvider`/`defaultModel` (+ optional `enabledModels` for Ctrl+P scoping).
- **/new:** starts a new session. herdr's `agent_session.value` (a path) switches immediately. The model is kept and thinking resets to the default.
- **/compact:** exists (`/compact [instructions]`). The session file gets a `compaction` entry.
- **No permission mode.**

**codex-cli 0.156.1**
- **Trust dialog on start:** a new folder gets "Trust this folder?" with `› 1. Trust and continue`. Pressing the digit only moves the cursor here; Enter confirms. herdr shows the agent as `idle`, not `blocked`, during this dialog.
- **Model:** there's no direct form. `/model gpt-5.5` is sent to the model as an ordinary chat message. `/model` opens "Select Model and Effort" (numbered rows `› 2. GPT-5.6-Terra  <description>`, wrapping cursor), then "Select Reasoning Level for <Model>" (Low / Medium (default) / High / Extra high / More reasoning… → Max, Ultra).
- **Saving vs session-only:** on the effort step, `enter` saves the default to config.toml and `s` applies "for this session only". The bridge always uses `s`. Confirmation: `• Model changed to <slug> <effort> for this session only`.
- **Footer:** `<Model> <effort> · <cwd> …`. It shows the display name (`GPT-5.6-Terra high`), or the slug for models not in the catalogue (`gpt-5.6-sol high`). Plan mode appends `Plan mode`.
- **Model list:** `codex debug models` returns the catalogue: `slug`, `display_name`, `visibility` (list/hide), `supported_reasoning_levels`, `default_reasoning_level`. The picker shows the `visibility: list` models. The configured `gpt-5.6-sol` isn't in the catalogue, so it can't be re-selected from the picker. With this ChatGPT login it also fails on `/compact` ("model is not supported when using Codex with a ChatGPT account").
- **Rollout file:** `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`. `turn_context` has `model`, `effort`, `approval_policy`, `approvals_reviewer`, `sandbox_policy.type` and `collaboration_mode.mode`. It's written per turn only, so the footer is fresher.
- **Permissions (mode):** `/permissions` opens "Update Model Permissions": Ask for approval / Approve for me / Full Access, with the current one marked "(current)". Choosing one prints `• Permission selection requested: <label>`. In `turn_context`, "Approve for me" is `approvals_reviewer: auto_review`, and Full Access is `sandbox_policy.type: danger-full-access`. `shift+tab` separately toggles Plan mode, shown in the footer. It isn't exposed in this round.
- **/new:** asks "Where should the new conversation run?" (Current checkout / New worktree). After Current checkout it prints `To continue this session, run codex resume…`, and the model goes back to the config default. herdr keeps the old session id until the next message.
- **/compact:** exists (`/compact`). Errors show as `■ Error …`.
- **Side effect:** codex's trust answer adds a `[projects."<cwd>"] trust_level` entry to `~/.codex/config.toml`. I'll remove the entry for the scratch folder when done.

### What's built
- **`ControlDriver` protocol** with `ClaudeDriver` (the existing Claude code, moved behind it), `PiDriver` and `CodexDriver`. Each driver reads its agent's values, lists its capabilities, and applies and confirms changes. Shared pieces:
  - `Picker`: parses numbered pickers with `›`/`❯` cursors, strips descriptions and "(default)".
  - `drivePicker`: moves one key at a time, re-reading the screen until the cursor moves, then presses the confirm key.
  - `waitForScreen`: only new output counts, and a new `■ Error…` fails the call with `502 control_failed`.
- **`GET /agents/:id/controls`**: per-agent lists and `supports`. `POST /agents/:id/control` checks the request against that agent's lists: `400 bad_request` for a value not in them, `400 unsupported` for a control the kind doesn't have.
- **`ModelCatalogs`**: `pi --list-models`, `codex debug models` and pi's `settings.json`, cached for 10 minutes, plus a lookup for codex rollout files. Tests swap in a fake command runner.
- **Agent fields**: `Agent.model/modelLabel/effort` are now filled for pi (footer, then session file) and codex (footer, then `turn_context`). codex `permissionMode` uses a mode the bridge just set until a newer `turn_context` shows up.
- **Settle wait**: codex reports `working` for a moment after some picker actions, so a control re-checks for about 1.5 s before answering `409 agent_busy`.
- **Tests**: 115 pass, 0 warnings. That includes fake pi and codex UIs (a state machine with both pickers, the Advanced Reasoning submenu, and enter=save-default vs s=session) and fixtures of real footers, pickers, `--list-models` and rollouts.

### Live results (bridge on 7979, real panes)
- **pi (w14:p4):**
  - `model` openai-codex/gpt-5.6-sol, openai-codex/gpt-5.5 and openai-codex/gpt-5.6-terra each switched in 0.56–2.1 s.
  - `effort` low, minimal and high each switched in about 0.56 s.
  - A bad model returned `400 bad_request`, and `permissionMode` returned `400 unsupported`.
  - `clear` (`/new`) returned 202 in 0.56 s with a new sessionId, and the model went back to pi's default.
  - `compact` on a two-message session returned `502 control_failed` "Compaction failed: Nothing to compact (session too small)". A successful pi compaction isn't live-tested; it's covered by the session-file check.
  - `~/.pi/agent/settings.json` stayed byte-identical.
- **codex (w14:p5):**
  - 8/8 model/effort changes returned 202 in 1.0–3.3 s, including Max and Ultra through "More reasoning…" → Advanced Reasoning. Switching model keeps the effort when the new model supports it; otherwise it uses that model's default.
  - `permissionMode` ask, fullAccess and approveForMe each took 0.8–1.3 s. No confirmation dialog appeared for Full Access here.
  - `compact` returned 202 in 22.5 s, confirmed by "• Context compacted" (it also started a new session id). `clear` returned 202 in about 1 s via "Current checkout".
  - `~/.codex/config.toml` unchanged.
- **Claude regression (w14:p2):** 6/6 controls returned 202 in 0.16–1.0 s.

### Bugs found live and fixed
- **Stale errors:** the first codex run reported `502` from an old "■ Error running remote compact task" line still on screen. Error and confirmation checks now count only new output.
- **Saved default by accident:** `drivePicker(title: "")` never matched, because Swift's `"x".contains("")` is false. So the Advanced Reasoning submenu stayed open, and the next command's Enter committed Max as codex's saved default. Now:
  - an empty title matches any picker;
  - a failed picker flow presses Esc until no picker is left;
  - the Max/Ultra step waits for the submenu itself.

  I restored `~/.codex/config.toml` from my snapshot straight away. My snapshot was taken before codex's folder trust ran, so the scratch folder's trust entry was removed along with the accidental change.
- **Wrong confirmation for compact:** codex doesn't keep the typed `/compact` on screen, so compact is now confirmed by a new "Context compacted" line.

### Not covered
- codex's Plan-mode toggle (`shift+tab`) isn't exposed. It's a separate "collaboration mode", not a permission mode.
- A model that exists only in codex's config (e.g. `gpt-5.6-sol`) can't be selected in the picker again, and its effort can't be changed. Both return `400 unsupported` with the reason.
- On codex, pressing a digit in a picker only moves the cursor, and Enter confirms. That matters for approvals: the approval keys `["N"]` may need an extra `enter` on codex. I haven't changed the approval parser for codex yet.

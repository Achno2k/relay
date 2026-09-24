# Relay bridge API (v1 contract)

The contract between `bridge/` (a Swift daemon on the Mac) and `ios/` (the SwiftUI app).
Both sides build against `docs/fixtures/*.json`. Change this file first, then the code.

## Transport
- Base URL: `http://<tailscale-ip>:7878`. The bridge binds to the Tailscale IP and 127.0.0.1 only.
- Auth: `Authorization: Bearer <token>` on every route except `GET /health`.
  - `/ws` takes the token as `?token=` because iOS `URLSessionWebSocketTask` can't set headers reliably.
- Token lives in `~/.relay/token` (0600) and is created on first run. A bridge that finds only the pre-rename `~/.herd` moves it to `~/.relay` first, so the token (and pairing) survives the rename.
- Timestamps are ISO 8601 with an offset. JSON keys are camelCase.
- Errors: `{"error": {"code": "not_found", "message": "..."}}` with the matching HTTP status.

## Pairing
- `relay pair` prints a QR code for `relay://pair?url=<base>&token=<token>` and prints the same string as text. The app also accepts the old `herd://pair?…` links.

## Models

```jsonc
Workspace { "id": "w13", "name": "forge", "agentCount": 2 }

Agent {
  "id": "w13:p1",                // herdr pane id (stable while the pane lives)
  "name": "swift-herdr",         // herdr agent name, may be null
  "kind": "claude",              // claude | pi | codex | ...
  "title": "Native Swift iOS app for herdr agents", // terminal_title_stripped
  "workspaceId": "w13",
  "workspaceName": "forge",
  "cwdName": "forge",            // last path component only; full paths never cross the wire
  "status": "idle",              // idle | working | blocked | done | unknown
  "hasTranscript": true,         // a transcript file was found
  "transcriptState": "ready",    // ready | pending | unsupported (see below)
  "updatedAt": "2026-09-23T13:04:01+00:00",
  // Controls. Optional: null for non-claude agents or when unknown.
  "model": "claude-opus-5-5",    // full model id of the last assistant message
  "modelLabel": "Opus 5.5",      // display name; the bridge owns the mapping
  "permissionMode": "auto",      // default | acceptEdits | plan | auto | bypassPermissions | dontAsk
  "effort": "medium",            // low | medium | high | xhigh | max
  "sessionId": "1aa5901c-..."    // current transcript session; changes after /clear => refetch messages
}

Message {
  "id": "1aa5901c-...",          // transcript uuid (or a synthetic id for the fallback)
  "role": "user" | "assistant",
  "createdAt": "...",
  "blocks": [Block]
}

Block (tagged on "type"):
  { "type": "text",       "text": "markdown" }
  { "type": "thinking",   "text": "..." }
  { "type": "toolCall",   "id": "toolu_..", "name": "Edit", "summary": "Edited api.md", "input": "<truncated json string>" }
  { "type": "toolResult", "toolCallId": "toolu_..", "isError": false, "preview": "<first ~400 chars>" }
  { "type": "attachment", "id": "9f2c4e1a7b3d5f60", "name": "screenshot.jpg", "kind": "image" }   // user messages only; see Attachments

Approval {
  "agentId": "w13:p1",
  "question": "Do you want to make this edit to api.md?",
  "options": [ { "label": "Yes", "keys": ["1"] }, { "label": "Yes, don't ask again", "keys": ["2"] }, { "label": "No", "keys": ["esc"] } ],
  "step": { "index": 2, "count": 3, "title": "Focus" }   // optional, see below
}

ApprovalStep {                    // present only for multi-question prompts (tab bar `←  ☒ Delivery  ☐ Focus  ✔ Submit  →`)
  "index": 2,                     // 1-based position of the active tab
  "count": 3,                     // number of tabs, including the final Submit (review) tab
  "title": "Focus"                // optional; the tab's label ("Submit" on the review screen)
}

ApprovalOption {
  "label": "Type something.",
  "keys": ["3"],
  "freeText": true                // optional, default false. Choosing it opens a text input in the agent's menu
}                                 // (Claude's "Type something." row). The app sends `keys`, then the typed answer via POST /agents/:id/text.
```
- Keys differ by agent kind. Send each option's `keys` exactly as given:
  - Claude: a digit selects a row (`["1"]`); a trailing "No … (esc)" is `["esc"]`.
  - codex: a digit only moves the cursor, so options are arrow moves from the `›` row plus `"enter"` (`["enter"]`, `["down","enter"]`); "No, and tell Codex … (esc)" is `["esc"]`. The question is codex's heading plus the command, e.g. "Would you like to run the following command?\n`curl -sI https://example.com | head -1`". Shortcut hints like `(y)`/`(p)` are removed from labels.
  - pi has no approval prompts (it has no permission gates by design).
- Free-text options: the bridge sets `freeText: true` on Claude's "Type something." row. Clients also treat a label of exactly `Type something.` as free text, so older bridges still work.
  - Its `keys` are arrow moves from the menu cursor to that row (e.g. `["down","down"]`), not its number: pressing the number also types the digit into the field. If the cursor is already on it, `keys` is `["up","down"]`. `keys` is never empty.
  - Send `POST /keys` with those keys, then `POST /text` right away. No delay is needed between them.
- Menu shapes: numbered menus give `["N"]` per option (a trailing "No … (esc)" gives `["esc"]`). Unnumbered cursor menus (Claude's folder-trust prompt: `❯ No, exit` / `Yes, I trust this folder`) give arrow moves relative to the `❯` line plus `"enter"`, e.g. `["down","enter"]`. The trust prompt's question is `Trust this folder? <cwdName>`.

`Agent.transcriptState`:
- `ready`: a transcript was found. `/messages` returns it.
- `pending`: the kind has a transcript parser (claude, pi, codex), but the file doesn't exist yet, e.g. a new agent before its first message. `/messages` returns `{"messages":[],"hasMore":false}`. The startup screen is never shown as chat; a startup dialog (trust prompt etc.) arrives as `status: "blocked"` + `/approval`. The state becomes `ready`, and `agent.updated` fires, once the first message creates the transcript.
- `unsupported`: no parser for this kind. `/messages` is one synthetic assistant message (`screen:<paneId>`) holding the last screen lines in a code block.
- Bridges older than this field don't send it. Clients can treat a missing value as `hasTranscript ? ready : (claude/pi/codex ? pending : unsupported)`.

Transcript rules (Claude JSONL at `~/.claude/projects/<cwd with / and . replaced by ->/<sessionId>.jsonl`):
- Keep only lines with `type` of `user` or `assistant` and `isSidechain == false`.
- Consecutive assistant lines (one line per content block) merge into one assistant Message.
- A `user` line whose content is only `tool_result` attaches to the preceding assistant Message as a `toolResult` block. It is not a user bubble.
- User string content that starts with `<command-`, `<local-command`, or `<system-reminder` is dropped. So are `isMeta: true` and `isCompactSummary: true` (the summary `/compact` injects).
- Absolute paths in `summary`, `input`, and `preview` are made cwd-relative. Anything else absolute keeps only its last component.
- pi: `agent_session.kind == "path"`. Parse that JSONL with the same Message model (best effort).
- codex: the rollout `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`. The session id comes from herdr's `agent_session`; before herdr has one, the bridge uses the newest rollout (last 2 days) whose `session_meta.cwd` is the agent's cwd and that was created after the bridge first saw the agent, so a new codex agent never borrows another codex agent's rollout in the same folder (it stays `pending` until its first message). `hasTranscript` is true once a rollout is found.
  - Only `event_msg` → `item_completed` items are read; that's what codex's own UI shows. The raw `response_item`s repeat them with injected context and are ignored.
  - `UserMessage` → user message (the attachment rules apply). `AgentMessage` → `text`, `Reasoning` summary → `thinking`.
  - Consecutive assistant items merge into one assistant Message, which grows while codex works (`message.upserted`).
  - Tool items become `toolCall` + `toolResult`:
    - `CommandExecution` → `Shell` "Ran <cmd>" (from `["/bin/zsh","-lc",cmd]`); the preview is the output. `isError` when the exit code is non-zero or the status is `failed`/`declined`.
    - `FileChange` → `Edit` "Edited <file>" (or "Edited N files"); the preview is the diff.
    - `McpToolCall` → `<server>.<tool>` "Called …".
    - web search → `WebSearch`; `ImageView` → `ViewImage`.
  - Paths are scrubbed the same way as for Claude.

## REST
| Method | Path | Body | Returns |
|---|---|---|---|
| GET | /health | – | `{"ok":true,"name":"relay","version":"0.1.0"}` |
| GET | /workspaces | – | `[Workspace]` |
| GET | /agents | – | `[Agent]` (all live agents; the app groups them by workspace) |
| GET | /agents/:id | – | `Agent` |
| GET | /machine | – | `Machine` (the Mac this bridge runs on; see Machine) |
| GET | /agents/:id/messages?before=<msgId>&limit=50 | – | `{"messages":[Message], "hasMore": bool}`, oldest first |
| POST | /agents/:id/attachments | raw file bytes; headers `Content-Type`, `X-Filename` | `201 Attachment`. See Attachments. |
| GET | /agents/:id/attachments/:attachmentId | – | the file bytes with their `Content-Type`. `404` once expired. |
| POST | /agents/:id/prompt | `{"text": "...", "attachments": ["<attachmentId>", …]}` (`attachments` optional; `text` may be empty when there are attachments) | `202 {}` (calls herdr `agent.prompt`). The bridge first empties Claude's input box, so a prompt never glues onto leftover text. `409 agent_blocked` if the agent is at a dialog. |
| POST | /agents/:id/keys | `{"keys": ["esc"]}` | `202 {}` (stop = `["esc"]`). After a stop the bridge clears the prompt Claude puts back in its input box, so it may take up to ~2 s to answer. Other keys are sent as is. Key names are herdr's (`esc`, `enter`, `up`, `down`, `ctrl+u`, digits, …). |
| POST | /agents/:id/text | `{"text": "...", "submit": true}` | `202 {}`. Types `text` literally into the pane as it is now (herdr `pane.send_text`), with no clearing and no Esc, then presses Enter if `submit` (default true). Used for free-text approval answers. `400 bad_request` if `text` is empty. |
| GET | /agents/:id/approval | – | `Approval` or `204` when not blocked |
| GET | /controls | – | `Controls`: Claude's list only, kept for older apps. Use `/agents/:id/controls`. |
| GET | /controls?kind=claude\|codex\|pi | – | `AgentControls` for a kind with no agent yet (the New chat sheet), with `defaultModel`/`defaultEffort` and, for pi and codex, `effortsByModel`. `400 unsupported` for other kinds. |
| GET | /agents/:id/controls | – | `AgentControls` for that agent's kind (see below) |
| POST | /agents/:id/control | exactly one of `{"model":"sonnet"}`, `{"permissionMode":"plan"}`, `{"effort":"high"}`, `{"command":"compact"}`, `{"command":"clear"}` | `202 Agent` (fresh, with the new value) once the change shows on screen or in the transcript. Errors below. |
| POST | /agents | `{"workspaceId":"w13","kind":"claude","name":"optional","model":"optional","effort":"optional"}` | `201 Agent`: a new tab in the workspace (cwd = the workspace's first pane's cwd), the agent started with that model and effort. See "Starting an agent". |

`:id` is URL-encoded (`w13%3Ap1`).

## Attachments

```jsonc
Attachment {
  "id": "9f2c4e1a7b3d5f60",      // 16 lowercase hex characters
  "name": "screenshot.jpg",      // sanitised file name (the name used everywhere, including in history)
  "kind": "image",               // image | pdf | file
  "size": 183422                 // bytes
}
```
- Upload: `POST /agents/:id/attachments`.
  - The body is the raw file. `Content-Type` is the file's MIME type. `X-Filename` is the original name, percent-encoded UTF-8.
  - Limit 20 MB per file: `413 too_large`. An empty body gives `400 bad_request`.
  - The app converts and downscales images itself (JPEG, ≤2048 px). The bridge stores bytes as they are.
- Name sanitising: characters outside `A-Z a-z 0-9 . _ -` become `-`, runs of `-` collapse, and the stem is cut to 80 characters (extension kept). An empty result becomes `file`.
- `kind` comes from the extension: image types → `image`, `.pdf` → `pdf`, anything else → `file`.
- Storage (the Mac only): `~/.relay/uploads/<paneId with non-alphanumerics as _>/<id>-<name>`, mode 0600. Deleted after 7 days; the bridge checks at startup and daily. After that, `GET …/attachments/:id` returns `404 not_found`, and history shows the block without its file.
- Send: `POST /agents/:id/prompt` with `"attachments": [id, …]`, at most 10.
  - Unknown ids give `400 bad_request`.
  - The bridge appends one final line to the text Claude receives: `Attached files: <abs path> <abs path> …`, separated from the text by a blank line. Claude opens images and PDFs from those paths itself.
- History:
  - Claude Code rewrites the prompt before storing it. It embeds images inline and replaces their path with `[Image #N]`, wraps multi-line pastes in `<pasted_content>` tags, and hard-wraps long lines.
  - So the bridge also logs what it sent (`~/.relay/uploads/sent.jsonl`: time, pane, original text, attachment ids; pruned with the uploads). It matches user messages against that log, ignoring whitespace, tags and placeholders.
  - A matched message becomes one `attachment` block per file (id, name, kind; never the path), then the original text as a `text` block if there was any.
  - Without a log entry, a marker line with intact paths is parsed the same way.
  - Tool calls that read these files show only the file name, as usual.
- Every user message has `<pasted_content>` tags removed.
- `GET /agents/:id/attachments/:attachmentId` looks the id up across all agents' uploads, so history keeps working if the agent's pane id changes.

## Machine

```jsonc
Machine {
  "id": "c0ffee00-…",            // stable per Mac (hardware UUID); lets the app list several machines later
  "name": "Dev's MacBook Pro",   // scutil --get ComputerName
  "kind": "laptop",              // laptop | desktop (laptop = has an internal battery; newer ids like Mac17,2 don't say "Book")
  "model": "Mac15,9",            // sysctl hw.model
  "os": "macOS 26.4"
}
```
- One bridge serves one machine. The app keeps its own list of machines (bridges) and calls `GET /machine` on each.

## Controls

```jsonc
Controls {
  "models":  [ { "id": "opus", "label": "Opus 5.5" }, { "id": "sonnet", "label": "Sonnet 5" },
               { "id": "haiku", "label": "Haiku 4.5" }, { "id": "fable", "label": "Fable 5.1" } ],
  "modes":   [ { "id": "default", "label": "Default" }, { "id": "acceptEdits", "label": "Accept edits" },
               { "id": "plan", "label": "Plan" }, { "id": "auto", "label": "Auto" },
               { "id": "bypassPermissions", "label": "Bypass permissions" } ],
  "efforts": [ { "id": "low", "label": "Low" }, { "id": "medium", "label": "Medium" }, { "id": "high", "label": "High" },
               { "id": "xhigh", "label": "Extra high" }, { "id": "max", "label": "Max" } ]
}
```
### Per-agent controls (Claude, pi, codex)

```jsonc
AgentControls {
  "models":  [ { "id": "openai-codex/gpt-5.6-sol", "label": "gpt-5.6-sol" }, … ],  // the agent's own list
  "efforts": [ { "id": "off", "label": "Off" }, … ],                              // for the agent's current model
  "modes":   [ { "id": "ask", "label": "Ask for approval" }, … ],                  // [] when the kind has none
  "supports": { "model": true, "effort": true, "mode": false, "compact": true, "clear": true },
  // Optional (always sent by GET /controls?kind=…; may be absent on /agents/:id/controls):
  "defaultModel": "openai-codex/gpt-5.6-sol",   // the agent's saved default (what it starts with if you pick nothing)
  "defaultEffort": "high",
  "effortsByModel": { "openai-codex/gpt-5.6-sol": [ { "id": "off", "label": "Off" }, … ], … }  // pi and codex; for claude all models share `efforts`
}
```
| kind | models (`id` → what `Agent.model` holds) | efforts | modes | compact / clear |
|---|---|---|---|---|
| claude | aliases `opus` `sonnet` `haiku` `fable`; `Agent.model` is the full id (`claude-sonnet-5`), so match by substring | low medium high xhigh max | default acceptEdits plan auto bypassPermissions (the Shift+Tab cycle) | `/compact`, `/clear` |
| pi | `pi --list-models` as `provider/id`. The saved default and scoped (`enabledModels`) come first, then the rest by provider. `Agent.model` is exactly one of these ids. | per model, using pi's own rule on `~/.pi/agent/models-store.json`. Examples: claude-fable-5 has minimal…max (no `off`); claude-haiku-4-5 has off…high; a model without thinking has only `off`. | none (`supports.mode: false`) | `/compact`, `/new` |
| codex | `codex debug models` entries with `visibility: list` (`id` = slug, `label` = display name), plus the current model if it's not in the catalogue. `Agent.model` is the slug. | the current model's `supported_reasoning_levels` (e.g. low medium high xhigh max ultra) | `ask` Ask for approval, `approveForMe` Approve for me, `fullAccess` Full Access | `/compact`, `/new` (current checkout) |

- `POST /agents/:id/control` takes the same body for every kind: `model` and `effort` use ids from that agent's lists, `permissionMode` uses an id from `modes`, and `command` is `compact` or `clear`.
  - A value that isn't in the agent's list gives `400 bad_request`. A control the kind doesn't support gives `400 unsupported` with a message saying why.
- Changes apply to the running session only. The bridge never saves them as the agent's default:
  - Claude: the settings file is restored.
  - pi: `/model` and `/thinking` don't save anything.
  - codex: the bridge picks "for this session only".
- On pi, switching model resets thinking to that model's default, and `Agent.effort` reflects that. The `efforts` list changes with the model, so refetch `GET /agents/:id/controls` after a model switch.
- If pi still rejects a level ("Unknown thinking level … Available levels: …"), the bridge returns `400 unsupported` with pi's list at once, and leaves that level out for this model from then on.
- codex has no direct command, so the bridge drives its `/model` picker. It picks the model row, then the effort row (Max/Ultra sit under "More reasoning…"), then presses `s`, re-reading the screen after every key.
  - A model outside the catalogue can't be re-selected there, and changing only the effort needs the current model to be in the list. Otherwise it's `400 unsupported`.
- `Agent.model`, `modelLabel` and `effort` are filled for pi and codex from the footer, falling back to the session file. `permissionMode` is filled for codex from the last `turn_context`, or from what the bridge just set.
- More errors and side effects:
  - `502 control_failed`: the agent itself reported an error, quoted in `message`. Examples: pi's "Compaction failed: Nothing to compact (session too small)", codex's "■ Error running remote compact task: …".
  - `/compact` on codex starts a new session id, so refetch messages.
  - `clear` (`/new`) puts pi and codex back on their saved default model and effort.
- Fixtures: `docs/fixtures/agent-controls-claude.json`, `agent-controls-pi.json`, `agent-controls-codex.json`, and `agents-multi.json` (a pi and a codex agent with these fields).

### Starting an agent (`POST /agents`)

- Body:
  - `workspaceId` and `kind` are required. `name` is optional.
  - `model` and `effort` are optional, and use ids from `GET /controls?kind=<kind>`. `effort` must be in `effortsByModel[model]` when that exists; with no `model`, it's checked against the default model's efforts.
  - A value outside the list gives `400 bad_request`.
- The bridge passes them as the agent's own launch flags, so nothing is typed into the agent and no saved default changes:
  - claude: `--model <alias> --effort <level>`
  - codex: `-m <slug> -c model_reasoning_effort="<effort>"`
  - pi: `--model <provider/id>:<thinking>`, or `--thinking <level>` alone
- The `201 Agent` carries the chosen `model`, `modelLabel` and `effort`. Anything left out is the agent's saved default (`defaultModel`/`defaultEffort`).
- A startup dialog (e.g. folder trust) still comes back as `status: "blocked"` plus `/approval`. The agent then starts with the chosen model/effort once it's answered.
- `prompt` (optional) is still accepted and sent once the agent is ready, but the app no longer uses it. The first message goes through `POST /agents/:id/prompt` like any other.
- `cwdFromPane` (optional, for tests): start in that pane's cwd instead of the workspace's first pane's.

### Claude details

- `Agent.model` is a full id (`claude-sonnet-5`). Match it to the menu with `modelLabel`, or check whether the id contains the alias (`"sonnet"`).
- `POST /agents/:id/control` (Claude agents only):
  - `model`: an alias from `models`. `permissionMode` and `effort`: an `id` from their lists.
  - `command`: `compact` (summarise the context and keep the session) or `clear` (new session: `sessionId` changes, so refetch messages).
  - Returns `202` with the updated `Agent` once the change is confirmed. The bridge waits up to ~10 s; `/compact` can take up to ~90 s.
  - Errors:
    - `400 bad_request`: zero or several keys, or an unknown value.
    - `400 unsupported`: this agent can't reach that value, e.g. `bypassPermissions` isn't in its Shift+Tab cycle, or it isn't a Claude agent.
    - `409 agent_busy`: the agent is working. `409 agent_blocked`: it's at a dialog.
    - `409 control_refused`: Claude declined the change, e.g. it printed "Kept model as Opus 5.5" because its "Switch model?" confirmation was answered No.
    - `501 not_implemented`: this Claude version has no reliable way to set that control.
    - `504 control_timeout`: sent, but the change never showed up.
- Every change to `model`, `modelLabel`, `permissionMode`, `effort` or `sessionId` also emits `agent.updated`.
- Scope:
  - A switch applies to that agent's session only. Claude's `/model` and `/effort` also save the choice as the default for new sessions, but the bridge puts `~/.claude/settings.json` back afterwards.
  - Claude calls the default mode "manual" in its footer; the API calls it `default`.
  - On a long, cached conversation Claude asks "Switch model? … 1. Yes, switch to Sonnet 5 / 2. No, go back". The bridge answers Yes itself. If the change times out, the bridge presses Esc to close any dialog it opened.
  - Claude Code 2.1.280 cycles auto → default → acceptEdits → plan. `bypassPermissions` is only in the cycle when Claude was started with it.

## WebSocket `/ws?token=...`
Server → client only, one JSON object per frame:
```jsonc
{ "type": "hello" }
{ "type": "agent.updated", "agent": Agent }        // status/title change, re-derived from herdr, not from the event payload
{ "type": "agent.created", "agent": Agent }
{ "type": "agent.closed",  "agentId": "w13:p1" }
{ "type": "message.upserted", "agentId": "w13:p1", "message": Message }  // new or grown message (the last assistant message grows while working)
```
- The client refetches `/agents` and the open chat's messages on (re)connect. The socket carries deltas only; nothing is replayed.
- v1 tails every live agent that has a transcript (a handful of files, cheap).

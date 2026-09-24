# Herd bridge API (v1 contract)

The contract between `bridge/` (a Swift daemon on the Mac) and `ios/` (the SwiftUI app).
Both sides build against `docs/fixtures/*.json`. Change this file first, then the code.

## Transport
- Base URL: `http://<tailscale-ip>:7878`. The bridge binds to the Tailscale IP and 127.0.0.1 only.
- Auth: `Authorization: Bearer <token>` on every route except `GET /health`.
  - `/ws` takes the token as `?token=` because iOS `URLSessionWebSocketTask` can't set headers reliably.
- Token lives in `~/.herd/token` (0600) and is created on first run.
- Timestamps are ISO 8601 with an offset. JSON keys are camelCase.
- Errors: `{"error": {"code": "not_found", "message": "..."}}` with the matching HTTP status.

## Pairing
- `herd pair` prints a QR code for `herd://pair?url=<base>&token=<token>` and prints the same string as text.

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
  "hasTranscript": true,         // false => messages are a screen-read fallback
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
- Free-text options: the bridge sets `freeText: true` on Claude's "Type something." row. Clients also treat a label of exactly `Type something.` as free text, so older bridges still work.
  - Its `keys` are arrow moves from the menu cursor to that row (e.g. `["down","down"]`), not its number: pressing the number also types the digit into the field. If the cursor is already on it, `keys` is `["up","down"]`. `keys` is never empty.
  - Send `POST /keys` with those keys, then `POST /text` right away. No delay is needed between them.
- Menu shapes: numbered menus give `["N"]` per option (a trailing "No … (esc)" gives `["esc"]`). Unnumbered cursor menus (Claude's folder-trust prompt: `❯ No, exit` / `Yes, I trust this folder`) give arrow moves relative to the `❯` line plus `"enter"`, e.g. `["down","enter"]`. The trust prompt's question is `Trust this folder? <cwdName>`.

Transcript rules (Claude JSONL at `~/.claude/projects/<cwd with / and . replaced by ->/<sessionId>.jsonl`):
- Keep only lines with `type` of `user` or `assistant` and `isSidechain == false`.
- Consecutive assistant lines (one line per content block) merge into one assistant Message.
- A `user` line whose content is only `tool_result` attaches to the preceding assistant Message as a `toolResult` block. It is not a user bubble.
- User string content that starts with `<command-`, `<local-command`, or `<system-reminder` is dropped. So are `isMeta: true` and `isCompactSummary: true` (the summary `/compact` injects).
- Absolute paths in `summary`, `input`, and `preview` are made cwd-relative. Anything else absolute keeps only its last component.
- pi: `agent_session.kind == "path"`. Parse that JSONL with the same Message model (best effort).

## REST
| Method | Path | Body | Returns |
|---|---|---|---|
| GET | /health | – | `{"ok":true,"version":"0.1.0"}` |
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
| GET | /controls | – | `Controls` (the choices the app offers; see below) |
| POST | /agents/:id/control | exactly one of `{"model":"sonnet"}`, `{"permissionMode":"plan"}`, `{"effort":"high"}`, `{"command":"compact"}`, `{"command":"clear"}` | `202 Agent` (fresh, with the new value) once the change shows on screen or in the transcript. Errors below. |
| POST | /agents | `{"workspaceId":"w13","kind":"claude","name":"optional","prompt":"optional"}` | `201 Agent` (new tab in the workspace, cwd = workspace's first pane cwd, `agent.start`, then optional prompt) |

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
- Storage (the Mac only): `~/.herd/uploads/<paneId with non-alphanumerics as _>/<id>-<name>`, mode 0600. Deleted after 7 days; the bridge checks at startup and daily. After that, `GET …/attachments/:id` returns `404 not_found`, and history shows the block without its file.
- Send: `POST /agents/:id/prompt` with `"attachments": [id, …]`, at most 10.
  - Unknown ids give `400 bad_request`.
  - The bridge appends one final line to the text Claude receives: `Attached files: <abs path> <abs path> …`, separated from the text by a blank line. Claude opens images and PDFs from those paths itself.
- History:
  - Claude Code rewrites the prompt before storing it. It embeds images inline and replaces their path with `[Image #N]`, wraps multi-line pastes in `<pasted_content>` tags, and hard-wraps long lines.
  - So the bridge also logs what it sent (`~/.herd/uploads/sent.jsonl`: time, pane, original text, attachment ids; pruned with the uploads). It matches user messages against that log, ignoring whitespace, tags and placeholders.
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

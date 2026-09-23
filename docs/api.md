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
  "updatedAt": "2026-09-23T13:04:01+00:00"
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
- User string content that starts with `<command-`, `<local-command`, or `<system-reminder` is dropped. So is `isMeta: true`.
- Absolute paths in `summary`, `input`, and `preview` are made cwd-relative. Anything else absolute keeps only its last component.
- pi: `agent_session.kind == "path"`. Parse that JSONL with the same Message model (best effort).

## REST
| Method | Path | Body | Returns |
|---|---|---|---|
| GET | /health | – | `{"ok":true,"version":"0.1.0"}` |
| GET | /workspaces | – | `[Workspace]` |
| GET | /agents | – | `[Agent]` (all live agents; the app groups them by workspace) |
| GET | /agents/:id | – | `Agent` |
| GET | /agents/:id/messages?before=<msgId>&limit=50 | – | `{"messages":[Message], "hasMore": bool}`, oldest first |
| POST | /agents/:id/prompt | `{"text": "..."}` | `202 {}` (calls herdr `agent.prompt`). The bridge first empties Claude's input box, so a prompt never glues onto leftover text. `409 agent_blocked` if the agent is at a dialog. |
| POST | /agents/:id/keys | `{"keys": ["esc"]}` | `202 {}` (stop = `["esc"]`). After a stop the bridge clears the prompt Claude puts back in its input box, so it may take up to ~2 s to answer. Other keys are sent as is. Key names are herdr's (`esc`, `enter`, `up`, `down`, `ctrl+u`, digits, …). |
| POST | /agents/:id/text | `{"text": "...", "submit": true}` | `202 {}`. Types `text` literally into the pane as it is now (herdr `pane.send_text`), with no clearing and no Esc, then presses Enter if `submit` (default true). Used for free-text approval answers. `400 bad_request` if `text` is empty. |
| GET | /agents/:id/approval | – | `Approval` or `204` when not blocked |
| POST | /agents | `{"workspaceId":"w13","kind":"claude","name":"optional","prompt":"optional"}` | `201 Agent` (new tab in the workspace, cwd = workspace's first pane cwd, `agent.start`, then optional prompt) |

`:id` is URL-encoded (`w13%3Ap1`).

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

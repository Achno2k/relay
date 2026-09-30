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
- Round 5 hardening: every JSON request body is capped at 2 MB (`413 too_large`); the herdr socket dying mid-request answers `503 herdr_unavailable` within a couple of seconds, never hangs; `/health` also reports `herdr` (`connected`/`unavailable`) and `uptimeSeconds`.

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
  { "type": "toolCall",   "id": "toolu_..", "name": "Edit", "summary": "Edited api.md", "input": "<truncated json string>",
    "path": "docs/api.md",       // optional (round 10); see "Tool call files and edits"
    "edit": ToolEdit,            // optional (round 10)
    "plan": "# Plan\n..." }      // optional (round 10), ExitPlanMode only; see "Plans"
  { "type": "toolResult", "toolCallId": "toolu_..", "isError": false, "preview": "<first ~400 chars>" }
  { "type": "attachment", "id": "9f2c4e1a7b3d5f60", "name": "screenshot.jpg", "kind": "image" }   // user messages only; see Attachments

Approval {
  "agentId": "w13:p1",
  "question": "Do you want to make this edit to api.md?",
  "options": [ { "label": "Yes", "keys": ["1"] }, { "label": "Yes, don't ask again", "keys": ["2"] }, { "label": "No", "keys": ["esc"] } ],
  "step": { "index": 2, "count": 3, "title": "Focus" },  // optional, see below
  "plan": "# Plan\n..."          // optional (round 10): the markdown plan, only on Claude's ExitPlanMode prompt; see "Plans"
}

ToolEdit {                        // round 10; what a file-changing toolCall did, enough to draw a diff
  "kind": "edit",                 // edit | write | diff
  "changes": [ { "old": "foo()", "new": "bar()", "replaceAll": false } ],  // kind edit: Edit = 1, MultiEdit = N, in order
  "content": "...",               // kind write: the full content written
  "diff": "--- a/x\n+++ b/x\n@@ …", // kind diff: a unified diff (codex FileChange; may cover several files)
  "truncated": false              // true if any string above was cut (see caps)
}

FileContent {                     // round 10; GET /agents/:id/file
  "path": "Sources/App.swift",    // cwd-relative, as requested (normalised)
  "content": "import SwiftUI\n…",  // UTF-8 text, at most 1 MB
  "size": 48213,                  // bytes on disk
  "truncated": false,             // true when size > 1 MB and content is the first 1 MB (cut on a line/UTF-8 boundary)
  "language": "swift"             // optional, from the extension (swift, go, ts, tsx, js, py, md, json, yaml, sh, …)
}

ApprovalStep {                    // present only for multi-question prompts (tab bar `←  ☒ Delivery  ☐ Focus  ✔ Submit  →`)
  "index": 2,                     // 1-based position of the active tab
  "count": 3,                     // number of tabs, including the final Submit (review) tab
  "title": "Focus"                // optional; the tab's label ("Submit" on the review screen)
}

UsageProvider {
  "id": "claude",                 // claude | codex  ("codex" covers pi too; see Usage)
  "label": "Claude",              // "Claude" | "Codex / pi"
  "plan": "Max",                  // subscription tier, or null when unknown
  "windows": [UsageWindow],
  "updatedAt": "2026-09-23T13:04:01+00:00",  // when this snapshot was fetched, regardless of staleness
  "source": "claude -p /usage",   // what the bridge asked, for debugging
  "stale": false,                 // true if updatedAt is old or the last fetch failed
  "unavailableReason": null       // human string when stale/empty, e.g. "codex app-server didn't respond"
}

UsageWindow {
  "id": "session",                // source-defined key: session | weekly_all | primary | secondary
  "label": "Session",             // "Session" | "This week" | "5-hour" | "Weekly" | …
  "usedPercent": 11.0,            // 0-100, or null if this window has no data yet
  "windowMinutes": null,          // the window's length, when the source reports one (codex does; Claude doesn't)
  "resetsAt": "2026-09-24T23:00:00+00:00"  // or null
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
- Claude Code also injects lines the user never typed. User string content that starts with `<task-notification`, `<bash-stdout>` or `<bash-stderr>` is dropped. `<bash-input>cmd</bash-input>` (shell mode) becomes a user `text` block `! cmd`, as the user typed it.
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
| GET | /health | – | `{"ok":true,"name":"relay","version":"0.1.0","herdr":"connected","uptimeSeconds":42}` |
| GET | /workspaces | – | `[Workspace]` |
| GET | /agents | – | `[Agent]` (all live agents; the app groups them by workspace) |
| GET | /agents/:id | – | `Agent` |
| GET | /machine | – | `Machine` (the Mac this bridge runs on; see Machine) |
| GET | /agents/:id/messages?before=<msgId>&limit=50 | – | `{"messages":[Message], "hasMore": bool}`, oldest first |
| POST | /agents/:id/attachments | raw file bytes; headers `Content-Type`, `X-Filename` | `201 Attachment`. See Attachments. |
| GET | /agents/:id/attachments/:attachmentId | – | the file bytes with their `Content-Type`. `404` once expired. |
| POST | /agents/:id/prompt | `{"text": "...", "attachments": ["<attachmentId>", …]}` (`attachments` optional; `text` may be empty when there are attachments) | `202 {}` (calls herdr `agent.prompt`). The bridge first empties the agent's input box (every kind: claude, pi, codex), so a prompt never glues onto leftover text. Slash-command controls do the same. `409 agent_blocked` if the agent is at a dialog. |
| POST | /agents/:id/keys | `{"keys": ["esc"]}` | `202 {}` (stop = `["esc"]`). After a stop the bridge clears the prompt Claude puts back in its input box, so it may take up to ~2 s to answer. Other keys are sent as is. Key names are herdr's (`esc`, `enter`, `up`, `down`, `ctrl+u`, digits, …). |
| POST | /agents/:id/text | `{"text": "...", "submit": true}` | `202 {}`. Types `text` literally into the pane as it is now (herdr `pane.send_text`), with no clearing and no Esc, then presses Enter if `submit` (default true). Used for free-text approval answers. `400 bad_request` if `text` is empty. |
| GET | /agents/:id/approval | – | `Approval` or `204` when not blocked |
| GET | /agents/:id/file?path=<cwd-relative path> | – | `FileContent` for text; raw bytes with their `Content-Type` for images. See Files. |
| GET | /controls | – | `Controls`: Claude's list only, kept for older apps. Use `/agents/:id/controls`. |
| GET | /controls?kind=claude\|codex\|pi | – | `AgentControls` for a kind with no agent yet (the New chat sheet), with `defaultModel`/`defaultEffort` (each left out when the agent has no saved default, e.g. no `model` in `~/.claude/settings.json`; the app then shows no default) and, for pi and codex, `effortsByModel`. `400 unsupported` for other kinds. |
| GET | /agents/:id/controls | – | `AgentControls` for that agent's kind (see below) |
| POST | /agents/:id/control | exactly one of `{"model":"sonnet"}`, `{"permissionMode":"plan"}`, `{"effort":"high"}`, `{"command":"compact"}`, `{"command":"clear"}` | `202 Agent` (fresh, with the new value) once the change shows on screen or in the transcript. Errors below. |
| POST | /agents | `{"workspaceId":"w13","kind":"claude","name":"optional","model":"optional","effort":"optional"}` | `201 Agent`: a new tab in the workspace (cwd = the workspace's first pane's cwd), the agent started with that model and effort. See "Starting an agent". |
| GET | /usage | – | `{"providers":[UsageProvider]}`. Always the cache; never triggers a live fetch. See Usage. |
| POST | /usage/refresh | – | `202 {}`: asks the bridge to refresh now. `429 rate_limited` if called again within 15 s of the last refresh (manual or automatic). |

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

## Files

Round 10. The app shows a file the agent read or changed ("tap files in chat"). Only files inside the agent's project; there is no directory listing.

- `GET /agents/:id/file?path=<cwd-relative path>`, `path` percent-encoded. Works for every kind (it only needs the pane's cwd from herdr).
- Path rules:
  - `path` is relative to the agent's cwd, e.g. a toolCall's `path`. Empty, absolute (`/…`, `~…`) or containing a NUL byte: `400 bad_request`.
  - The bridge joins it to the cwd and resolves it (realpath, every symlink followed). The result must be the cwd itself or below it (also realpath'd). Otherwise `403 forbidden`, whether or not the file exists, so `..` and symlinks can't escape.
  - Missing file: `404 not_found`. A directory: `400 bad_request`. An agent with no known cwd: `404 not_found`.
- Text: the file is text when its first 8 KB has no NUL byte and is valid UTF-8 (an incomplete sequence at the cut is fine). The answer is `200 FileContent`, `Content-Type: application/json`.
  - Cap 1 MB: a larger file gives its first 1 MB, cut back to the last newline (or a UTF-8 boundary), with `truncated: true`. `size` is always the size on disk.
  - `content` is the file as it is on disk, not scrubbed: it's the user's own project file. The path in the response is still cwd-relative only.
  - `language` is a lowercase hint from the extension (or the name: `Dockerfile`, `Makefile`), left out when unknown. The app uses it for highlighting only.
- Images (`png`, `jpg`/`jpeg`, `gif`, `webp`, `heic`, `bmp`, `tiff`, by extension, checked against the file's magic bytes): `200` with the raw bytes and their `Content-Type` (e.g. `image/png`), `Cache-Control: no-store`. Cap 20 MB: `413 too_large`. `svg` is text. An image extension whose bytes don't match is treated like any other file (text if it's text, else `415`).
- Anything else that isn't text: `415 unsupported`.
- Clients tell the two `200` shapes apart by `Content-Type` (starts with `application/json` vs `image/`).

## Tool call files and edits

Round 10. Extra, optional `toolCall` fields so the app can open a file and draw a diff. `input` stays as it was (capped at 1000 characters), so it can't be used for diffs.

- `path`: the file the tool acted on, cwd-relative, scrubbed like `summary`.
  - Set only when the file is inside the agent's cwd (lexically, after scrubbing: not absolute, no leading `..`). So a tappable row always has a `path` that `GET /agents/:id/file` accepts, unless the file was deleted since.
  - claude: `Read`, `Write`, `Edit`, `MultiEdit` (`file_path`), `NotebookEdit` (`notebook_path`).
  - pi: `read`, `write`, `edit` (`path`).
  - codex: `Edit` from a `FileChange` with exactly one file.
  - Left out for everything else (Bash, Grep, Glob, …), for multi-file codex edits, and for files outside the cwd (e.g. Claude's plan file in `~/.claude/plans/`).
- `edit` (`ToolEdit`): what a file-changing tool did. Present even when `path` isn't (e.g. outside the cwd).
  - claude `Edit`: `kind: "edit"`, one change from `old_string` / `new_string` / `replace_all`.
  - claude `MultiEdit`: `kind: "edit"`, one change per `edits[]` entry, in order.
  - claude `Write`: `kind: "write"`, `content` from `content`. It doesn't say whether the file existed before; the app draws it as all-added.
  - pi `edit`: `kind: "edit"` from `oldText` / `newText` (or its `edits[]`). pi `write`: `kind: "write"`.
  - codex `FileChange`: `kind: "diff"`, one unified diff for all its files. Each file's hunk gets `--- a/<path>` / `+++ b/<path>` headers (cwd-relative) if codex's `unified_diff` lacks them. A single added file with only `content` is `kind: "write"`.
  - A failed call (its `toolResult` has `isError: true`) still carries `edit`; the app shows it as not applied.
- Every string in `edit` is scrubbed like `input` (absolute paths made cwd-relative). So the diff can differ from the file on disk in exactly those paths.
- Caps: each string at most 64 KB, and all of one `edit`'s strings at most 256 KB together. Cuts land on a line boundary (a string with no newline in reach is cut on a UTF-8 boundary), later strings are cut or emptied first, and `truncated` becomes `true`. The app can offer the whole file through `GET /agents/:id/file`.
- Live `reply.live` tools never carry `path` or `edit`.
- Fixture: `docs/fixtures/messages-edits.json`.

## Plans

Round 10. Claude's plan mode ends with the `ExitPlanMode` tool and an approval prompt ("Claude has written up a plan and is ready to execute. Would you like to proceed?"). The plan itself is only in the tool input or in the plan file Claude wrote, so the bridge adds it.

- The plan, in this order:
  1. `ExitPlanMode`'s `input.plan`, when it's a non-empty string.
  2. Otherwise the plan file: the last `Write`/`Edit`/`MultiEdit` in this session (before that `ExitPlanMode`) whose `file_path` is under `~/.claude/plans/`, read from disk. If the file is gone, a `Write`'s own `content` is used.
  3. Otherwise no plan (the field is left out).
- It's markdown, scrubbed like a `text` block, capped at 256 KB (cut on a line boundary).
- `Approval.plan`: set while the agent is blocked on that prompt. The bridge finds the transcript's last `ExitPlanMode` `toolCall`. If the transcript doesn't have it yet (Claude writes it a moment late), it uses step 2 for the latest plan file of the session.
- `toolCall.plan`: every `ExitPlanMode` `toolCall` in history carries it, so the plan stays visible after approval (or rejection). For step 2 the file is read when the message is built, so an older `ExitPlanMode` shows the file as it is now if Claude later rewrote the same file.
- pi and codex have no plan approvals; codex's own plan updates are not covered here.
- Fixtures: `docs/fixtures/approval-plan.json`, and the `ExitPlanMode` call in `messages-edits.json`.

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
- The comments above are macOS. On Linux:
  - `id`: `/etc/machine-id` (32 hex characters, no dashes).
  - `name`: the hostname.
  - `kind`: `laptop` if `/sys/class/power_supply/BAT*` exists, else `desktop`.
  - `model`: `/sys/class/dmi/id/product_name`, falling back to `uname -m`.
  - `os`: `PRETTY_NAME` from `/etc/os-release` (e.g. `Ubuntu 24.04.1 LTS`), else `Linux`.

## Multiple machines

Round 9. The app drives agents on several machines, e.g. the Mac plus a Linux VM. No route or payload changes.

**One bridge per machine.**
- Each machine runs its own `relay serve` next to its own herdr, with its own token in its own `~/.relay`.
- The app talks to each bridge directly over Tailscale. No bridge proxies another, and bridges don't know about each other.
- A remote bridge is the same binary. Linux setup: `docs/remote-setup.md`.

**Pairings.** The app keeps a list of pairings, keyed by `GET /machine` `id`.
- Entry: `machineId`, `url`, `token`, plus app-only fields (local label, last `Machine`, last `/health`).
- Adding one: scan or paste `relay://pair?url=…&token=…` (unchanged link), then call `GET /machine` with that url and token.
  - `200`: if the `id` is already in the list, **replace** that entry's `url` and `token` (re-pair after a new IP or `relay token --rotate`), keep its label and all per-agent state. Otherwise append.
  - Unreachable or `401`: save nothing, show the error.
- A stored pairing whose bridge answers `401` on any route is `needsRePair`. Only that machine; the others carry on.
- A stored pairing whose `/machine` answers a different `id` than the one it's stored under is also `needsRePair` (the URL now reaches another bridge). The app never merges two machines' data.
- Removing a pairing drops its token and every per-agent key for that machine. Nothing is sent to the bridge.
- Migrating the pre-round-9 single pairing: call `/machine` to learn its id. If the bridge is offline, keep it under a provisional id and re-key it on the first successful `/machine`.

**Ids are per machine.**
- Agent ids (`w13:p1`), workspace ids, message ids, attachment ids and live `seq` are only unique within one bridge. Two machines can both have `w1:p1`.
- The app keys everything by machine id + id: selection, seen, archive, drafts, pending sends, controls caches, live replies, expansion, deep links.
- On the wire, each bridge only ever sees its own raw ids. Never send one machine's id to another bridge.
- Machine ids match `[A-Za-z0-9._-]{1,64}` (a Mac hardware UUID, a Linux `/etc/machine-id`, or the hostname fallback). So `|` and `/` never occur in one and are safe separators in a namespaced string key.
- An attachment uploaded to machine A can only be used in a prompt to an agent on machine A.
- `POST /agents` goes to the bridge of the chosen machine, with a `workspaceId` from that machine's `/workspaces`.

**Each bridge is independent.**
- One WebSocket per machine, with its own reconnect and backoff. One machine offline or slow never blocks another's REST calls, WS or UI.
- Per-machine status is re-derived, never stored as truth:
  - `connecting`: first attempt or reconnecting after a drop;
  - `online`: `/ws` connected;
  - `offline`: unreachable (timeouts, connection refused, no Tailscale route);
  - `needsRePair`: `401`, or a changed `/machine` id.
- `online` with `/health` `herdr: "unavailable"` means the bridge is up but its herdr is not; the app shows the machine with no live agents rather than as offline.
- An offline machine's last known agents stay listed (greyed), from the app's own cache; they aren't live and can't be prompted.
- `/usage` is per bridge. Bridges don't report an account identity, so "same subscription on two machines" is a guess the app makes (same provider `id` + `plan`).

**Dev and test overrides (test-only, don't set these in real installs).**
- `RELAY_MACHINE_ID` / `RELAY_MACHINE_NAME`: when set and non-empty, `GET /machine` reports these as `id` / `name` instead of the real ones. Lets a second bridge on the same Mac show up as another machine. A `RELAY_MACHINE_ID` that doesn't match `[A-Za-z0-9._-]{1,64}` makes `relay serve` exit 78.
- `scripts/second-bridge.sh`: starts a Go bridge on port 7881 with a temp `RELAY_HOME`, `RELAY_MACHINE_ID=test-vm`, `RELAY_MACHINE_NAME="Test VM"`, the same herdr, and prints a pair link for `http://127.0.0.1:7881`.

**`relay pair --url <base>`.** Puts `<base>` (e.g. `http://100.101.102.103:7878` or a MagicDNS name) in the link verbatim, for when the auto-detected Tailscale IPv4 is wrong (NAT, several tailnet IPs). `--host`/`--port` still work; `--url` wins over both.

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
  // Optional (sent by GET /controls?kind=… when the agent has a saved default; may be absent on /agents/:id/controls):
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
{ "type": "reply.live", "agentId": "w13:p1", "seq": 4, "text": "The answer starts here" | null,
  "tool": { "name": "Bash", "summary": "Ran ping -c 8 127.0.0.1", "state": "running" } | null }  // live preview; see Live reply
{ "type": "usage.updated", "provider": UsageProvider }   // one provider's snapshot changed; see Usage
```
- The client refetches `/agents` and the open chat's messages on (re)connect. The socket carries deltas only; nothing is replayed.
- v1 tails every live agent that has a transcript (a handful of files, cheap).

## Live reply

Claude (and the other kinds) only write a text block to the transcript once it's complete, and Claude
writes a `tool_use` a few seconds after the tool has started. Without help the app would show nothing
but the pulsing dot. While an agent is `working`, the bridge reads the pane's visible screen and streams
what's in progress: the assistant prose being written and the tool that's running.

- `reply.live`: `agentId`, `seq`, `text`, `tool`. Every frame carries the full state; nothing is
  "unchanged if missing". A missing `tool` key means `null`.
  - `seq`: a per-agent counter that increases on every frame for that agent, including clearing ones,
    so the client can ignore a frame that arrives after a newer one already landed.
  - `text`: the current assistant prose of this turn (markdown), or `null`. Never tool text: no tool
    headers, commands, `⎿`/`└` output, spinners or "Running…" lines.
  - `tool`: the tool call that's on screen right now and not yet in the transcript, or `null`.
    - `name`: the same tool name the transcript's `toolCall` will carry for that kind (claude `Bash`,
      `Read`, `Edit`, `Write`, `Grep`, `Glob`, `WebFetch`, `WebSearch`, `Agent`, …; codex `Shell`,
      `Edit`, `<server>.<tool>`; pi `bash`, `read`, `write`, `edit`, `ls`, `grep`, `find`). An
      unrecognised tool is named `Tool`.
    - `summary`: built and scrubbed exactly like the transcript `toolCall.summary`, so the client can
      match the two (`Ran ping -c 8 127.0.0.1`, `Edited Sources/App.swift`). When the screen doesn't
      show the arguments yet (claude's "Running 1 shell command…"), it's the generic summary for that
      name (`Ran a command`, `Read a file`), and it's refined in a later frame once they show.
    - `state`: `"running"`. Clients should treat any other value the same way.
- Sent only while at least one `/ws` client is connected, throttled to about 4 frames per second per
  agent, and only when `text` or `tool` actually changed.
- Stable by construction. Screen redraws (claude blinks a running tool's `⏺`, spinners animate, a
  partial read can drop lines) never make the preview alternate:
  - `text` only grows. A different text replaces it only after two reads in a row agree (a new text
    block after a tool), and a replaced or landed text is never sent again this turn. A shorter or
    empty read keeps the current text.
  - `tool` appears, changes or clears only after two reads in a row agree, except a generic summary
    being refined, which goes out at once.
- Clearing, both bypass the throttle:
  - `text` goes to `null` once the transcript has that text (`message.upserted` for that agent carries
    it), `tool` goes to `null` once the transcript has its `toolCall` (same `summary`, or for a generic
    summary any new `toolCall` with the same `name`). The other field is untouched.
  - Both go to `null` when the agent stops working. A client should treat that like clearing a scratch
    buffer: stop showing the preview and let the real messages take over.
- The preview is never persisted and never duplicates transcript text or tool calls.
- Only the current turn counts: everything above the user's last prompt on screen is ignored, so a
  previous turn's reply never shows up as live text.
- The source per kind, read from the pane's visible screen (`agent.read source: visible`; herdr refuses
  to scroll a working agent's alternate-screen history, so `recent_unwrapped` isn't an option while it's
  actively working). ANSI, spinners, footers and box-drawing stripped; paths scrubbed the same way as
  everywhere else:
  - claude: blocks start at `⏺` (or, while a running tool's `⏺` blinks off, at the line before its
    `⎿`). A block is a tool when its header is a call signature `Name(args)` (possibly wrapped), when
    it has a `⎿` line, or when it's a grouped label like `Running 1 shell command…` / `Read 3 files`.
    The arguments come from the signature or from the `⎿  $ command` line. `text` is the last non-tool
    block; `tool` is the last block when it's a tool.
  - codex: `• ` bullets. `• Running …` / `• Ran …`, `• Explored`, `• Edited …`, `• Called …` and any
    bullet followed by a `└` result are tools; `• Working (…)` is chrome. codex streams its prose, so
    `text` grows like claude's.
  - pi: tools are its tool boxes (`$ command`, `read path`, `write path`, `edit path`, `ls path`,
    `grep /pattern/ in path`, `find pattern in path`); `tool` is the last one while its `Elapsed …` is
    still counting or nothing follows it. `text` is the last non-chrome paragraph after the last tool
    box, only while pi's own `Working` spinner isn't present (pi renders its reply in one piece).
- Wrapped lines are rejoined into one paragraph; a blank line in the block is kept as a paragraph break.
- Only the visible viewport is read, so only the tail of a reply is guaranteed once it scrolls: if the
  block's own start marker has scrolled off, the last paragraph still on screen is shown instead of nothing.

## Usage

Subscription usage, one entry per **subscription**, not per harness. Works for any Relay user out of
the box: no status-line setup, nothing to configure. Every provider entry carries `usedBy: [String]` —
which of the herdr-driven harnesses (`"claude"`, `"codex"`, `"pi"`) are actually authenticated against
that subscription on this Mac, e.g. `["codex", "pi"]`. A harness only appears in `usedBy` when its own
auth check reports it's actively using that subscription; it is never guessed.

Three provider entries:

- `claude`: spawns `claude -p "/usage" --output-format stream-json --verbose --no-session-persistence --strict-mcp-config`
  from a dedicated cwd (`~/.relay/usage-probe`), and parses the assistant message's structured
  `usage_report.rate_limits.limits[]` (not the prose). `--no-session-persistence` and the dedicated cwd
  keep this out of the user's normal Claude Code history, and `--strict-mcp-config` skips the user's MCP servers so the probe doesn't hang on them. The bridge stops reading at the first `usage_report` line (45 s backstop). `windows` are built from the `session` and
  `weekly_all` entries. `plan` comes from a second, much cheaper call, `claude auth status --json`
  (`subscriptionType`). `usedBy` always includes `"claude"`; it includes `"pi"` only when
  `pi auth check --provider anthropic --json` reports `status: "ready"` — pi's `/login` Anthropic
  session is a separate token store from Claude Code's own, so this is checked independently rather
  than assumed. It commonly reports `"invalid"` (not logged in on this Mac), in which case pi is left
  out of `usedBy` even though the card exists.
- `codex`: a one-shot JSON-RPC call to `codex app-server` (`account/rateLimits/read`), which uses
  codex's own already-stored ChatGPT OAuth session — the bridge never touches `~/.codex/auth.json`.
  `windows` are built from `rateLimits.primary`/`.secondary`. Labelled "ChatGPT". `usedBy` always
  includes `"codex"`; it includes `"pi"` when `pi auth check --provider openai-codex --json` reports
  `status: "ready"` (pi authenticates through the same ChatGPT OAuth account as Codex — confirmed in
  `docs/tasks/round-5/usage-pi-research.md`).
- `opencode-go`: no usage/quota API exists for this plan (it's a flat API-key credential, not an OAuth
  subscription with rate-limit headers — see `usage-pi-research.md`). The bridge does not call out to
  OpenCode or read the stored key at all; the card is built entirely from `pi auth check --provider
  opencode-go --json`. When that reports `status: "ready"`, the provider is `{id: "opencode-go", label:
  "OpenCode Go", plan: "OpenCode Go", windows: [], usedBy: ["pi"], stale: false, unavailableReason:
  "Usage not available from OpenCode"}` — deliberately not `stale`, since there's no fetch to go stale;
  the client shows this as a calm "no data" state, not the dimmed/warning stale treatment. When pi
  isn't authenticated to OpenCode Go at all, the provider is omitted from `providers[]` entirely rather
  than shown empty.

All three are read-only probes: nothing is typed into a herdr pane, none is a herdr agent kind, and
`pi auth check` never reads or prints a credential value — it only reports the stored credential's
validity.

- The bridge polls `claude` and `codex` on a timer, but only while at least one `/ws` client is
  connected (a poll with nobody watching burns a subscription's rate limit for no reason); `pi auth
  check` (for `usedBy` and the `opencode-go` card) is cheap enough to run on every poll pass alongside
  them, gated by the same connected-client check. `GET /usage` always serves the cache; it never
  triggers a fetch itself, so it's cheap to call from the app on screen appear.
- Backoff: each provider's own poll interval starts at 60 s and doubles (capped at 10 min) while its
  `windows` don't change between polls, and resets to 60 s the moment they do. `opencode-go`'s `windows`
  are always `[]`, so once seeded it settles at the 10-minute ceiling like any other unchanging provider.
- `POST /usage/refresh` (pull-to-refresh) bypasses backoff and polls immediately, but is rate-limited to
  once every 15 s across all providers (automatic or manual) — `429 rate_limited` otherwise.
- `stale: true` means `updatedAt` is more than 15 minutes old (screen wasn't open, or the harness has
  been unreachable), not that the numbers are wrong — the app should show them dimmed with "updated x
  ago" rather than hide them. A provider that has never successfully returned data has `windows: []`,
  `stale: true`, and `unavailableReason` set (e.g. the CLI isn't on `PATH`, or isn't logged in). This is
  distinct from `opencode-go`'s permanent `unavailableReason` above, which is `stale: false` — there was
  never a fetch to fail, so nothing is stale.
- `usage.updated` is sent once per provider whose snapshot actually changed (compared field by field,
  not just re-fetched, `usedBy` included) — same shape as `GET /usage`'s `providers[]`, one entry.
- Fixtures: `docs/fixtures/usage.json`.

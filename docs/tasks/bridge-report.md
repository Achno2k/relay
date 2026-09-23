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

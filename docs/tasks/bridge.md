# Task: build `bridge/` (the `herd` daemon)

Read first: `../AGENTS.md`, `docs/api.md` (the contract, follow it exactly), `docs/herdr-schema.json` (herdr socket API, protocol 22).

## Build
- A Swift package in `bridge/`. Swift 6, macOS 15+, Hummingbird 2 (+ HummingbirdWebSocket). Executable `herd`, library `HerdCore` for testability.
- `HerdrClient` (actor): newline-delimited JSON requests/responses over the unix socket at `$HERDR_SOCKET_PATH` (default `~/.config/herdr/herdr.sock`).
  - Work out the exact request envelope from the schema. Use `herdr api snapshot` and the CLI's JSON output to check it.
  - Methods used: `agent.list`, `agent.get`, `agent.prompt`, `agent.send_keys`, `agent.read` (source `detection`/`recent`), `agent.start`, `tab.create`, `workspace.list`, `pane.list`.
  - A separate long-lived connection for `events.subscribe` (pane.agent_status_changed, pane.created, pane.closed, pane.agent_detected, pane.updated), with reconnect.
- `TranscriptParser`: turns Claude JSONL into the `[Message]` model, following the transcript rules in api.md. Keep it pure and fully tested. pi JSONL is best effort.
- `TranscriptTailer`: a DispatchSource file watch per live agent with a transcript. It reads only the appended bytes (keep the offset and handle partial lines). Emits `message.upserted` for new or grown messages.
- `ApprovalParser`: turns the `agent.read` detection/visible text of a blocked Claude agent into an `Approval`: the question line plus numbered options `❯ 1. Yes` / `2. ...`. Option N maps to keys `["N"]`, and a trailing "No" also maps to `["esc"]` if that's how it renders. Test it against synthetic screen captures.
- Path scrubbing: cwd-relative, otherwise last path component only. `cwdName` is the last component of the cwd.
- Agent `status` is always taken from a fresh `agent.get`/`agent.list`, never from the event payload.
- HTTP + WS server exactly as in api.md. Bearer auth middleware; `/ws` checks `?token=`. Bind to 127.0.0.1 and to the Tailscale IPv4 (`tailscale ip -4`, skipped if unavailable). Port 7878, overridable with `--port`.
- CLI (swift-argument-parser): `herd serve`, `herd pair` (prints the `herd://pair?...` URL plus a QR code in the terminal; draw it with a tiny inline QR encoder or CoreImage `CIQRCodeGenerator` rendered to half-block chars), `herd token --rotate`, `herd install-launchd` (writes a LaunchAgent plist; do not load it, print the command instead).

## Rules
- Never copy real transcripts or agent lists into the repo. To learn the format, inspect `~/.claude/projects/*/*.jsonl` locally, then write synthetic fixtures under `bridge/Tests/Fixtures/`.
- `swift build` and `swift test` must be clean with no warnings.
- Commit in small logical commits in the herd repo (end each message with the trailer lines below).
- When done, run `herd serve` on a spare port, curl `/health`, `/agents` and `/agents/<id>/messages` for a real agent, and leave a short report at `docs/tasks/bridge-report.md`: what works, what doesn't, and any deviations from api.md (update api.md if you had to deviate).

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Q4qo2QabVUAvi2YgczSwpD
```

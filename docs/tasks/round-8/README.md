# Round 8: port the bridge to Go, and fix every open bug

The user wants the backend in Go instead of Swift. Go builds one static binary for macOS and Linux, and that sets up the next feature, multi-machine support with Linux remotes.

Read first: `../../../AGENTS.md`, `../../api.md` (all of it), and the Swift bridge files you're porting plus their tests in `bridge/Tests/RelayCoreTests/`.

## Goal
- Port `bridge/` (Swift, about 6k lines, 247 tests) to `bridge-go/` in Go, **behavior-identical**.
  - `docs/api.md` and `docs/fixtures/` are frozen. Every response and WS frame must match the Swift bridge byte for byte in shape: keys, nulls vs missing, date format, error codes.
- Clean structure is welcome: Go packages split by concern, interfaces at the seams. Changing behavior is not, unless it fixes a bug filed in `docs/qa/round8.md`.
- Fix every bug that exists today:
  - Bridge bugs get fixed in the Go port only. The Swift bridge is frozen and gets deleted at cutover.
  - App bugs get fixed in `ios/`.
- Linux support comes for free: the port must build and pass tests with `GOOS=linux`. Multi-machine app work is **not** in this round.

## Tech
- Go 1.24, module `relay` in `bridge-go/`. Binary `relay`, built from `bridge-go/cmd/relay`.
- Dependencies (only go-core edits `go.mod`, so ask it for anything else):
  - `github.com/coder/websocket` (WS)
  - `github.com/fsnotify/fsnotify` (tailing on both OSes)
  - `rsc.io/qr` (pairing QR)
  - Everything else comes from the stdlib: `net/http` 1.22 routing, `log/slog`, `flag` subcommands, `os/exec`.
- Machine-specific code sits behind build tags: `_darwin.go` / `_linux.go`.
- Quality bar per package:
  - `gofmt -l` is clean.
  - `go vet ./...` passes.
  - `go test -race ./...` passes.
  - `GOOS=linux go vet ./...` passes.
- Each Swift test gets ported as a Go test, same cases and same names (so parity is auditable). Test fixtures are copied into `bridge-go/**/testdata/` and must be synthetic.
- `RELAY_HOME` overrides `~/.relay`, as in Swift. **Never run a Go bridge against the real `~/.relay`** until cutover. Use `RELAY_HOME=$(mktemp -d)` and a port other than 7878.

## Sessions and ownership
| Session | Owns (Go) | Ports from Swift | Swift tests to port |
|---|---|---|---|
| go-core | `go.mod`, `cmd/relay/`, `internal/api/` (wire types + errors), `internal/herdr/` (socket, client, event stream, key names, `herdrtest` fake), `internal/config/` (home, token, `~/.herd` migration, tailscale, launchd + **new systemd unit**, log rotation), `internal/machine/` (darwin + **new linux**), `internal/qr/` | Models, UsageModels, APIError, UnixSocket, HerdrClient, HerdrEventStream, KeyNames, Config, LogRotator, QRCode, `relay` CLI, the Machine part of Uploads | SocketResilience, FdHygiene, KeyNames, LogRotator, Migration, Support/FakeHerdr |
| go-transcripts | `internal/transcript/` | TranscriptParser, TranscriptLocator, CodexRollouts, TranscriptTailer (fsnotify), PathScrubber, ToolSummary | TranscriptParser, CodexTranscript, Tailer, HugeTranscript, PathScrubber, Fuzz |
| go-live | `internal/live/`, `internal/approval/` | LiveReplyParser, LiveReplyTracker, LiveReplyMonitor, ApprovalParser, InputBox | LiveReplyParser, LiveReplyTracker, LiveReplyMonitor, ApprovalParser, InputBox |
| go-drivers | `internal/controls/`, `internal/usage/`, `internal/uploads/` | AgentControls, ClaudeControls, ControlDrivers, CodexDriver, PiDriver, ModelCatalogs, SettingsGuard, Usage*, Uploads (minus Machine), UploadCleaner | Controls, ClaudeControls, AgentDrivers, AgentDriversFlow, Usage, Uploads |
| go-server | `internal/server/` (routes, auth/error middleware, WS, event hub), `internal/service/` (AgentService, AgentMonitor, app wiring) | Server, AgentService, AgentMonitor, EventHub, RelayApp | Routes, Auth, NewAgent, InputClearing, Misc |
| go-parity | `bridge-go/parity/` (tool + script), `docs/tasks/round-8/parity.md` | none | none |
| qa-bridge | `docs/qa/round8.md` (bridge section). **No code.** | none | none |
| ios-bugs | `ios/**` | none | none |

If a Swift test covers code in another session's package, port it in that session and tell the other one.

## Order and interfaces
1. **go-core lands the skeleton first, within about 30 minutes.**
   - Contents: `go.mod` with all the deps, the package dirs, `internal/api` types, and the `herdr.Client` interface plus the `herdrtest` fake.
   - Once committed, it messages every go-* session.
   - The others start by reading their Swift and porting pure logic, then switch to `internal/api` types once they land.
2. go-server defines the interfaces it consumes from transcript / live / controls / usage / uploads and agrees them by herdr message. Keep them small; a package exposes functions and types, and the server wires them.
3. Before the other go-* sessions finish, go-parity builds a harness that runs the Go bridge (`RELAY_HOME` temp, port 7880) next to the live Swift bridge (7878). It then diffs:
   - every GET endpoint, for every agent, workspace, `/machine`, `/usage` and controls;
   - the WS streams, while prompts go to the **e2e agents only**.
4. qa-bridge audits the Swift bridge and the live system for bugs and files each one to the **Go owner** of that area. ios-bugs hunts and fixes app bugs.
5. The lead (relay-lead) verifies everything, then does the cutover:
   - the LaunchAgent points to the Go binary;
   - `bridge-go/` becomes `bridge/`;
   - the docs and scripts get updated.

## Rules
- Stay in your paths and commit only them (`git add <paths>`, never `-a`/`-A`/`.`). Everyone shares one tree on `master`.
- Messages:
  - To a peer: `herdr agent prompt <name> "<short message>"`, without `--wait`.
  - The lead is `relay-lead`. Ask it about anything blocking or permission-sensitive.
- **Don't rebuild, restart or kickstart the live Swift bridge (`com.relay.bridge`, 7878).** Read it and probe it only.
- Test agents live in herdr workspace `herd-e2e` (cwd `~/.relay/e2e`): `w14:p2` claude, `w14:p4` pi, `w14:p5` codex.
  - **Never touch the user's other agents.** Read-only GETs against them are fine; prompts, keys and controls are not.
  - Codex must run in the real path, not a symlinked one.
  - Only one session sends to an e2e agent at a time: go-parity and qa-bridge coordinate.
- Only ios-bugs uses the simulator (iPhone 17 Pro). The iPhone is off limits unless relay-lead says otherwise.
- Never commit real transcripts, agent lists, chat titles or paths from this machine. Fixtures are synthetic.
- Full paths never cross the wire. Status is re-derived from herdr, never cached as truth.
- When done, write `docs/tasks/round-8/<session>-report.md` covering:
  - what changed;
  - the tests ported (Swift count vs Go count);
  - how it was verified;
  - what's left.

  Then message relay-lead.

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

## Per-session notes
- **go-core:**
  - `relay` CLI: `serve` (same flags: `--port`, `--local-only`, `--require-tailscale`), `pair`, `token`, `install-launchd`, plus a new `install-systemd` that writes a user unit.
  - Tailscale binary lookup on Linux: `/usr/bin/tailscale` and `PATH`.
  - Machine on Linux:
    - `id` = `/etc/machine-id`;
    - `name` = hostname;
    - `kind` = `laptop` if `/sys/class/power_supply/BAT*` exists, else `desktop`;
    - `model` = `/sys/class/dmi/id/product_name`, falling back to `uname -m`;
    - `os` = `PRETTY_NAME` from `/etc/os-release`.

    Update `api.md` Machine notes to say so (only that section).
  - Try a Linux test run with `podman` (`podman machine start`, then the `golang:1.24` image). If that won't work, `GOOS=linux go vet` + `go test` compile-only (`go test -c`) is the floor.
- **go-transcripts:** the tailer must survive rotation, truncation and huge files the same way the Swift one does (see TailerTests / HugeTranscriptTests). It uses fsnotify with a polling fallback.
- **go-live:** there's a known open item from round 5: an "Update available!" banner can bleed into live reply text on rare ANSI overlap. Fix it properly and add a test.
- **go-drivers:** usage runs `claude -p /usage --no-session-persistence` and the codex app-server. Keep the PATH handling working on Linux too, and don't hardcode `/opt/homebrew` only.
- **go-server:** port every Round 5 hardening item from `api.md`:
  - the 2 MB body cap;
  - `503 herdr_unavailable` fast;
  - `/health` fields;
  - the fd hygiene;
  - reconnects.

  WS auth via `?token=`.
- **go-parity:**
  - The harness must be rerunnable by the lead with one command.
  - It prints a clear diff and exits non-zero on mismatch.
  - It normalizes only fields that legitimately differ: `uptimeSeconds`, timestamps from "now", `seq`.
  - Once parity is clean, also run the iOS live UI tests (simulator, coordinate with ios-bugs) against the Go bridge on 7880.
  - Also review the Go code against the Swift for missed behavior, and file findings to `docs/qa/round8.md`.
- **qa-bridge:**
  - Audit the Swift bridge by code review plus live probes (the e2e agents only). Look for:
    - races and leaks;
    - wrong status;
    - transcript edge cases (Claude / Codex / pi);
    - approvals;
    - controls per kind;
    - uploads;
    - WS reconnect/replay;
    - path leaks;
    - the herdr socket dying.
  - Each bug goes into `docs/qa/round8.md` with an id `R8-n`, severity, the Go owner, a repro and the expected behavior. Then message the owner.
  - Afterwards, re-verify each fix against the Go bridge (port 7880).
- **ios-bugs:**
  - Hunt and fix app bugs: code review, the mock UI tests, and the simulator against the live bridge.
  - Areas:
    - reconnects;
    - background/foreground (never manually tested);
    - offline/airplane (simulate by blocking the bridge port or pointing at a dead port);
    - duplicate messages;
    - approvals;
    - controls;
    - attachments (10 files, 20 MB through the picker);
    - re-pairing after token rotation (`needsRePairing`);
    - sidebar;
    - accessibility.
  - File each bug in `docs/qa/round8.md` (iOS section), fix it with a test, and keep `RelayTests` + mock UI tests green.
  - Don't change the API. If the app needs a bridge change, message relay-lead.

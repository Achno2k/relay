# Round 8 QA: bugs

Owners are round 8 sessions (see `docs/tasks/round-8/README.md`). Bridge bugs are fixed in the Go port (`bridge-go/`), not in Swift.

## Bridge

| id | severity | owner | status | title |
|---|---|---|---|---|
| R8-1 | P1 | go-drivers | fixed 083c18c, live check pending (7880) | Claude usage card stale for days: `claude -p /usage` outlives the 20 s watchdog |
| R8-2 | P1 | go-drivers | fixed 083c18c, live check pending (7880) | `GET /usage` blocks for the whole poll (64 s measured) |
| R8-3 | P1 | go-server | open | Error messages leak full paths (herdr messages and screen quotes passed verbatim) |
| R8-4 | P2 | go-server | open | `POST /agents` leaves an orphan shell tab when `agent.start` fails |
| R8-5 | P2 | go-core, go-server | open | herdr client errors (`unsupported_agent_kind`, `agent_name_taken`) become `502` after ~6.6 s of retries |
| R8-6 | P2 | go-core | open | herdr dying mid-request answers `502 herdr_error`, api.md says `503 herdr_unavailable` |
| R8-7 | P2 | go-core, go-server | open | Hung herdr: `504` after 15 s, `/health` stays `connected`, calls starve 13 s after recovery |
| R8-8 | P2 | go-transcripts | fixed f460552, QA cases pass (package); live pending | Path leak: `file:///Users/...` and `host:/Users/...` are not scrubbed |
| R8-9 | P2 | go-transcripts | fixed f460552, QA cases pass (package); live pending | `<task-notification>` (and `<bash-input>`/`<bash-stdout>`) user lines show as raw-XML user bubbles |
| R8-10 | P2 | go-live | open | Permission dialog footer "Esc to cancel · Tab to amend" is sent as `reply.live` text |
| R8-11 | P2 | go-server, go-drivers | open (api.md decided, c28fd33) | pi/codex prompts and slash-command controls glue onto leftover input text |
| R8-12 | P2 | go-server, go-core | open | SIGTERM is ignored: the bridge is still alive 60 s later |
| R8-13 | P2 | go-server | open | `Agent.updatedAt` goes stale (cached mtime); found by go-parity |
| R8-14 | P3 | go-live | open | Wrapped approval option labels are cut at the line break |
| R8-15 | P3 | go-transcripts | fixed f460552, live check pending (7880) | Tailer (re)start reads the whole transcript into memory, no cap |
| R8-16 | P3 | go-transcripts | fixed f460552, live check pending (7880) | Tailer: bytes written between the seed read and the watcher start wait for the next write |
| R8-17 | P3 | go-drivers | fixed 083c18c, live check pending (7880) | `/model`/`/effort` from the phone persist as default when `~/.claude/settings.json` didn't exist |
| R8-18 | P3 | go-server | open | WS with a bad token gets a bodiless `400`, not `401` JSON |
| R8-19 | P3 | go-server | open | Per-agent state maps are never pruned when agents close |
| R8-20 | P3 | go-core | open | Too-long `HERDR_SOCKET_PATH` fails every request with `502 herdr_error` instead of at startup |
| R8-21 | P3 | go-core | open | `~/.relay` keeps loose permissions (0744) if it already exists |
| R8-22 | P2 | go-live | open | Claude tool output whose header scrolled off leaks into `reply.live` (as text, or as a junk `Tool`) |
| R8-23 | P3 | go-live | open | Claude fullscreen chrome (`1 new message (click) ↓`, `Jump to bottom (click)`) shows up in `reply.live` text |

How these were found: code review of `bridge/Sources/RelayCore`, live probes against 7878 with the e2e agents (`w14:p2` claude, `w14:p4` pi), and a throwaway Swift bridge (`RELAY_HOME` temp, port 7890) on a fake herdr socket that can answer, drop the connection, or hang. Paths below are synthetic.

Checked and fine (no bug): WS auto-ping is on (Hummingbird default 30 s), so dead clients don't keep usage polling alive. Body cap `413`, bad JSON `400`, unknown agent/message `404`, key validation, 10-attachment cap, 20 MB upload cap, empty upload, upload name sanitising (`../../etc/pa ss✓wd` → `etc-pa-ss-wd`), attachment id lookup rejects non-ids.

### R8-1: Claude usage card stale for days
- Repro: `GET /usage`. The `claude` provider has `stale: true`, `unavailableReason: "claude -p /usage didn't return usage data"`, and `updatedAt` three days old. `POST /usage/refresh` doesn't fix it.
- Cause: `claude -p /usage --output-format stream-json --verbose --no-session-persistence` now takes 8 to 18 s (37 s with a launchd-like env) before its assistant line, because it starts every MCP server, plugin and hook first. The 20 s watchdog kills it before the line with `usage_report` is written.
- Expected: the card refreshes.
- Fix: add `--strict-mcp-config` (assistant line at 3.7 s, `usage_report` still present). Read stdout line by line and stop at the first line with `usage_report`. Raise the timeout to about 45 s as a backstop.

### R8-2: `GET /usage` blocks during a poll
- Repro: `POST /usage/refresh`, then `GET /usage` right after. Measured 63.8 s, then 18.1 s, then 0.005 s.
- Cause: `UsageMonitor` is an actor and runs the blocking probes (`claude` 20 s, `claude auth` 8 s, codex 6 s, three `pi auth check` 8 s each) on itself, so `snapshot()` waits for the whole poll.
- Expected (api.md): `GET /usage` always serves the cache and is cheap.
- Fix: keep the cache behind a mutex and run probes outside it; `snapshot()` never waits on a probe.

### R8-3: error messages leak full paths
- Repro: `POST /agents {"workspaceId":"w14","kind":"pi","name":"bridge-pi"}` (name already used). Response: `{"error":{"code":"agent_name_taken","message":"agent name bridge-pi is already used; candidates: … cwd=/Users/dev/.relay/e2e status=Done"}}`.
- Cause: `APIError.from` passes herdr's message through verbatim. `control_failed` also quotes the agent's screen line unscrubbed (`CodexDriver`/`PiDriver`/`waitForScreen`).
- Expected (AGENTS.md): full paths never cross the wire.
- Fix: scrub every error `message` in the error middleware (a cwd-less `PathScrubber` is enough); scrub `control_failed` quotes with the agent's cwd.

### R8-4: orphan tab after a failed `POST /agents`
- Repro: `POST /agents {"workspaceId":"w14","kind":"nosuchagent"}`, or the duplicate-name body from R8-3. Each leaves a new plain-shell tab in w14 (seen as `w14:tJ`, `w14:tK`; closed by hand).
- Expected: nothing left behind on failure.
- Fix: close the tab when `agent.start` fails with anything but `agent_not_ready` (that one leaves a live agent, keep it).

### R8-5: herdr client errors become 502 after retries
- Repro: same two bodies as R8-4. Both answer `502` after 6.6 s and 6.9 s.
- Cause: `create` retries every `HerdrError` except codes starting with `invalid` or containing `unknown`, and `APIError.from` maps everything else to `502`.
- Expected: `400 bad_request` (or `400 unsupported`) for an unsupported kind, `409` for a taken name, answered at once.
- Fix: retry only while the new shell isn't ready; map `unsupported_*` → 400 and `*_taken` → 409 (go-core owns the mapping, go-server the retry loop).

### R8-6: herdr dying mid-request gives 502
- Repro (fake herdr): accept the connection, read the request, close without answering. `GET /agents` → `502 {"code":"herdr_error","message":"herdr closed the connection"}`.
- Expected (api.md Round 5 hardening): `503 herdr_unavailable`. The Swift test only covers a socket that's already gone (connect refused).
- Fix: EOF or reset before a response line maps to `herdr_unavailable` (503).

### R8-7: hung herdr
- Repro (fake herdr that accepts and never answers): `GET /agents` → `504 herdr_timeout` after 15.0 s. `/health` says `"herdr":"connected"` the whole time. With 100 requests in flight while hung, the first `GET /agents` after herdr recovers takes 13.1 s (GCD threads are all blocked in `read`).
- Expected (api.md): answers within a couple of seconds, never hangs; `/health` reflects reachability.
- Fix: short per-call deadline for reads and lists (about 3 s; keep a long one for `agent.start`), `/health` from a cheap live check or the last call's outcome, no bounded worker pool (plain goroutines with `SetDeadline`).

### R8-8: `file://` and `host:` paths not scrubbed
- Repro (Swift `PathScrubber`, cwd `/Users/dev/proj`): `open file:///Users/dev/Downloads/secret.pdf` and `scp host:/Users/dev/notes/a.txt .` come out unchanged. `PATH=…:/Users/dev/.local/bin` and `--dir=/Users/…` are fine.
- Where it shows up: codex `McpToolCall` results, `UserMessage`/`AgentMessage` text and command output carry `file:///Users/…` (about 25 completed items across local rollouts; `cwd` and `ImageView.path` are already stripped).
- Cause: the path regex's lookbehind excludes `/` and `:`.
- Fix: turn `file://<abs path>` into the scrubbed path, and allow a path after `:` when it isn't `://`. Add both cases to the PathScrubber tests.

### R8-9: `<task-notification>` user bubbles
- Repro: a Claude agent that ran a background task. `/messages` has a user message whose text starts with `<task-notification>` (confirmed on the wire). Locally 121 such lines in two weeks, plus a few `<bash-input>`/`<bash-stdout>` (Claude's `!` shell mode).
- Expected: Claude Code injects these; the user didn't type them. They shouldn't look like the user's own prompts.
- Fix: add `<task-notification` to the dropped prefixes. `<bash-input>` could become a user text `$ cmd` and `<bash-stdout>` a code block, or be dropped. Decided in api.md (c28fd33): drop `<task-notification`, `<bash-stdout>`, `<bash-stderr>`; `<bash-input>cmd</bash-input>` becomes a user text `! cmd`.

### R8-10: dialog footer sent as live text
- Repro: e2e claude in `default` mode, prompt it to create a file with Write. WS: `reply.live` seq 56 `text: "Esc to cancel · Tab to amend"`, cleared 130 ms later when the agent turns `blocked`.
- Expected (api.md Live reply): `text` is assistant prose only, never chrome.
- Fix: treat a permission dialog (the rule plus `Do you want to …?`, numbered `❯ 1.` rows, `Esc to cancel` footer) as chrome that ends the text block. Add a parser test with this screen.

### R8-11: pi/codex prompts glue onto leftover input
- Repro: `POST /agents/w14%3Ap4/text {"text":"LEFTOVER ","submit":false}`, then `POST …/prompt {"text":"Reply with just the word ok"}`. pi's transcript user message is `LEFTOVER Reply with just the word ok`.
- Cause: `clearInput` returns early for any kind but claude, and controls use `submit` ("they start empty") for pi and codex. Codex runs the same code path (not run live, to save codex quota).
- Expected: a prompt or `/model`, `/thinking`, `/compact`, `/new` never merges with text already in the box.
- Fix: clear pi's and codex's input before `agent.prompt` too (find the right key per kind, e.g. `ctrl+u` repeated, and confirm from the screen like Claude's `InputBox`). Decided in api.md (c28fd33): every kind's input is emptied before a prompt and before slash-command controls.

### R8-12: SIGTERM ignored
- Repro: start a bridge (temp home, port 7890), `kill -TERM`. Still alive after 60 s; needs `kill -9`. go-parity saw the same.
- Cause: `UsageMonitor.run` and `LogRotator.run` sleep in loops without `cancelWhenGracefulShutdown`, so the `ServiceGroup` waits forever.
- Expected: exit within a couple of seconds, so `launchctl bootout`/`kickstart -k` and `systemctl --user stop` don't wait for SIGKILL.
- Fix (Go): every loop selects on the root context; `serve` shuts the HTTP servers down with a short timeout.

### R8-13: `Agent.updatedAt` goes stale (go-parity)
- Repro (go-parity, live): 7878 `/agents/w14:p2` had `updatedAt` 2026-09-25T09:35 while the transcript mtime was 2026-09-28T11:07; a fresh Swift instance returned 11:07.
- Cause: `snapshot` reads the mtime with `URL.resourceValues`, which Foundation caches per URL instance; reused URLs keep the first value.
- Expected: `updatedAt` ≥ the transcript's current mtime.
- Fix (Go): `os.Stat` every time.

### R8-14: wrapped approval labels cut
- Repro: Write approval on e2e claude. Screen row 2 wraps: `2. Yes, and switch to accept edits (…) for this session; Yes, and always allow access to` / `   <cwd> for this session (shift+tab)`. `/approval` label ends at "allow access to".
- Fix: join indented continuation lines into the option above, strip the trailing hint, then scrub (the continuation holds the cwd).

### R8-15: tailer reads the whole file on start
- `TranscriptTailer.start` seeds by reading everything from offset 0 in one `read`. `/messages` caps at 64 MB (`readBounded`); the tailer has no cap and runs on every tailer (re)creation (after rotation, a dead tailer, a new session).
- Fix: seed from at most the same 64 MB tail, aligned to a newline.

### R8-16: tailer seed race
- `start` reads the file, then creates the watch. Bytes appended in between are only read on the next write event, so a turn's last message can wait until the agent writes again (the monitor only recreates dead tailers).
- Fix: start the fsnotify watch first, then seed, then read once more.

### R8-17: settings default kept when the file didn't exist
- `SettingsGuard.snapshot()` holds `nil` when `~/.claude/settings.json` is missing and `restore` then does nothing, so the file Claude creates for `/model sonnet` stays and becomes the default.
- Expected (api.md): the bridge never saves a phone change as the default.
- Fix: remember "absent" and delete the file (or the keys Claude added) on restore.

### R8-18: WS bad token → 400
- Repro: connect to `/ws?token=bad`. `400`, empty body. REST gives `401 {"error":{"code":"unauthorized",…}}`.
- Fix: `401` with the JSON body before the upgrade. The app treats a refused socket as a token problem already (`Models.swift` comment), so keep that accepting 400 and 401.

### R8-19: per-agent maps never pruned
- `AgentService.seen`, `lastControls`, `sticky`, `confirmed`, `createdKinds`, `controlCache` (per transcript URL) and `TranscriptLocator.found` only grow. Pane ids are never reused, so every closed agent stays forever.
- Fix: drop per-pane entries on `agent.closed`; cap the per-file caches.

### R8-20: long socket path
- `HERDR_SOCKET_PATH` longer than `sun_path` (104 bytes on macOS): the bridge starts, `/health` says `unavailable`, every route answers `502 {"code":"herdr_error","message":"socket path too long"}`.
- Fix: check at startup and exit with a clear message.

### R8-21: `~/.relay` permissions
- `~/.relay` is `drwxr--r--` here: launchd creates it for the log before the bridge runs, and `RelayHome.ensure` only applies 0700 when it creates the directory. The token and uploads are 0600/0700, so nothing readable leaks today; `relay.log` is 0644.
- Fix: `chmod 0700` the home on start; create the log 0600.

### R8-22: scrolled-off tool output leaks into live reply (go-live)
- Seen live by go-live (read-only Go monitor with Swift-identical parsing, narrow or short panes, long tool output).
- When a Claude tool's `⏺ Name(args)` header has scrolled off the viewport, the indented `⎿` output left on screen has no block marker. It becomes `reply.live` `text` (e.g. `…(1m 4s · 5 lines) (ctrl+b to run in background)`), or a tool named `Tool` whose summary is diff lines (an Edit's `96  ### …` rows).
- Expected (api.md Live reply): `text` is never tool output; an unrecognised fragment isn't a tool.
- Fix: a leading run of indented `⎿`/output lines with no header above is tool output: skip it for `text`, and don't build a tool from it unless the header is visible. Add parser tests with a viewport that starts mid-output.

### R8-23: fullscreen chrome in live text (go-live)
- Seen live by go-live. Claude's fullscreen view draws `1 new message (click) ↓` and `Jump to bottom (click)`, and those lines end up in `reply.live` `text`.
- Fix: add both to the chrome list the parser strips. Test with a screen that has each.

## iOS

| id | severity | owner | status | title |
|---|---|---|---|---|

Details below, one section per bug: repro, expected, actual, fix, how verified.

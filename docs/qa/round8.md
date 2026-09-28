# Round 8 QA: bugs

Owners are round 8 sessions (see `docs/tasks/round-8/README.md`). Bridge bugs are fixed in the Go port (`bridge-go/`), not in Swift.

## Bridge

| id | severity | owner | status | title |
|---|---|---|---|---|
| R8-1 | P1 | go-drivers | verified (Go, 7891): claude card fresh, not stale | Claude usage card stale for days: `claude -p /usage` outlives the 20 s watchdog |
| R8-2 | P1 | go-drivers | verified (Go, 7891): GET /usage 0.5 ms during a refresh | `GET /usage` blocks for the whole poll (64 s measured) |
| R8-3 | P1 | go-server | verified (Go, 7891): herdr message says cwd=e2e | Error messages leak full paths (herdr messages and screen quotes passed verbatim) |
| R8-4 | P2 | go-server | verified (Go, 7891): no tab left in w14 | `POST /agents` leaves an orphan shell tab when `agent.start` fails |
| R8-5 | P2 | go-core, go-server | verified (Go, 7891): 400 in 33 ms, 409 in 41 ms | herdr client errors (`unsupported_agent_kind`, `agent_name_taken`) become `502` after ~6.6 s of retries |
| R8-6 | P2 | go-core | verified (Go, fake herdr): 503 herdr_unavailable | herdr dying mid-request answers `502 herdr_error`, api.md says `503 herdr_unavailable` |
| R8-7 | P2 | go-core, go-server | verified (Go, fake herdr): 504 in 3.0 s, /health unavailable, 5 ms after recovery | Hung herdr: `504` after 15 s, `/health` stays `connected`, calls starve 13 s after recovery |
| R8-8 | P2 | go-transcripts | verified (Go, 7891): no /Users or file:/// blocks on the wire | Path leak: `file:///Users/...` and `host:/Users/...` are not scrubbed |
| R8-9 | P2 | go-transcripts | verified (Go, 7891): no raw-tag user bubbles; `! cmd` in package test | `<task-notification>` (and `<bash-input>`/`<bash-stdout>`) user lines show as raw-XML user bubbles |
| R8-10 | P2 | go-live | verified (Go, 7891): no chrome in any reply.live frame | Permission dialog footer "Esc to cancel · Tab to amend" is sent as `reply.live` text |
| R8-11 | P2 | go-server, go-drivers | verified (Go, 7891) on pi: prompt arrived without LEFTOVER; codex not run live (quota) | pi/codex prompts and slash-command controls glue onto leftover input text |
| R8-12 | P2 | go-server, go-core | verified (Go): exits 0.04 s after SIGTERM with a WS client | SIGTERM is ignored: the bridge is still alive 60 s later |
| R8-13 | P2 | go-server | verified (Go, 7891): updatedAt equals transcript mtime | `Agent.updatedAt` goes stale (cached mtime); found by go-parity |
| R8-14 | P3 | go-live | verified (Go, 7891): wrapped label joined, cwd scrubbed, hint gone | Wrapped approval option labels are cut at the line break |
| R8-15 | P3 | go-transcripts | fixed f460552 (package tests); not load-tested live | Tailer (re)start reads the whole transcript into memory, no cap |
| R8-16 | P3 | go-transcripts | fixed f460552 (package tests) | Tailer: bytes written between the seed read and the watcher start wait for the next write |
| R8-17 | P3 | go-drivers | fixed 083c18c (package tests); not run live (would delete settings.json) | `/model`/`/effort` from the phone persist as default when `~/.claude/settings.json` didn't exist |
| R8-18 | P3 | go-server | verified (Go, 7891): 401 + JSON | WS with a bad token gets a bodiless `400`, not `401` JSON |
| R8-19 | P3 | go-server | fixed 3414f33 (package tests) | Per-agent state maps are never pruned when agents close |
| R8-20 | P3 | go-core | verified (Go): exit 78 with a clear message | Too-long `HERDR_SOCKET_PATH` fails every request with `502 herdr_error` instead of at startup |
| R8-21 | P3 | go-core | verified (Go): home and token 0700/0600 | `~/.relay` keeps loose permissions (0744) if it already exists |
| R8-22 | P2 | go-live | fixed 352be0c (package tests); not reproduced live | Claude tool output whose header scrolled off leaks into `reply.live` (as text, or as a junk `Tool`) |
| R8-23 | P3 | go-live | fixed 352be0c (package tests); not reproduced live | Claude fullscreen chrome (`1 new message (click) ↓`, `Jump to bottom (click)`) shows up in `reply.live` text |
| R8-24 | P3 | go-live | fixed 7fed37f, QA cases pass (package); live check after go-parity's slot | Claude's single-file label `Reading <file>` goes out as tool `Tool`, not `Read` |

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

### R8-24: `Reading <file>` becomes tool `Tool` (Swift and Go)
- Repro (Go 7891, e2e claude, a Read that needs approval): while the read runs the screen shows `Reading qa-note-r8.txt` / `⎿  qa-note-r8.txt`. `reply.live` seq 4 sends `tool: {"name":"Tool","summary":"Reading qa-note-r8.txt"}`.
- Cause: the grouped-label pattern (`claudeGroup` in Go, `LiveReplyParser` in Swift) needs a count (`Reading 2 files`); the single-file form has none.
- Expected (api.md Live reply): name `Read`, summary `Read qa-note-r8.txt` (same as the transcript's `toolCall`).
- Fix: accept `Reading|Read <path>` (and likely `Writing`, `Editing`, `Searching for <pattern>` without a count) and map them like the grouped labels. Add a parser test.

## iOS

| id | severity | owner | status | title |
|---|---|---|---|---|
| R8-i1 | P1 | ios-bugs | fixed, verified (sim) | Token rotated: app shows "Reconnecting…" forever, never offers to pair again |
| R8-i2 | P2 | ios-bugs | fixed, unit-tested | A failed approval answer (offline) removes the card; the blocked agent can't be answered |
| R8-i3 | P2 | ios-bugs | fixed, verified (sim) | Background keeps a socket the app can't read; foreground and network-back wait out a backoff of up to 30 s |
| R8-i4 | P3 | ios-bugs | fixed, verified (sim) | Files picker reads every file in full on the main thread, even a 600 MB one it then rejects |
| R8-i5 | P3 | ios-bugs | fixed, unit-tested | Every resync replaces the open chat with the last 50 messages: scrolled-up history is lost, a message that lands mid-request vanishes |

How these were found: code review of `ios/`, the mock UI tests, and the simulator (iPhone 17 Pro) against the live bridge through a local proxy on 7881 that can drop every connection ("bridge down") or answer like a bridge with a rotated token (`401` JSON on REST, bare `400` on the WS upgrade, as the Swift bridge does today). The live bridge was never restarted and its token never changed.

Details below, one section per bug: repro, expected, actual, fix, how verified.

### R8-i1: token rotation never reaches the re-pair prompt
- Repro: app connected. `relay token --rotate` and restart the bridge (simulated: proxy flips to "rotated"). Wait.
- Expected: the sticky "Token rejected. Tap to pair again." banner.
- Actual (HEAD build, sim): "Reconnecting…" forever. Proxy log shows only `/ws` retries (1, 2, 4, 8, 16 s…), never a REST call, and only REST sets `needsRePairing`.
- Cause: the bridge refuses the WS upgrade with a bodiless `400` (R8-18); `WSClient` treated it like any drop.
- Fix: `WSClient` reads the upgrade's HTTP status and yields `.rejected(status:)` before `.disconnected`. `AppStore`: `401`/`403` sets `needsRePairing`; anything else resyncs over REST, which says `401` if it's the token. "Reconnecting…" is hidden while the re-pair banner shows. Works with either bridge answer (400 now, 401 once R8-18 lands).
- Verified: sim, fixed build: the re-pair banner appears 1 s after the flip. Unit tests `refusedSocket*`, `handshakeRejectionReadsTheUpgradeStatus`.

### R8-i2: failed approval answer strands the agent
- Repro: agent blocked, sheet open. Go offline (bridge down), tap an option.
- Expected: the answer fails visibly and the question can still be answered.
- Actual: `answer()` clears `approval` before sending, so the sheet and the card both go. The status stays `blocked`, so nothing refetches the approval (and for 15 s it's also marked "just answered"). Only a foreground or reconnect brings it back.
- Fix: on failure, forget the "just answered" mark and put the approval back, so the card returns (the sheet isn't forced up again).
- Verified: unit test `failedAnswerKeepsTheApprovalAnswerable`.

### R8-i3: background and reconnect timing
- Repro 1: open a chat, background the app for a while (phone asleep), foreground.
- Repro 2: bridge down (or airplane mode) long enough for the backoff to reach 30 s, then bring it back.
- Actual:
  - Background left the socket open. A suspended app can't read it, it can come back as a zombie that never errors (the ping had no deadline), and while the bridge thinks a client is there it keeps reading screens and polling usage.
  - Foreground only refetched over REST. A socket waiting out its backoff stayed down for up to 30 s ("Reconnecting…", no live updates) with the network fine. Same after airplane mode off.
- Fix:
  - `.background` closes the socket (`AppStore.suspend`); `.active` reopens it at once and resyncs (`resume`), and the new socket's `hello` resyncs again, so frames missed in between are covered.
  - Foreground while reconnecting, or `NWPathMonitor` reporting the network back, opens a fresh socket now instead of after the backoff.
  - The WS ping gets a 10 s deadline, so a dead route surfaces as a drop within 25 s.
- Verified: sim against the live bridge (via proxy): in the background the app's `/ws` connection is gone (`lsof`), on foreground a new one opens with a resync. Unit tests `backgroundClosesTheSocketAndForegroundReopensIt`, `foregroundWhile*`, `networkComingBackReconnectsAtOnce`, `networkChangesWhileSuspendedDoNothing`.

### R8-i4: Files picker reads on the main thread
- Actual: `loadFiles` did `Data(contentsOf:)` for every picked URL on the main thread, and only then checked the 20 MB limit. Ten 20 MB files are 200 MB read while the UI waits; a 600 MB file is loaded in full just to be rejected (on a phone, a big enough file risks a jetsam kill).
- Fix: `ComposerAttachments.add(files:)` reads in a detached task, one file at a time in pick order, stops at 10, and `AttachmentProcessing.read` checks a non-image's size before reading (images may be bigger, they're downscaled).
- Verified: new opt-in `FilesPickerUITests` through the real Files picker (sim, mock bridge): 10 × 20 MB files all land in the tray, upload, and Send enables; a 600 MB `huge.zip` gets "huge.zip is over 20 MB." with nothing in the tray. Unit tests `oversizedFileIsRejectedWithoutReadingIt` (sparse 20 MB + 1 file), `smallFileIsReadWithItsType`, `pickedFilesArriveInOrderAndStopAtTen`.

### R8-i5: resync replaces the open chat
- Actual: `loadMessages` did `setPage` on every resync (foreground, reconnect, now twice per foreground).
  - Pages loaded by scrolling up were thrown away and `hasMore` reset, so the view jumped back to the last 50.
  - A message the socket delivered while the page request was in flight (e.g. the user's own prompt echo) was overwritten by the older snapshot and vanished until the next full load.
- Fix: `RelayState.mergeLatestPage`: if the page overlaps what's loaded, keep the older messages and their `hasMore`; keep loaded messages newer than the page's last; the page's copy wins for ids in both. No overlap (more than a page arrived while away) still replaces.
- Verified: unit tests `resyncKeepsOlderPagesTheUserScrolledTo`, `resyncKeepsAMessageTheSocketDeliveredMidRequest`, `resyncAfterALongGapReplacesTheChat`.

# bridge-harden report (round 5)

Scope: `bridge/` (except live-typing's `LiveReply*.swift`) and api.md's error/health sections.
`swift test`: 189 pass, 0 warnings (shared with live-typing's 25 `LiveReply*` tests, confirmed green
together before this commit).

## herdr socket

- **`/health` now reports herdr connectivity and uptime**: `{"ok":true,...,"herdr":"connected"|"unavailable","uptimeSeconds":N}`. `AgentMonitor` tracks whether its last `agent.list`/`workspace.list` round trip succeeded (`isHerdrReachable`); `/health` reads it, so the app (or a human) can tell "herdr is down" apart from "the bridge is down" at a glance.
- **REST already answered fast, verified it stays that way**: `HerdrClient`'s per-call `UnixSocket` fails immediately (`.unavailable`) when the socket file is gone (`ECONNREFUSED`/`ENOENT`), and every socket read has a timeout, so a hung herdr can't hang a request either. New tests prove both paths: `restIsFastNotHungWhenSocketIsGone` (socket never existed) and `restRouteAnswers503FastWhenHerdrDies` (killed mid-run, via the real router) — both assert well under the herdr timeout, not just "eventually returns".
- **Event-stream reconnect, proven by actually killing and restarting a fake herdr**: `FakeHerdr` only stopped its *listener*, not already-accepted connections, so a live `events.subscribe` socket never noticed a "restart" — a real gap in the test harness that would have silently hidden the very failure mode the brief calls out. Fixed `FakeHerdr.stop()` to drop accepted connections too, then added `eventStreamReconnectsAfterHerdrRestarts`: start the stream, kill herdr, bring a fresh `FakeHerdr` up on the same socket path, assert a second `resync` event arrives. Also `healthReflectsHerdrReachabilityAcrossARestart` for the `/health` side of the same scenario. Per the round-5 rules this uses `FakeHerdr`, never the user's real herdr.

## Transcript tailing

- **Rotation/truncation/deleted files**: already handled (`TranscriptTailer` resets its parser and offset when the file shrinks; dies cleanly on delete/rename/revoke) and already tested (`TailerTests`). Verified by reading, not re-tested.
- **Deleted cwd** (the `/tmp` wipe scenario): `TranscriptLocator.locate` already degrades to "no transcript" when `claudeProjects` doesn't exist (`try?` around `contentsOfDirectory`), and this is exercised incidentally by nearly every existing test (they all pass a nonexistent `claudeProjects` path and things still work). No change needed; confirmed by inspection.
- **Invalid UTF-8 / partial JSON lines**: `TranscriptParser.consume(line:)` already treats "can't parse as JSON" as "skip this line", including for genuinely invalid UTF-8 bytes (`JSONSerialization` fails, `try?` swallows it). Added `transcriptParserNeverCrashesOnGarbageLines`, which feeds 300 fully-random byte strings per transcript format straight into the parser.
- **Huge files (>50 MB), the one real gap found**: `/messages` read the *entire* transcript file into memory and parsed *every* message into an array on every cache miss, with no upper bound — a long-lived or runaway session could mean holding tens of MB of `Data` plus a large `[Message]` just to answer one page. Fixed: `AgentService.readBounded` only reads the file's last 64 MB once it's over that size (aligned to the next newline, so the first line read is never a truncated fragment), trading off very old history in that one session for a hard memory ceiling. `HugeTranscriptTests` covers the boundary logic directly and end-to-end with a real >64 MB synthetic JSONL file through `AgentService.transcriptMessages`, asserting it returns promptly and doesn't crash.

## Input validation

- **Body size limits**: Hummingbird already caps every `request.decode` at 2 MB (`context.maxUploadSize`) — but the bridge's own catch-all was swallowing that as generic `400 bad_request`, hiding the real `413`. Fixed `RelayRoutes.decode` to catch `NIOTooManyBytesError` specifically and answer `413 too_large`, consistent with the existing attachment-upload behavior. Attachments keep their own explicit 20 MB cap.
- **Keys validated against herdr's shape**: added `KeyNames` (single printable char, a named key like `esc`/`enter`/`f1`, a `modifier+key` combo, or a 1–2 digit menu row — matching what `ApprovalParser` itself ever emits) and wired it into `POST /agents/:id/keys`; a bad key is now `400 bad_request` instead of being forwarded to herdr untouched. `KeyNamesTests` includes a 2000-iteration fuzz pass.
- **Agent ids**: `agentId(context)` now rejects control characters and ids over 128 bytes before they reach herdr or any path-scrubbing regex, instead of trusting arbitrary percent-decoded input. `rejectsAdversarialAgentIds` covers both.
- **Fuzzing the parsers**: `FuzzTests.swift` — 500 random/adversarial screens each into `ApprovalParser.parse`/`.fallback`/`.step`, the codex/cursor `Picker`, and `InputBox`; 500 into `PathScrubber.scrub` (checked at a forced word-boundary, since the scrubber is intentionally conservative about *not* touching a sibling directory name); 1000 random names into `UploadStore.sanitize`, checked for no `/`, no `\`, never empty, within the length cap, and — the actual security property — that `dir.appendingPathComponent("<id>-<name>")` can never resolve outside `dir`.

## Security

- **Constant-time token compare**: already correct by inspection (walks every byte of both inputs, XORs into one accumulator, no early exit except the length check). Added a functional correctness test (`AuthTests`); a real timing measurement isn't reliable as a unit test — tried it, and this box running several concurrent `swift test`s alone produced a >40x noise floor, so a flaky assertion was removed rather than shipped.
- **Bind address**: confirmed live — `lsof` on the running bridge shows only `127.0.0.1:7878` and the Tailscale IP, nothing on `0.0.0.0`. This falls straight out of `Serve.run()`'s host list (`["127.0.0.1"]` plus, optionally, the real Tailscale IPv4) with no user input in the loop, so no code change was needed; verified rather than assumed.
- **Uploads**: 0600 permissions and 7-day expiry were already covered by `UploadsTests`. "Served only to the right agent id" is `find(id)`'s documented, intentional behavior (api.md: "looks the id up across all agents' uploads, so history keeps working if the agent's pane id changes") — the 16-byte random hex id is the actual access control, not the pane. Left as-is; flagging here rather than silently reinterpreting the contract.
- **No path leaks**: existing tests already checked a couple of routes for `/Users/`. Added `noRouteEverLeaksAnAbsolutePath`, which sweeps every GET route this test world can reach (including error bodies: 404s, a too-large body, an unknown workspace) for both `/Users/` and `/private/` in one place, using a fixture whose agent title (`"Refactor /Users/dev/shop-api/auth"`) is deliberately adversarial.

## Operations

- **Log rotation**: `~/.relay/relay.log` is launchd's `StandardOutPath`/`StandardErrorPath` — it just appends forever, nothing ever rotated it. Added `LogRotator`, a `Service` that checks the file's size every 5 minutes and, once it's over 10 MB, truncates it in place (`ftruncate` + `lseek` on the stdout/stderr fds themselves, not a rename) so the already-open fds keep writing correctly afterward instead of leaving a hole. Wired into `RelayApp`'s service group. `LogRotatorTests` verifies the truncate-in-place behavior on a real fd, including that a write immediately after rotation lands at the right offset.
- **Graceful shutdown on SIGTERM**: already handled by `ServiceGroup(gracefulShutdownSignals: [.sigterm, .sigint])` (swift-service-lifecycle). Not re-tested (that's the library's contract, not ours); confirmed present by inspection.
- **`/health` connection state + uptime**: see "herdr socket" above.
- **30-minute soak**: run against the live bridge (real `launchctl`-managed process, on 7878, with the app's own herdr connected) sampling RSS/CPU/`/health` every 5s. Restarted after coordinating with live-typing's kickstarts so the process being sampled didn't get pulled out from under the test; see results below.

## Consistency (error codes vs api.md)

Audited every `APIError`/`HerdrError`→HTTP mapping in the bridge against api.md: `not_found`, `bad_request`, `unauthorized`, `agent_blocked`, `agent_busy`, `control_refused`, `control_timeout`, `control_failed`, `unsupported`, `too_large`, `herdr_unavailable`, `herdr_timeout`, `herdr_error` all matched already. The one drift found and fixed is the `NIOTooManyBytesError` → `400` bug above (should always have been `413 too_large`). Documented the new `/health` shape and the 2 MB/503 behavior in api.md (staged only my own hunks with `git add -p`, leaving live-typing's "Live reply" section and websocket example line for their own commit).

## Soak test results

<!-- filled in after the run -->

## Left open

- Huge-transcript handling trims to the newest 64 MB rather than paginating a multi-GB file properly; revisit if a real session ever gets that large.
- No dedicated timing-attack test for the token compare (see Security above) — verified by code inspection instead.
- `git add -p` was used on api.md and shared source files (`AgentMonitor.swift`, `RelayApp.swift`, `Models.swift`) to avoid sweeping up live-typing's in-flight work; live-typing confirmed those specific hunks build and pass tests before this commit.

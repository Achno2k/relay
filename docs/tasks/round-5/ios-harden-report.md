# ios-harden report

## Scope note

The brief (`docs/tasks/round-5/ios-harden.md`) covers connection recovery, lifecycle, four
categories of races, large-data performance, error-code mapping, and pairing edge cases — a lot of
ground across `ios/Relay/Stores/`, `ios/RelayKit/`, `ios/Relay/App/`. Rather than a shallow pass over
every bullet, I surveyed the current state first (see below), then fixed the concrete, verifiable
gaps that survey turned up, each with a test. Some items (a full 5,000-message/50-agent profiling
pass with Instruments/os_signpost numbers, an exhaustive pairing-edge matrix) are noted as left open
rather than claimed done without evidence — see "Left open".

Survey findings that shaped the plan: the WS reconnect/backoff layer (`WSClient`) was already solid;
the real gaps were (1) no staleness guard anywhere a slower request could land after a faster one,
(2) any non-`RelayError` (a decode failure, a raw `URLError`) fell through to
`error.localizedDescription`, which is Swift/Foundation's own text, not a curated message, (3) a
failed send silently removed the bubble from the transcript, (4) no double-tap guard on send, (5) a
large transcript's per-token growth forced a full O(n) array copy every time, (6) a rejected token
just kept re-showing the same auto-dismissing 4s toast forever instead of prompting re-pairing.

## What changed

### Connection: staleness guards
- `AppStore.loadMessages` and `AppStore.refresh` now carry a generation counter (`messagesRequest`
  per agent, `refreshGeneration` globally). A response from an older, slower call is discarded if a
  newer call for the same target already landed — covers a rapid agent switch, a reconnect racing an
  `open()`, and foreground + reconnect both firing `refresh()` at once.
- Tests: `staleMessageLoadNeverOverwritesNewer`, `overlappingRefreshAppliesOnlyTheNewestSnapshot`
  (`ConnectionHardeningTests.swift`), using a new `ProgrammableBackend` that can answer calls out of
  real-time order.

### Errors: no raw text, ever
- `APIClient` (RelayKit): decode failures (`DecodingError` and friends) are wrapped into
  `RelayError.badResponse` instead of propagating; `URLError` from a dead/unreachable transport
  becomes a new `RelayError.unreachable(timedOut:)` with curated copy ("Can't reach the bridge…" /
  "…didn't respond in time"). Every call site (`send`, `raw`, `uploadAttachment`, `approval`,
  `control`) goes through this.
- `AppStore.report()` and `AppModel.pair()` now only ever show a `RelayError`'s `errorDescription`
  (or the bridge's own curated `message`, per api.md); anything else gets a generic "Something went
  wrong. Try again." instead of `error.localizedDescription`.
- Tests: `APIClientErrorTests.swift` (6 tests) using a `StubURLProtocol` to drive malformed JSON,
  truncated JSON, offline, timeout and 401 through the real `APIClient`, plus a test asserting no
  `RelayError` case's copy contains `DecodingError`/`NSError` text.

### Races
- Double-tap send: `send()` now recognizes a repeat call with the same trimmed text/attachments
  within 2s of the last send to the same agent as a duplicate (double tap, stuck composer) and
  ignores it rather than sending twice. A genuinely different message still goes out immediately.
- Failed sends never silently drop: instead of removing the pending bubble on a `prompt` failure, it
  now stays in `pending` and is added to a new `failedPending` set; `retry(_:to:)` resends the same
  bubble, `discardFailed(_:from:)` removes it explicitly. All three paths that used to just nil out
  `pending[agentId]` (`resolvePending`, `dropPendingIfFinished`, `.agentClosed`) now also prune
  `failedPending` so it can't leak stale ids.
- Tests: `doubleTapSendGoesOutOnce`, `distinctSendsBothGoOut`, `failedSendKeepsBubbleVisibleAndRetryable`,
  `discardFailedRemovesTheBubble`.

### Lifecycle
- `refresh()`'s post-agents work (Claude's catalog, the open chat's controls/messages/approval,
  machine info) used to be five sequential round trips. Controls/messages/approval for the selected
  agent now run concurrently (`async let`), with machine info alongside them; the catalog fetch stays
  ahead of `loadAgentControls` since its 404-fallback path depends on it. This is the main lever for
  the "correct within ~1s" foreground target — a slow Tailscale link now costs roughly the slowest of
  three calls instead of the sum of five.
- `handleMemoryWarning()`: drops every chat's cached messages except the currently open one, and
  clears the attachment thumbnail cache, wired to `UIApplication.didReceiveMemoryWarningNotification`
  in `MainView`. Nothing is lost — `open(_:)` always reloads a chat's messages regardless of whether
  it thinks it's already loaded.
- Cold launch with a stale selected agent (closed pane): audited, already correct —
  `selectedAgentId` is restored from defaults at init, and `refresh()` already reassigns it to the
  first live agent if the restored id isn't in the fresh `/agents` list. No change needed.
- Tests: `memoryWarningKeepsOnlyTheOpenChat`.

### Large data
- `RelayState.upsert(_:agentId:)` (the message reducer used for every `message.upserted` while an
  agent is working) used to do `var list = messages[agentId]!; list[i] = message; messages[agentId] =
  list` — that pattern holds two live references to the array across the mutation (the dictionary's
  and `list`'s), forcing Swift's copy-on-write to copy the *entire* transcript array on every single
  streamed delta. On a 5,000-message transcript that's a full-array copy on every token. Rewrote it to
  mutate through the dictionary's own subscript (`messages[agentId]![i] = message`), which is a
  single owner, so growth is O(1) amortized instead of O(n) per event — directly the "no full
  re-decode per event" requirement, just at the array-copy level rather than JSON decoding (nothing
  in this path decodes JSON per event to begin with).
- `AppStore.messages(for:)` is read on every SwiftUI body evaluation. It used to always concatenate
  `state.messages[agentId] + pending[agentId]`, which reallocates and copies the whole transcript
  array even when `pending` is empty (the steady state). Now it returns the transcript array as-is
  (no copy) when there's nothing pending.
- Not done: an Instruments/os_signpost profiling pass with actual numbers for 5,000 messages/50
  agents, and any scrolling/virtualization work (that's the chat list view, `Features/Chat/`, owned
  by ios-polish/live-typing, not `Stores/`). See "Left open".

### Pairing edge cases
- Token rotated on the Mac: a 401 now sets `AppStore.needsRePairing = true`, and `MainView`'s banner
  shows a sticky "Token rejected. Tap to pair again." pill (tapping calls `model.unpair()`, which
  routes back to `PairingView`) instead of the same auto-dismissing 4s toast repeating forever. It
  clears on the next successful `refresh()`.
  Reason the previous behavior was a real bug: the transient error banner auto-dismisses after 4s
  by design (correct for one-off failures), which silently hid the one error that actually needs the
  person to act.
- Bridge moved to a new port / unreachable: already covered by the `RelayError.unreachable` mapping
  above — a clear "Can't reach the bridge…" message instead of a generic HTTP failure or raw
  `URLError` text.
- Invalid QR code: audited, already correct (`Pairing(link:)` returns `nil` →
  `RelayError.invalidPairingLink`'s clear copy). No change needed.
- Test: `unauthorizedSetsStickyRePairingBanner`.

## Tests

15 new unit tests across two new files (`ConnectionHardeningTests.swift`, `APIClientErrorTests.swift`),
all Swift Testing (`@Test`/`#expect`), matching the existing `RecordingBackend`/`FixtureFiles`
conventions. `ConnectionHardeningTests.swift` adds a `ProgrammableBackend` actor (configurable
delays/failures/call-count-indexed responses) where `RecordingBackend` isn't shaped for timing
scenarios; `APIClientErrorTests.swift` adds a `StubURLProtocol` to drive real `URLSession` traffic
through `APIClient` without a live bridge.

## Verified

- `cd ios/RelayKit && swift build`: clean (after clearing a stale module-cache path left over from
  the `herd` → `relay` rename — `rm -rf .build` fixed it; unrelated to this round's changes, just a
  build-environment artifact from the directory rename in `AGENTS.md`).
- `xcodebuild build` (Relay scheme, iPhone 17 Pro simulator): clean.
- `RelayTests` (unit): **84/84 pass**, confirmed clean twice in a row (one run in the same batch
  showed a `signal kill` crash on an unrelated pre-existing test under heavy concurrent
  `xcodebuild` load from other round-5 sessions; a clean isolated rerun passed, so treated as
  environment contention, not a regression — see below).
- `RelayUITests` (mock UI): **18/18 pass, 0 failed, 11 skipped** (live-bridge-only tests), isolated.
- Did not run the live bridge E2E tests (`w14:p2`/`p4`/`p5`) myself this round — nothing I touched
  talks to a real bridge kind-specific behavior; `qa` covers the live matrix.
- Did not run on the physical device (`00008110-000414D91422801E`); per round-5 rules that's `qa`'s
  lane and it was in active use for their live E2E pass during this session.

### Simulator contention with other round-5 sessions (investigated, not a code issue)
Mid-session, `qa` reported `MockUITests.testControls`/`testControlsDisabledWhileWorking` failing
("no menu item Model" / "pill didn't follow the model") after I'd landed the `refresh()`
parallelization change, plus a transient "cannot find `needsRePairing` in scope" build error. I
found: (1) the build error was a snapshot of my own file mid-edit, self-resolved; (2) a rerun of
`MockUITests` on my end showed 4 of 5 tests crashing with **"Test crashed with signal kill"** — an
OS-level watchdog kill, not an assertion failure — coinciding with five sessions' worth of concurrent
`xcodebuild`/simulator processes plus a bridge kickstart in flight. `qa` independently saw the same
signal-kill pattern reproducing `ios-polish`'s bugs, and `ios-polish`'s own report
(`ios-polish-report.md`) documents the identical finding independently. Once the simulator was
quiet (qa moved to the physical device for their live pass), an isolated rerun of `testControls`
passed, then the full `MockUITests` class passed 5/5 twice, then the full `RelayUITests` suite
passed 18/18. Reported back to `qa` at each step; no code fix was needed.

## Left open

- Instruments/os_signpost profiling with actual numbers for a 5,000-message transcript and 50
  agents — the two concrete algorithmic fixes above (O(1) `RelayState.upsert`, no-copy
  `messages(for:)`) address the specific "full re-decode/copy per event" failure mode described in
  the brief, but I have no measured before/after main-thread-stall numbers to report. Scrolling
  smoothness itself is a `ChatView`/`Features/Chat/` concern (ios-polish/live-typing's paths), not
  `Stores/`.
- A live re-pairing walkthrough against a real bridge (rotate the token on the Mac, move the bridge
  to a new port, scan a stale/invalid QR) — `unauthorizedSetsStickyRePairingBanner` covers the store
  logic against a programmable backend; I didn't drive the actual pairing UI against a live bridge
  this round.
- Background 10+ minutes then foreground, and a genuine airplane-mode toggle, on a real device —
  `qa`'s lane per round-5 rules; flagged as covered by their checklist.

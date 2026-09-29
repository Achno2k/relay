# Round 9 mm-qa report

## What changed
- `docs/qa/round9.md`: test plan, results, bugs R9-1 to R9-4.
- `ios/RelayUITests/Round9UITests.swift`: 22 mock UI tests (`-mockVM`, `-mockOffline`, `-mockDrop`, `-mockOnlineAfter`, `mock-` pair links). They cover All vs one machine, filter across relaunch, Now/approvals across machines, same pane id on two machines, offline/drop/come back, offline chat can't be prompted, add (plus bad token), rename, remove (cancel, confirm, fallback to All), re-pair (replace, different machine refused), Machines screen, New chat machine picker, single-machine (no tags), 44 pt targets and AX sizes.
- `ios/RelayUITests/Round9LiveUITests.swift`: 8 live tests, skipped unless `TEST_RUNNER_RELAY_R9_MAC_LINK` / `TEST_RUNNER_RELAY_R9_VM_LINK` are set. Each states the bridge state it needs; the runner stops or starts the 7881 bridge between or during tests.
- `ios/Relay.xcodeproj` regenerated for the new test files. No app code touched.

## Tests (iPhone 17 Pro, Simulator in front)
- Baseline at 0365559: `RelayTests` 137 passed, mock `RelayUITests` 42 run, 8 skipped, 0 failed.
- `Round9UITests` 22/22 on HEAD after the fixes.
- Full suite on b032844: 242 tests, 0 failures. `RelayTests` 170/170; UI 72 run, 56 passed, 16 skipped (the 8 old live tests plus the 8 Round9 live tests, which need env). No regressions vs the baseline.
- Live (7878 + Go bridge on 7881 as `test-vm`, e2e agent `w14:p2` only):
  - pass: migrate + add a second machine, prompt through Test VM, offline then back, changed id → re-pair needed, rotated token → re-pair, rename survives relaunch, migrate while offline (after R9-4), attachment routed only to 7881.
  - One unreproduced failure in "offline then back" (agents slow to reappear after going online); 3 reruns passed.

## Bugs
| id | sev | owner | status |
|---|---|---|---|
| R9-1 remove confirm popover mis-anchored | P3 | mm-ui | verified 6eabaed |
| R9-2 prompt in an offline machine's chat lost | P2 | mm-ui | verified 6eabaed |
| R9-3 "Reconnecting…" for an offline machine | P3 | mm-data | verified 105c653 |
| R9-4 offline-at-launch migration never recovers | P2 | mm-data | verified 679f940 |

## How to run
- Mock: `xcodebuild test -scheme Relay -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -only-testing:RelayUITests/Round9UITests`.
- Live: start a test bridge on 7881 (`scripts/second-bridge.sh`, or `relay serve --port 7881 --local-only` with `RELAY_HOME`/`RELAY_MACHINE_ID=test-vm`/`RELAY_MACHINE_NAME="Test VM"` for a token that survives restarts). Pass both pair links as `TEST_RUNNER_RELAY_R9_MAC_LINK` / `TEST_RUNNER_RELAY_R9_VM_LINK`, then run `Round9LiveUITests/test1…` through `test8…` in order, setting up each test's bridge state from its doc comment.

## What's left
- L7 not run: one bridge up with its herdr unavailable ("online, no live agents").
- The live tests are phased by hand (the runner restarts 7881), so they're not one-shot CI.
- `bridge-go/bin/relay` (untracked) is stale; the live runs used a fresh `go build`.

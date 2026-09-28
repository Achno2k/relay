# Round 8: ios-bugs report

Commits: `3391a48` (R8-i1 to R8-i6), `a0224f4` (R8-i7). Details, repros and evidence per bug are in `docs/qa/round8.md` (iOS section). No API change; nothing outside `ios/` besides the QA doc and this report.

## What changed
- R8-i1 (P1): a rotated token ended in endless "Reconnecting…". `WSClient` now reports a refused upgrade (`ConnectionEvent.rejected(status:)`); `AppStore` asks REST, which says `401`, and shows the re-pair banner. It works with the Swift bridge's bare `400` and the Go bridge's `401` (R8-18).
- R8-i2 (P2): a failed approval answer brings the card back instead of stranding the blocked agent.
- R8-i3 (P2): background closes the socket (`AppStore.suspend`). Foreground reopens it and resyncs (`resume`). Foreground while reconnecting, or the network coming back (`NWPathMonitor`), retries at once instead of after a backoff of up to 30 s. WS ping has a 10 s deadline.
- R8-i4 (P3): Files picks are size-checked before reading and read off the main thread, in pick order, capped at 10.
- R8-i5 (P3): a resync merges the latest page into the open chat. It no longer drops scrolled-up history or a message that landed mid-request.
- R8-i6 (P2): failed sends show "Not sent. Tap to retry." with Retry/Delete in the context menu and as VoiceOver actions. `AppStore` had the logic since round 5; no view used it.
- R8-i7 (P2): approval options wrap in full at large text sizes (was `.lineLimit(2)`).

## Tests
- No Swift bridge tests to port (app only).
- Unit (`RelayTests`): 121 → 137. New suite `Round8Tests` (16 tests), with a `SocketScriptBackend` that drives the socket per test.
- UI (`RelayUITests`):
  - `Round8UITests`, 3 tests, mock: failed-send retry, failed-send delete, approval options at AX3.
  - `FilesPickerUITests`, 2 tests, opt-in (`TEST_RUNNER_RELAY_FILES_QA=1`, seeded Files folders): real Files picker with 10 × 20 MB and a 600 MB file.
  - `A11yAuditUITests`, opt-in (`TEST_RUNNER_RELAY_SHOTS`): audit of chat, approval, New chat and Usage at default size and AX3. It reports and never fails.
- The tests fail without their fix where that can be run: R8-i7's test failed with the old limit. R8-i1 and R8-i3 were reproduced on a HEAD build in the sim before the fix.

## How it was verified
- Sim (iPhone 17 Pro, iOS 26.4) against the live bridge through a local proxy on 7881 that can play "bridge down" and "rotated token" (the live bridge was never restarted, its token never touched):
  - R8-i1: HEAD build stuck on "Reconnecting…" with only `/ws` retries in the proxy log; the fixed build showed the re-pair banner within 1 s.
  - R8-i3: in the background the app's `/ws` connection is gone (`lsof`); foreground opens a new one and resyncs.
- Real Files picker, sim + mock: 10 × 20 MB all upload and Send enables; 600 MB rejected by size with nothing in the tray.
- Live* UI suites (slot from qa-bridge; created panes `w14:pX`, `w14:pY` closed after):
  - Swift bridge 7878: 11/11. `LiveNewChatTests` failed once (a menu tap in the test didn't land), then passed on rerun.
  - Go bridge 7880 (go-parity's instance, master 8bf8366): 11/11 on the first run. Per-test results sent to go-parity.
- Clean worktree of `a0224f4`: `RelayTests` 137/137. Mock `RelayUITests` 35/35 passed; the live and opt-in suites skip without their env.

## What's left
- The closed sidebar shows up in XCUITest's tree and in the audit. On iOS 26 XCUITest also lists the peek overlay, which is `.accessibilityHidden` the same way, so this isn't evidence of a bug. It needs a VoiceOver swipe on the device.
- Not covered in the sim: real airplane mode (NWPathMonitor path) and a real phone sleeping in the background. The unit tests cover the logic, and the proxy covered a dead bridge. Worth one manual pass on the iPhone.
- `LiveNewChatTests` is flaky: its menu taps sometimes don't land. It also assumes `/controls?kind=claude` has `defaultModel`; both bridges omit it when Claude has no saved model. The app already treats it as optional ("Default"). go-parity is raising the api.md "always sent" wording.
- Nice to have, not done: keep in-flight sends alive briefly in the background (`beginBackgroundTask`), so a send made right before locking the phone doesn't come back as "Not sent".

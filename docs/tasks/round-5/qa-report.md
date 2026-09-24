# qa report (round 5)

Read-only session; owns `docs/qa/` only.

## What changed
- Wrote `docs/qa/bugs.md` (2 bugs filed, 1 closed no-repro) and `docs/qa/summary.md` (full coverage writeup, go/no-go).

## What was verified and how
- Bridge: `swift test` (140/140, 17 suites) plus live API probing against the running bridge and the real `w14:p2`/`p4`/`p5` e2e agents — auth boundaries, control validation errors, attachment limits (20 MB exact/over, empty body, 10/11-file cap, unknown id), a real end-to-end approval (numbered options + free-text row, answered, transcript confirms), stop-mid-tool (esc while a Bash loop was running, confirmed the interruption marker in the transcript), `relay pair` output, token file mode, `/machine`. Bridge survived a release rebuild + `launchctl kickstart` during this session (health check clean after).
- iOS unit tests: `RelayTests` 64/64 on the iPhone 17 Pro simulator.
- iOS UI tests, simulator: hit real instability early on from 5 concurrent Claude sessions building/testing against the same simulator at once (watchdog SIGKILLs, one build break that was ios-harden's own mid-edit snapshot). Coordinated with ios-harden/ios-polish over herdr rather than filing false positives; both independently reproduced the same contention pattern and confirmed clean full-suite runs once isolated. ios-harden's final isolated run: 18 passed, 0 failed, 11 skipped (live-only).
- iOS UI tests, physical device (id `00008110-000414D91422801E`): got the lead's go-ahead, verified `build-for-testing` first as asked, then ran `LiveE2ETests` (6/6), `LiveKindControlsTests` (3/3, covering pi and codex), `LiveNewChatTests` (1/1) against the real bridge over Tailscale and the real e2e agents. Reinstalled the normal build afterward and handed the phone back. One non-reproducing flake (device keyboard-focus timing in a test helper, not an app bug).
- **Live typing**, after it landed (`43277d4`): `swift test` 205/205 (29 suites), iOS unit 90/90, `MockUITests` 5/5 (no regression from the `ChatView`/`LiveReplyView` hook). Live-verified on a dedicated throwaway agent (`w14:pJ`, started and closed just for this) rather than the shared `w14:p2`, per the contention lesson learned earlier in this session: `LiveReplyUITests` passes on the simulator, and a raw WebSocket probe alongside a live prompt showed 18 clean `reply.live` frames (`seq` 1→18, no gaps), text growing 101→1339 chars, and `text: null` landing once `message.upserted` had. One UX note (not a bug, matches documented tail-scroll behavior): the preview can visibly shrink once the reply scrolls past a screenful, before the real message replaces it.

## Bugs filed
- QA-1 (P3, ios-polish): sidebar filter menu timing — closed, no repro (simulator contention).
- QA-2 (P3, bridge-harden): attachment filename sanitizing drops the segment before a `/` instead of replacing it with `-`, per `docs/api.md`. Confirmed no path-traversal risk (`../../` collapses safely to the last component). Open — either fix the code or the doc.

No P0/P1s found across bridge, controls, approvals, attachments, or stop/pairing flows.

## Left open
- **Background/foreground** and **airplane mode**: not exercised. This session had no interactive simulator/device UI-automation tool beyond XCUITest (no `ios-simulator`-equivalent MCP tool was available to it), and neither is covered by the existing UI test suite. Recommend a manual pass by whoever has that tooling, or a small XCUITest addition (`XCUIDevice.shared.press(.home)` + relaunch for backgrounding; airplane mode needs a manual device toggle).
- **Re-pairing after token rotation** (`needsRePairing`, added by ios-harden this session): not yet exercised end-to-end; worth a dedicated check once that lands solidly.
- Attachment limits were verified at the bridge API layer (curl) rather than by driving the app's own attachment picker with real 10-file / 20 MB payloads.

## Go/no-go
Go for daily use. See `docs/qa/summary.md` for the full writeup.

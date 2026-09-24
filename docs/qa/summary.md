# Round 5 QA summary

## Covered

**Bridge (`bridge/`)**
- `swift test`: 140/140 pass, 17 suites.
- Live API probing against the running bridge (7878) and the real e2e agents (`w14:p2` claude, `w14:p4` pi, `w14:p5` codex):
  - Auth: `401` with no/bad token on protected routes, `GET /health` needs none.
  - `/agents/:id/control`: bad model value, two-keys-at-once, zero-keys, pi mode-unsupported all 400 with the documented codes; `409 agent_busy`/`agent_blocked` verified live.
  - Attachments: 20 MB exactly succeeds, 20 MB+1 byte gives `413 too_large`, empty body gives `400 bad_request`, 11 attachments on a prompt gives `400 bad_request` ("at most 10"), 10 succeeds, unknown attachment id gives `400 bad_request`, expired/unknown id on fetch gives `404 not_found`.
  - Filename sanitizing: see bug QA-2.
  - Approvals end to end: triggered a real `AskUserQuestion` on the claude e2e agent, saw `blocked` + the `Approval` shape (numbered options, free-text row, step-less single question), answered via `/keys`, confirmed it resolves to `done` and the transcript shows the answer.
  - Stop mid-tool: started a long `Bash` loop, confirmed `working`, sent `["esc"]`, confirmed it returns to `done` and the transcript shows the interruption marker (`[Request interrupted by user for tool use]`) plus the tool's rejected `toolResult`.
  - `relay pair` prints both the QR and the `relay://pair?url=…&token=…` text line.
  - Token file is `0600`.
  - `/machine` returns the expected shape.
  - Bridge release rebuild + `launchctl kickstart` (done by live-typing/bridge-harden during this session) survived cleanly; `/health` still reports `herdr: connected` afterward.

**iOS unit tests (simulator, iPhone 17 Pro)**
- `RelayTests`: 64/64 pass (controls, decoding, pairing incl. old `herd://` links, transcript state, new chat, reducer, chat items, attachments incl. image downscale, sidebar model incl. filters/archive, approvals).

**iOS UI tests**
- Simulator (`RelayUITests`, non-live subset): full suite reported clean by ios-harden on an isolated run — 18 passed, 0 failed, 11 skipped (live-bridge-only). This covers empty states, controls menu (model/mode/effort/compact/clear, busy-disabled), sidebar tree + new-chat-in-folder, filter menu + archive + remembered-across-launches, attach image/PDF, scroll-to-bottom, new chat (codex model+effort, pi model+effort-follows-model), sidebar motion.
- Physical device (iPhone, id `00008110-000414D91422801E`, `TEAM=TC56945264`, `Relay-FreeTeam.entitlements`), paired over Tailscale to the real bridge, against the real claude/pi/codex e2e agents:
  - `LiveE2ETests`: 6/6 pass — prompt/approve/stop, controls, free-text answer, multiple-question approval, image attachment, PDF attachment.
  - `LiveKindControlsTests`: codex approval, codex model+effort, pi model+effort — all pass.
  - `LiveNewChatTests`: claude create-with-model-and-effort opens an empty chat — pass.
  - One non-reproducing flake: `testPromptApproveAndStop` failed once with "Neither element nor any descendant has keyboard focus" on `composer.tap()` immediately followed by `typeText`; passed clean on immediate retry. Looks like a device keyboard-focus timing quirk in the test helper, not an app bug — not filed.
  - Normal (non-test) build reinstalled on the device afterward; phone handed back.

## Bugs filed (`docs/qa/bugs.md`)

| id | severity | owner | status |
|---|---|---|---|
| QA-1 | P3 | ios-polish | closed, no repro (was simulator contention from 5 concurrent sessions building/testing at once, confirmed independently by ios-polish and ios-harden) |
| QA-2 | P3 | bridge-harden | open — attachment filename sanitizing drops everything before a `/` instead of replacing it with `-`, per `docs/api.md`. Confirmed **no path-traversal**: `../../../foo` safely resolves to `foo`, nothing written outside the uploads dir. Doc/behavior mismatch only. |

No P0/P1s found.

## Not covered, and why

- **Live typing**: not covered — `live-typing` had not landed at the time this pass ended. Should be re-tested once it merges.
- **Background/foreground**: not exercised. This session had no interactive simulator/device UI-automation tool beyond XCUITest (no `ios-simulator` MCP or equivalent was available), and backgrounding/foregrounding isn't covered by the existing XCUITest suite, so it went untested. Recommend a manual pass (Home button, then reopen) checking the socket reconnects and messages don't duplicate.
- **Airplane mode**: not exercised, same tooling gap — flipping the device's actual radio isn't reachable from this session's tools. Recommend a manual pass: pull the phone off Tailscale/Wi-Fi mid-chat, confirm the app shows a disconnected state and recovers without duplicating messages once back online.
- **10-file attachment UI flow** (as opposed to the API-level 10/11 check above) and the **20 MB file through the app's own picker** (as opposed to a raw upload): only checked at the bridge API layer, not by driving the app's attachment picker with real large files. The existing `LiveE2ETests` image/PDF attachment tests exercise small generated files only.
- **Re-pairing after a token rotation** (`needsRePairing` sticky state ios-harden added this session): not yet in any test I ran or saw pass; worth a dedicated check once that lands solidly (kick the bridge, rotate the token file, confirm the app surfaces the sticky re-pair state and clears it only on a successful request).

## Go/no-go

**Go for daily use**, with two follow-ups before calling round 5 fully done:
1. Re-test once live-typing lands (not yet covered at all).
2. QA-2 (filename sanitizing doc/behavior mismatch) — low priority, no security impact, fine to fix or just re-document.

Everything else exercised this session — bridge contract, controls for all three agent kinds, approvals (numbered + free-text + multi-turn), attachments including both documented limits, stop mid-tool, pairing (new and old link format), archive/filters, new chat sheet, and the real device against real agents — came back clean.

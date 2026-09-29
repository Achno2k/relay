# Round 9 QA: multiple machines

Owner: mm-qa. Brief: `docs/tasks/round-9/README.md`. Contract: `docs/api.md` ("Multiple machines").

## Test plan

Simulator: iPhone 17 Pro, Simulator app open, window in front. UI tests never run alongside another session's.

### Mock UI tests (`ios/RelayUITests/Round9UITests.swift`)
Mock has two machines: "Mock MacBook Pro" (laptop, macOS) and "Mock VM" (desktop, Linux), with one pane id on both.

| # | Case | Pass when |
|---|---|---|
| M1 | All is the default | Capsule reads "All machines"; agents from both machines listed; project sections carry a machine tag |
| M2 | Pick one machine | Only its agents; capsule shows its name; checkmark moves; back to All restores both |
| M3 | Now and approvals span machines | In All, Now/approval rows from both machines, subtitle names the machine |
| M4 | Same pane id on two machines | Each row opens its own chat (title and messages differ); selection survives relaunch on the right machine |
| M5 | Offline machine | Its dot says offline (VoiceOver "Machine: Mock VM, offline"); its agents stay, greyed "offline"; other machine keeps working (send a prompt) |
| M6 | Add machine | "Add machine…" opens the pairing flow; paste link adds a third machine; it shows in the menu and Machines screen |
| M7 | Rename | Machines screen rename changes menu entry, capsule and section tags; survives relaunch |
| M8 | Remove | Confirm dialog; cancel keeps it; confirm drops the machine and its agents; last-used selection falls back to All |
| M9 | Re-pair | Re-pair a known machine with a new link replaces the entry (no duplicate) |
| M10 | Machines screen | Rows show status, OS, bridge version |
| M11 | New chat in All | Asks for the machine, defaults to the last used; created agent lands on that machine |
| M12 | Single machine | With one machine paired, no machine tags on section headers |
| M13 | A11y | 44 pt targets on menu and Machines rows; labels; AX sizes don't clip rows |
| M14 | Offline machine's agent | Can't be prompted (composer disabled or clear error), no crash |

### Migration
| # | Case | Pass when |
|---|---|---|
| G1 | Old single pairing + live 7878 | App starts with the one machine under its real `/machine` id; selected chat, seen, archive, filter kept |
| G2 | Old pairing, bridge offline at first launch | Kept under a provisional id; after the bridge is back, fixed up to the real id with no duplicate |
| G3 | Unit tests (mm-data) | Keying, pairing and defaults migration, per-machine state: read and run them |

### Live (e2e agents only: `w14:p2` claude, `w14:p4` pi)
- Mac bridge 7878 plus `scripts/second-bridge.sh` (7881, "Test VM", same herdr).
- L1: both show in the menu, online; same e2e agents appear under both, keyed apart.
- L2: prompt the e2e claude via the Test VM entry; reply lands in that chat only.
- L3: stop the 7881 bridge; Test VM goes offline, 7878 unaffected; restart, it reconnects.
- L4: re-pair 7881 with a new token; entry replaced.
- L5: migration G1 against 7878.
- L6: restart 7881 under another `RELAY_MACHINE_ID`; that entry goes `needsRePair`, never merges; 7878 unaffected.
- L7: 7881 with herdr unavailable shows online with no live agents, not offline (if a fake socket is easy).
- L8: attachment picked in a Test VM chat goes to 7881 only (bridge log / uploads dir).

### Regression
- Baseline at 0365559 (iPhone 17 Pro): `RelayTests` 137 passed; mock `RelayUITests` 42 run, 8 skipped, 0 failed.
- `RelayTests` (full) and the mock `RelayUITests` (all but `Live*`), no new failures vs master.

## Bugs

| id | severity | owner | status | title |
|---|---|---|---|---|
| R9-1 | P3 | mm-ui | open | Remove-machine confirm popover points at the middle of the form, not at "Remove Machine" |
| R9-2 | P2 | mm-ui | open | Prompt sent in an offline machine's chat vanishes: no bubble, no error, text gone |
| R9-3 | P3 | mm-data | open | Chat banner says "Reconnecting…" for a machine the menu shows as offline |

Details below, one section per bug: repro, expected, actual, fix, how verified.

### R9-1: Remove confirm popover anchored to the form
- Repro: `-uitest -mock -mockVM`, Manage machines…, Mock VM, Remove Machine.
- Expected: the popover points at the Remove Machine button (or a bottom action sheet).
- Actual: it floats over Name/Status with its arrow pointing at the Name footer. `.confirmationDialog` sits on the whole `Form` in `MachinesView.swift` (~line 157), so iPhone anchors it to the form's centre. Screenshot: `round9-remove-confirm.png`.
- Test: `Round9UITests.testRemoveMachine` (passes; this is visual).

### R9-2: Prompt to an offline machine's chat is lost
- Repro: `-uitest -mock -mockVM -mockDrop mock-vm 6`. Wait for the Mock VM rows to read offline, open "Rotate TLS certificates" (VM `w2:p1`, never opened before), type "should not send", tap Send.
- Expected (api.md: offline agents "aren't live and can't be prompted"): the composer is disabled with an offline note, or the prompt shows as a "not sent" bubble with retry.
- Actual: the composer clears and nothing appears. The chat stays on its loading spinner forever (messages can't load), and the failed pending bubble `AppStore.send` keeps is hidden behind that spinner. The text is gone for the user.
- Test: `Round9UITests.testOfflineAgentCantBePrompted` (fails until fixed). Screenshots `round9-offline-typed.png`, `round9-offline-send.png`.

### R9-3: "Reconnecting…" for an offline machine
- Repro: as R9-2; the chat's banner reads "Reconnecting…" while the machine menu says "Mock VM · Offline".
- Expected: the banner matches the machine status, e.g. "Mock VM is offline" (and "Reconnecting…" only for `connecting`).
- Actual: `store.connection` is `.reconnecting` for both. Owner mm-data (`MainView` banner / `store.connection`); mm-ui if the copy lives in a Feature view.

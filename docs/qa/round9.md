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

## Results (iPhone 17 Pro)
- Mock: `Round9UITests` 22/22 on 6eabaed + 105c653.
- Full suite on 0a22e10: 232 tests, `RelayTests` 168/168, existing mock UI tests all pass, 8 live skipped. The only failures were R9-2's test (fixed since) and a timing bug in my own test (fixed).
- Live, `Round9LiveUITests` against 7878 plus a Go bridge on 7881 (`test-vm`, temp home; `scripts/second-bridge.sh` also checked: starts, prints the link, removes its home on Ctrl-C):
  - test1 migrate + add second machine (G1, L1): pass. The old pairing came back under the Mac's `/machine` id; the e2e agent shows once per machine.
  - test2 prompt through Test VM (L2): pass, e2e claude replied (`R9VMNHTV`). Both bridges share one herdr, so routing is shown by test3/test4 (VM-keyed chats follow 7881's state), not by the reply.
  - test3 VM offline then back (L3): pass 3 of 4. One early run: Test VM read online but its agents hadn't shown within ~20 s of swipes; not reproduced in 3 reruns.
  - test4 changed machine id (L6): pass. Re-pair needed, no `other-vm` entry, Mac online.
  - test5 rotated token, then re-pair (L4): pass. Re-pair needed, then online, still 2 machines.
  - test6 rename survives relaunch: pass.
  - test7 migrate while offline (G2): failed (R9-4); passes 2 of 2 on 679f940.
- test8 attachment in a Test VM chat (L8): pass. 7881's `uploads/` went 0 → 2 files, 7878's stayed at 4, and the agent read the PDF's word. So VM-keyed calls go to the VM's bridge only (L2 routing too).
- Not run: L7 (herdr unavailable on one bridge).

- Final full suite on b032844: 242 tests, 0 failures (`RelayTests` 170/170, UI 56 passed, 16 live skipped).

## Bugs

| id | severity | owner | status | title |
|---|---|---|---|---|
| R9-1 | P3 | mm-ui | verified (6eabaed): popover points at Remove Machine | Remove-machine confirm popover points at the middle of the form, not at "Remove Machine" |
| R9-2 | P2 | mm-ui | verified (6eabaed): composer off, 'Mock VM is offline', chatOffline | Prompt sent in an offline machine's chat vanishes: no bubble, no error, text gone |
| R9-3 | P3 | mm-data | verified (105c653): banner 'Mock VM is offline' |
| R9-4 | P2 | mm-data | verified (679f940): re-keyed to test-vm within ~15 s, 2 of 2 runs | Migrated pairing whose bridge was offline at launch never re-keys or reconnects until the app is relaunched | Chat banner says "Reconnecting…" for a machine the menu shows as offline |

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

### R9-4: Offline-at-launch migration never recovers in-session
- Repro (live): stop the 7881 test bridge. Launch `-uitest -seedLegacyPairing <7881 link> -demo sidebar`. The menu shows `machineMenuItem-provisional-legacy`, "Machine: 127.0.0.1, offline" (correct so far). Start the 7881 bridge (same token, `test-vm`) 30 s later.
- Expected (api.md: "keep it under a provisional id and re-key it on the first successful `/machine`"): within the reconnect backoff (≤ 30 s) the entry turns into `machineMenuItem-test-vm`, "Test VM, online", still one machine.
- Actual: 3 min 16 s after the bridge came up (17:47:14 → end of test) it still reads `provisional-legacy`, offline, and nothing reconnects. Terminating and relaunching the app fixes it at once (one machine, online). A normal paired machine that goes down and comes back does reconnect (test3), so this is specific to the provisional entry.
- Test: `Round9LiveUITests.test7MigrateWhileOffline` (runner stops 7881, starts it ~30 s in). The unit test `provisionalPairingOfflineWaitsForTheBridge` passes, so the mock path differs from the live one (WS retry vs `/machine` retry?).

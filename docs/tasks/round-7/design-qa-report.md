# Round 7: design-qa report

## Verdict (after the recheck at `b7933d3`)
- **Design fidelity: GO.** The sidebar matches design B · Grouped. Cards, rows, headers, toolbar, bottom bar, monochrome status, Now / Show all / completed fold / all-in-Now all measure within a few points of the mockup.
- **Ship: GO.**
  - Every P2 is fixed and verified on device: R7-1, R7-2, R7-3, R7-13.
  - R7-4 is fixed in the tree order, covered by `testAccessibilityOrder`, and the user confirmed it with VoiceOver.
- **Open P3s (not blocking):**
  - R7-14: section gap 56.7 pt vs 62.
  - R7-15: the Now header count hyphenates at AX5 only.
- **Accepted by the lead:** R7-9 (native filter menu), R7-11 (system secondary text contrast), R7-12 (native bottom fade).
- Findings, measurements and fixes: `docs/qa/round7.md`.
- First pass (at `3478dd7`) was ship NO-GO over the P2s listed below.

## What I did
1. **Baseline.** Built `830b8be` in a scratch worktree on a dedicated simulator ("Relay QA 17 Pro", so peers' test runs weren't disturbed). Shot the old sidebar in dark and light, the filter menu and search.
2. **Mockups.** The `.dc.html` files need `support.js`, which isn't present. A script fills the `{{t.x}}` holes from the `renderVals()` token table, inlines `LiveCards` for the filter's `dc-import`, and renders at 390×844 @3× with headless Chrome. Output is in `docs/design/sidebar-b/render/`, gitignored because it holds real chat titles.
3. **Compare.** Built each commit in mock mode (`-mock -demo sidebar -agent w2:p1`). Took shots with a QA-only UI test, `ios/RelayUITests/DesignQAShots.swift`: default, Show all, completed expanded, scrolled, filter menu, filter applied, search, AX3, dark and light.
   - It is skipped unless `RELAY_SHOTS` is set.
   - It also keeps shots as xcresult attachments, for device runs.
   - I measured edges, gaps and separators with pixel scans of the 3× PNGs.
4. **Accessibility.**
   - `performAccessibilityAudit` in dark and light, plus an accessibility tree dump for labels and order.
   - AX3 via `-UIPreferredContentSizeCategoryName`.
   - Contrast computed from the design tokens.
5. **Device.** The lead gave the go-ahead. I built a clean worktree of master `77c8d00`, ran the tests on the iPhone and reinstalled the normal build. Details below.

## Before / after
| | Before (`830b8be`) | After (`3478dd7`, device `77c8d00`) |
|---|---|---|
| Dark | `docs/screenshots/round7-before-dark.png` | `round7-after-dark-default.png`, `round7-device-dark-default.png` |
| Light | `round7-before-light.png` | `round7-after-light-default.png`, `round7-device-light-default.png` |
| Filter | `round7-before-filter-dark.png` | `round7-after-{dark,light}-filter.png`, `round7-device-dark-filter.png` |
| States | – | `round7-after-dark-showall.png`, `-{dark,light}-completed`, `-light-search`, `-light-filter-needsinput`, `-dark-scrolled` |
| AX3 | – | `round7-after-ax3-{default,scrolled}.png`, `round7-device-ax3-{default,scrolled}.png` |

Before: device chips, "Projects" list of folders, coloured status badges, flat rows.
After: grouped cards on the grouped ground, the Now card with Show all, project cards with completed folds, glass toolbar and bottom bar.

## Findings summary (full detail in `docs/qa/round7.md`)
| id | sev | owner | status |
|---|---|---|---|
| R7-1 titles truncate at AX3 | P2 | sidebar-lists | verified (a3b974b, device) |
| R7-2 "2 completed" / "running" hyphenate at AX3 | P2 | sidebar-lists | verified at AX3 (a3b974b) |
| R7-3 New chat wraps, toolbar crowds at AX3 | P2 | sidebar-shell | verified (6acdaff, device) |
| R7-4 toolbar after the list in the accessibility tree | P2 | sidebar-shell | verified (6acdaff; the user confirmed with VoiceOver) |
| R7-13 rows legible behind status bar/toolbar when scrolled | P2 | sidebar-shell | verified (6acdaff, device) |
| R7-5 section gap 71 vs 62 pt | P3 | sidebar-lists | partly fixed, see R7-14 |
| R7-6 separators 1 pt vs 0.5 pt | P3 | sidebar-lists | verified |
| R7-7 Now row title/subtitle gap +3 pt | P3 | sidebar-lists | verified |
| R7-8 needs-input dot grey | P3 | filter-menu | verified fixed (b7900b8, device) |
| R7-9 native filter menu vs mockup | P3 | filter-menu | accepted (lead) |
| R7-10 some animations ignore Reduce Motion | P3 | shell, lists | verified in code |
| R7-11 light secondary text < 4.5:1 | P3 | lead | accepted (lead) |
| R7-12 soft bottom fade | P3 | sidebar-shell | accepted (lead) |
| R7-14 section gap now 56.7 vs 62 pt | P3 | sidebar-lists | open |
| R7-15 Now header count hyphenates at AX5 | P3 | sidebar-lists | open |

## Accessibility
- **Targets:** every sidebar control is ≥ 44 pt. The audit's "hit area too small" hits are all in the chat transcript behind the peek.
- **Labels:** rows read "status, title, project · age". Show all carries its value (expanded/collapsed). "Relay", "Now" and the project names are headers.
- **Dynamic Type:** after the fixes, AX3 works: titles wrap, the folds restack, the bars are capped and New chat is icon-only. At AX5 only, the Now header count hyphenates (R7-15).
- **Contrast:**
  - Dark: secondary text is 5.9:1 on `surface`.
  - Light: 3.4 to 3.8:1 (R7-11, accepted).
  - The audit's "Contrast failed: New chat" in dark is a false positive (black on a near-white capsule).
- **Reduce Motion:** every sidebar animation now honours it (R7-10, checked in code).

## Device run
- **Setup:**
  - Device: iPhone 13 `00008110-000414D91422801E`, iOS 26.5.2.
  - Signing: team `TC56945264`, `Relay/Relay-FreeTeam.entitlements`.
  - Build: clean worktree of master `77c8d00`. It includes R7-8; the other R7 fixes were still uncommitted, so they aren't in it.
  - Order: `build-for-testing`, then `test-without-building`.
- **UI tests on device: 32/32 pass.** Suites: DesignQAShots, EmptyStateUITests, MockUITests, NewChatUITests, Round2UITests, Round3UITests, SidebarFilterMenuUITests, SidebarListsUITests, SidebarMotionUITests, SidebarShellUITests. The Live* suites (which need the bridge) were excluded; round 7 didn't change them.
- **Unit tests: 121/121 pass** on the simulator from the same worktree. On device, the tests that read `docs/fixtures` through `#filePath` can't reach the host filesystem (49 issues, all "file couldn't be opened"). It's a test-harness limit, not an app bug.
- **User state was protected:**
  - Every suite launches with `-uitest`, so UI state goes to the isolated `dev.amansingh.herd.uitest` defaults suite, and `-resetSidebar` clears only that suite.
  - Mock mode never reads or writes the pairing.
  - The app was installed over the top, never uninstalled, so the data container was kept.
- **Manual pass:** I compared the device screenshots (same size as the mockup) against the design; they match the simulator. I could not do a hands-on VoiceOver swipe; the user did it after the recheck and confirmed R7-4.
- **Afterwards:**
  - Reinstalled the normal build with `TEAM=TC56945264 scripts/install-device.sh` from the same clean worktree.
  - Uninstalled `RelayUITests-Runner`.
  - Removed all my scratch worktrees.
  - The phone is handed back to herd-native.

## Device recheck
- Build: clean worktree of master `b7933d3` (lists a3b974b, shell 6acdaff). Same device, team and entitlements as the first run.
- UI tests on device: 35/35 pass (the same suites, plus the new shell accessibility-order and -size tests). Unit tests: 121/121 on the sim.
- The P2s verified in the device shots:
  - AX3: titles wrap, the folds restack, New chat is icon-only, the bars are capped.
  - Scrolled: no rows behind the status bar or toolbar.
  - Separators are 1 px.
- Shots: `docs/screenshots/round7-recheck-device-{dark-default,dark-scrolled,ax3-default,ax3-scrolled}.png`.
- Normal build reinstalled from that worktree; test runner uninstalled; worktree removed. The phone is free.

## Commits (my paths only)
- `c1d2279` `ios/RelayUITests/DesignQAShots.swift` (QA shots and accessibility test).
- `faf3f73`, `240617b` `docs/qa/round7.md` and `docs/screenshots/round7-*`.
- `77c8d00` DesignQAShots keeps attachments for device runs.
- This report, the device screenshots, and the R7-8 verified status.

## Next
- R7-14 and R7-15 (P3) whenever sidebar-lists picks them up.

# Round 7 QA: sidebar design B · Grouped

Build compared: `3478dd7` (shell, lists, filter-menu all in). Mock mode (`-mock -demo sidebar -agent w2:p1`), iPhone 17 Pro sim, iOS 26.4.
Reference: `docs/design/sidebar-b/render/*.png` (mockups at 390×844, local only). Device is 402×874, so y values below are offset by the safe area (+8 pt).
How: `DesignQAShots` UI test (`TEST_RUNNER_RELAY_SHOTS=<dir>`), pixel scans of the 3× screenshots, `performAccessibilityAudit`.

## Matches the design
- Cards: 16 pt inset, 310 pt wide (342 pt sidebar), radius ≈ 26, `surface` fill on grouped ground.
- Rows: Now rows 60 pt, project rows 50 pt, Show all 50 pt. Title leading 66 pt in Now, 36 pt in projects (same as the mockup).
- Headers: "Now" / project names at 36 pt leading, "N running" on the right.
- Toolbar: glass machine capsule and glass Filter/••• pill, 44 pt tall. Large title "Relay" 34 pt bold at 20 pt.
- Status is monochrome: spinner, hand, eye, circle+check. No colour left in the sidebar.
- Bottom: 50 pt glass search circle at 16 pt, label-filled New chat capsule, both ≈ 30 pt above the bottom edge.
- Completed fold ("✓ 2 completed ⌄"), all-in-Now row ("analytics  2 running ›"), Show all / Show fewer.
- Filter menu: order, glyphs, checkmark column, separator above Archived.

## Findings

| id | sev | owner | status | finding |
|---|---|---|---|---|
| R7-1 | P2 | sidebar-lists | open | AX3: chat titles are `lineLimit(1)` and truncate to ~9 characters ("Weekly re…") |
| R7-2 | P2 | sidebar-lists | open | AX3: "2 completed" hyphenates over three lines ("2 / complet- / ed"); the all-in-Now row hyphenates "run-ning" |
| R7-3 | P2 | sidebar-shell | open | AX3: New chat wraps to two lines, the machine name truncates to "Mock…", Filter and ••• crowd the pill |
| R7-4 | P2 | sidebar-shell | verify | VoiceOver order: the toolbar comes after the whole list and the bottom bar in the accessibility tree |
| R7-13 | P2 | sidebar-shell | open | Scrolled: rows stay legible behind the status bar and just under the toolbar; the top edge barely fades |
| R7-5 | P3 | sidebar-lists | open | Section gap is 71 pt, design 62 pt: the 44 pt `+` makes project headers 44 pt tall (design 36 pt, the `+` overflows) |
| R7-6 | P3 | sidebar-lists | open | Row separators are 1 pt (3 px), design 0.5 pt |
| R7-7 | P3 | sidebar-lists | open | Now rows: title-to-subtitle gap about 3 pt wider than the design (SwiftUI line heights) |
| R7-8 | P3 | filter-menu | open | The needs-input dot on Filter renders grey, design is label colour |
| R7-9 | P3 | filter-menu | accepted (lead) | Native menu differs from the mockup: it grows over the Filter button, rows are 42 pt, and there is no veil or blur behind it |
| R7-10 | P3 | sidebar-shell, sidebar-lists | open | Reduce Motion: a few `.smooth` animations ignore it |
| R7-11 | P3 | herd-native (design call) | accepted (lead) | Light mode: secondary text contrast is 3.4 to 3.8:1, under AA 4.5:1 |
| R7-12 | P3 | sidebar-shell | accepted (lead) | The bottom scroll-edge fade is softer than the design; rows stay legible under the bar |

### R7-1: titles truncate at AX3
- Where: `SessionRow.swift:56,76` (`.lineLimit(1)`).
- Fix: `.lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)` or `lineLimit(1...2)` at accessibility sizes. Default size stays one line as designed.

### R7-2: completed and all-in-Now labels hyphenate at AX3
- Where: `SidebarProjectSection.swift` completed row (~l.132) and the all-in-Now row (~l.105).
- Fix: `lineLimit(1)` + `minimumScaleFactor(0.8)` on the count text, or `ViewThatFits` to drop the trailing text under the title.

### R7-3: toolbar and bottom bar at AX3
- New chat: `SidebarBottomBar.swift` `newChatButton`. Add `.lineLimit(1)`.
- Cap both bars the way system bars do: `.dynamicTypeSize(...DynamicTypeSize.accessibility1)` on `SidebarToolbar` and `SidebarBottomBar`. The list keeps full Dynamic Type.

### R7-4: VoiceOver order (verify on device)
- `app.debugDescription` lists: title, Now, rows…, search, New chat, then machine menu, Filter, More.
- Cause: both bars are `safeAreaBar`s declared after the `List`, so they come later in the tree.
- If VoiceOver reads it that way on device, give the toolbar `.accessibilitySortPriority(1)` (or put the list and bars in an `accessibilityElement(children: .contain)` with the toolbar sorted first).

### R7-13: top scroll edge
- Screenshot: `round7-after-dark-scrolled.png`. After scrolling, "Speed up image loading" and "Refresh push tokens" read clearly behind "9:41" and the toolbar's glass.
- Cause: `.scrollEdgeEffectStyle(.soft, for: .all)`; the soft effect barely dims the status-bar strip.
- The mockup has no scrolled state, but its toolbar sits on the grouped ground. Try `.hard` for `.top`, or a grouped-colour gradient behind the toolbar like the bottom fade. Needs a look on device.

### R7-5: section gap
- `SidebarSectionHeader` has `minHeight: 36`, but the `newChat-<project>` button is 44×44, so the header is 44.
- Fix: keep the 44 pt target and let it overflow, as the mockup does (`margin-right: -12px`, height 44 in a 36 row): e.g. `.padding(.vertical, -4)` on the `+` button.

### R7-6: separator thickness
- Measured 3 px (1 pt) in dark (rgb 56,56,59) and light (232). Mockup 0.5 pt (`t.sep`).

### R7-7: Now row text spacing
- Title glyphs 231→247, subtitle 256→269 (after), vs 221→237 and 242→253 (mockup). Gap 8.4 pt vs 5.3 pt.
- Cause: SwiftUI body/subheadline line heights (22/20 pt) vs the HTML defaults. `VStack(spacing: -2)` would match. Cosmetic.

### R7-8: needs-input dot is grey
- `SidebarFilterMenu.swift:28` uses `.fill(.primary)`; inside a `Menu` label that dims to grey.
- sidebar-shell hit the same thing with the machine dot and used `Color(.label)` (`SidebarToolbar.swift` `MachineMenu`). Do the same here.

### R7-9: native filter menu vs the mockup
- Mockup: glass card 256 wide at (70, 112), 48 pt rows, content behind gets a veil + 3 pt blur.
- Build: system menu ≈ 250 wide, grows out of the Filter button and covers the toolbar, 42 pt rows, no veil.
- The brief says to use the real iOS 26 control where HTML approximates glass. Recommend accepting it as native.

### R7-10: Reduce Motion gaps
- `SidebarBottomBar.swift:37,60`: search expand/collapse `withAnimation(.smooth)`.
- `SidebarView.swift`: `.animation(.smooth, value: store.filter)` and `value: isSearching`.
- `SessionRow.swift:94`: archive `withAnimation(.smooth)`.
- Show all, completed and project folds already honour it.

### R7-11: light-mode secondary text contrast
- Design `label2` light = rgba(60,60,67,0.64): 3.8:1 on white, 3.6:1 on #F2F2F7. The build uses system `.secondary` (0.6): 3.4:1.
- Dark is fine: 5.9:1 on `surface`.
- The audit reports "7 running" and "Show all 7" as "contrast nearly passed" in light.
- This is the system secondary label, and it darkens with Increase Contrast. Recommend accepting it, but it is a design call.

### R7-12: bottom fade
- Mockup: gradient 110 pt tall, fully opaque from ≈ 28 pt above the bottom, so nothing shows under the buttons.
- Build: `.scrollEdgeEffectStyle(.soft, for: .all)`; the last row's text is still readable under and below the bar (contrast ≈ 1.5:1 against the ground).
- It is the native iOS 26 effect. Accept, or use `.hard` for the bottom edge if the design wins.

## Accessibility summary
- 44 pt targets: every sidebar control passes (machine 202×44, Filter 44×44, More 44×44, `+` 44×44, search 50×50, New chat 144×50, rows 310×50/60). The only "hit area too small" audit hits are in the chat transcript behind the peek.
- Labels: rows read "status, title, project · age" (e.g. "Working, Weekly report export, analytics · 2m"). Show all has value expanded/collapsed. Headers ("Relay", "Now", project names) carry the header trait.
- Dynamic Type: see R7-1 to R7-3.
- Audit notes, not filed: "Contrast failed: New chat" in dark is a false positive (black text on a near-white capsule; the audit samples the glass). "Text clipped: Mock MacBook Pro" didn't reproduce visually at the default size.

## Screenshots
In `docs/screenshots/` (mock data only; mockup renders stay local because they hold real chat titles):
- Before (`830b8be`): `round7-before-dark.png`, `round7-before-light.png`, `round7-before-filter-dark.png`.
- After (`3478dd7`): `round7-after-{dark,light}-default.png`, `-dark-showall`, `-{dark,light}-completed`, `-{dark,light}-filter`, `-light-filter-needsinput`, `-light-search`, `-dark-scrolled`, `-ax3-default`, `-ax3-scrolled`.
- Regenerate: `TEST_RUNNER_RELAY_SHOTS=<dir> xcodebuild test -scheme Relay -destination 'id=<sim>' -only-testing:RelayUITests/DesignQAShots`.

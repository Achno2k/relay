# sidebar-lists report (round 7)

The rows and cards of design B · Grouped, and the grouping logic behind them. sidebar-shell lays them out in `SidebarView`. Code commit: `38bccc4`.

## View API (agreed with sidebar-shell and filter-menu)
- `SidebarNowCard(store:onSelect:)` returns a Section. It renders nothing when no agent is working.
- `SidebarProjectSection(store:section:onSelect:onNewChat:)` returns a Section. `onNewChat` gets the workspace id.
- `SidebarSessionList(title:agents:emptyText:store:onSelect:)` is the flat card the shell uses for a filter other than All, and for search results.
- `SessionRow(agent:store:showsProject:onSelect:)`. With `showsProject` it's the 60 pt two-line row (glyph, title, "project · 2m"). Without it, the 50 pt card row (title, trailing age).
- `StatusGlyph(status:unseen:)` keeps its old init.

## Grouping (`SidebarModel.grouped()`)
- **Now.** Every running chat across projects, newest first. The card shows 4 (`nowCap`), then a "Show all N" row that opens the rest.
- **Project cards.** Running chats never appear here, since they're already in Now. Needs input sorts first, then ready for review, then newest.
- **Completed fold.** A chat counts as completed when it's `done` and the person has seen it. Those fold into "✓ N completed". Idle chats stay as plain rows with no glyph, as in the mockup. The Completed filter still counts idle chats as completed; I didn't touch filter semantics.
- **All in Now.** A project whose chats are all running shows one row, "name · N running ›". Tapping it opens the full section, which lists those running chats with spinners and the `+`. Tapping the name in the header folds it back. Without this, a project's `+` could only be reached from the New chat sheet.
- **Workspace order.** Sections follow `/workspaces` order and keep empty projects (a "No chats" row) so new chat per project still works. Agents in a workspace that `/workspaces` doesn't list get a section too.
- `projects()` and `Project` are removed. Nothing uses them any more.
- **Remembered expansion.** Show all, open completed folds and opened all-in-Now projects live in `AppDefaults.standard`, under `sidebarNowShowAll`, `sidebarCompletedOpen` and `sidebarProjectsOpen`.

## Against the mockup (iPhone 17 Pro sim, dark and light)
- Cards use `secondarySystemGroupedBackground` (#1C1C1E / #FFF) in the shell's inset-grouped list, which gives radius 26 and a 16 pt inset.
- Headers sit 36 pt from the sidebar edge. "Now" is 16 pt below the title and the others 22 pt below the previous card, with the card 4 pt under the header. The all-in-Now row has no header and a 12 pt gap.
- Now rows are 60 pt: glyph at 18, titles at 50, and "Show all" lines up with the titles. Card rows are 50 pt at 20. Separators start at the text and run to the card's edge.
- Status is monochrome:
  - A label-coloured 8-spoke spinner drawn to the mockup's spec (18 pt, 2.2 stroke, 0.9 s stepped). A tinted `ProgressView` stayed grey.
  - hand.raised for needs input, eye for ready for review, checkmark.circle for completed, circle for idle.
  - In card rows, idle gets no glyph.
- The open chat's row gets a `systemFill` highlight.

## Behaviour kept
- **Archive.** Trailing swipe (full swipe archives) and a long-press context menu with Archive/Unarchive and Stop (disabled unless working), plus the preview card.
- **Motion.** Show all, the completed fold and the all-in-Now toggle animate with `.smooth`, and not at all under Reduce Motion. Each toggle gives selection haptics.
- **VoiceOver.**
  - Rows read the title, then "status, [project], age".
  - Show all and completed have a value of expanded or collapsed.
  - Headers carry the header trait.
  - `+` reads "New chat in <project>", with a 44 pt target.
- **Ids.** `session-<agentId>`, `nowShowAll`, `nowHeader`, `completed-<project>`, `project-<name>` (header text, or the all-in-Now row button), `newChat-<name>`.
- **Dynamic Type.** Rows use min heights and grow. At accessibility sizes titles wrap (see R7-1/R7-2). The glyph box scales with `@ScaledMetric`.

## Mock
`ios/Relay/Mock/MockSidebar.swift` adds two made-up projects and nine chats:
- 7 running in all, so Now shows "Show all 7".
- website gets 2 completed chats, marked seen in the test-isolated defaults so they fold.
- analytics is all running.
- mobile has 3 running and 1 idle.

The fixtures already have the blocked chat (shop-api) and the ready-for-review chat (website "Codex"). The fixture JSON is unchanged.

## Tests
- Unit: `SidebarGroupingTests` covers the Now order and cap, the completed fold, the card order, all-in-Now, archive, unknown workspaces and the expansion set. `Round2Tests` sidebar tests moved to `grouped()`. All 121 unit tests pass.
- UI: `SidebarListsUITests`.
  - Show all opens, is remembered across launches, and folds.
  - The completed fold expands and collapses.
  - The all-in-Now row opens and folds.
  - Needs-input and review rows, and new chat from a project `+`.
  - Archive by swipe and by context menu, the Archived filter, unarchive.
- Updated for the new sidebar: `Round2UITests` (tree test → project cards; archive tolerates a full swipe; new value strings), `MockUITests.testLongChatOpensAtBottom` (uses Show all and scrolls instead of expanding folders), and `NewChatUITests.testCodexWithModelAndEffort` (the sidebar now has a "Codex" row behind the sheet).

## Verification
All on the iPhone 17 Pro simulator (iOS 26.4), mock mode.
- Unit tests: 121/121 pass.
- Full mock UI suite (Live* excluded): 28 of 29 passed on the first run. The failure was `LiveToolUITests` timing ("row came at 5.7 s, limit 4.5 s"). It then passed 3/3 alone, so I count it as a flake under load.
- After the QA fixes: `SidebarListsUITests` 6/6 (including the new AX3 test), `Round2UITests` 5/5, `MockUITests.testLongChatOpensAtBottom`, `NewChatUITests` 2/2.
- Pixel checks on 3× screenshots:
  - Gap between cards is 62.0 pt (design 62).
  - Hairlines are 1 px in `separator` colour.
  - Now rows are 60 pt apart.
- Screenshots (mock data only), in `docs/screenshots/`:
  - `round7-lists-default.png`
  - `round7-lists-completed-expanded.png`
  - `round7-lists-all-in-now.png`
  - `round7-lists-ax3-now.png`
  - `round7-lists-ax3-folds.png`

## QA fixes (docs/qa/round7.md)
- R7-1: titles take up to 3 lines at accessibility sizes (subtitles 2) and 1 line at standard sizes. Rows pad 8 pt vertically.
- R7-2:
  - At accessibility sizes the completed row drops its check glyph, so "completed" fits on one line.
  - The all-in-Now row stacks the name over "N running". The count is `fixedSize`, so it can't hyphenate.
- R7-5:
  - The `+` keeps its 44 pt target but overhangs the 36 pt header (`padding(.vertical, -4)`).
  - The inset-grouped list adds about 3 pt above and 2.3 pt below every header, even with zero insets. `SidebarSectionHeader` subtracts it, and so does the 12 pt all-in-Now spacer.
- R7-6: the list's separators are hidden. Each card row draws its own `1 / displayScale` hairline, from the text to the card edge. The last row in a card has none. `SessionRow` has a new `separator:` parameter (default true).
- R7-7: the Now row title/subtitle stack uses `spacing: -2` (was 1).
- R7-10, my half: archive and unarchive don't animate under Reduce Motion.

## Notes for others
- A full swipe on a card row archives at once, with no button left to tap. UI tests should tolerate that (`if archive.waitForExistence(timeout: 1) { archive.tap() }`).
- The drag that starts a swipe needs to begin above the floating bottom bar. The test `scroll(to:)` helpers bring rows into the top two thirds first.
- When swiped, the row slides past the card's left edge. That's how the native inset-grouped swipe draws it. design-qa may want to judge it on device.

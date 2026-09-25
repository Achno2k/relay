# Round 7: filter-menu report

## What shipped
- `ios/Relay/Features/Sidebar/SidebarFilterMenu.swift`: `SidebarFilterMenu(selection: Binding<SessionFilter>, needsInput: Bool)`.
  - A plain 44×44 button with `line.3.horizontal.decrease` and no glass of its own. The shell puts it in the glass pill next to More (`SidebarToolbar`).
  - A native iOS 26 `Menu`, the real version of the mockup's glass menu. It gives:
    - the glass card and the blurred backdrop
    - the checkmark column, then the icon, then the label
  - Order: All, Needs input, Ready for review, Working, Completed, a separator, then Archived.
  - Needs-input dot: 7 pt, label colour (status is monochrome, as the design specifies; it was orange before).
  - Accessibility unchanged: id `sidebarFilter`, label "Filter: <title>", value "needs input".
- `ios/RelayUITests/SidebarFilterMenuUITests.swift`. It checks:
  - the menu order and the separator above Archived
  - that only the chosen item is Selected
  - the dot, and the 44 pt target
  - that the choice is remembered across launches
- The old `FilterButton` in `SidebarView.swift` is deleted by sidebar-shell.

## Behaviour
- No change. Same `SessionFilter` cases, `store.filter`, persistence (`sessionFilter` default), archive and flat lists.
- `SidebarModel.swift` is not touched.

## Decisions
- **Native Menu, not a custom overlay.** Agreed with sidebar-shell. It gets VoiceOver, Dynamic Type, Reduce Motion and dismissal for free.
  - Row height is the system's 42 pt against the mockup's 48 pt.
  - Width is about 250 pt against 256.
- **Checkmark toggles in two `Section`s, not an inline `Picker`.** Menus drop both `Section` and `Divider` inside a Picker, so Archived had no separator.
- **Menu icons are `UIImage`s with their own accessibility identifier.**
  - UIKit menus mark a row *Selected* when its image is named `checkmark…`.
  - So with the plain `checkmark.circle` symbol, VoiceOver read "Completed" as chosen when it wasn't.
  - The old Picker used the same symbol, so it probably had the same bug.
- **All uses `list.bullet`** (closer to the design's list glyph than `checklist`). The mapping lives in my file; `SessionFilter.symbol` is unchanged.

## Verification
- Tested in a clean worktree at `830b8be` with only my file wired into the old sidebar, on a dedicated simulator (iPhone 17 Pro, iOS 26.4, dark and light).
  - `SidebarFilterMenuUITests`: pass.
  - `Round2UITests.testFilterMenuShowsFlatSessions`: pass.
  - `Round2UITests.testArchiveAndUnarchive`: pass.
  - `SidebarMotionUITests`: pass.
- Screenshot compared with `LiveCardsFilter-light.png`: same structure, glyphs, check column and separator.
- sidebar-shell reports its shell and motion UI tests green with this file in the new toolbar.

## Notes for others
- design-qa: menu items are addressable by label (All, Needs input, Ready for review, Working, Completed, Archived). Row height and width are system-defined (see Decisions).
- `project.pbxproj` is committed by sidebar-shell after all files land.

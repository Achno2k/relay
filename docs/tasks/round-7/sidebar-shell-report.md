# sidebar-shell report (round 7)

The container for design B · Grouped. It composes the views from sidebar-lists (`SidebarNowCard`, `SidebarProjectSection`, `SidebarSessionList`) and filter-menu (`SidebarFilterMenu`).

## What changed
- `SidebarView.swift` is only a container now, down from 560 lines to about 95:
  - `List(.insetGrouped)`, 16 pt horizontal content margins, `listSectionSpacing(0)` (lists pad their own headers), `systemGroupedBackground`.
  - The title "Relay" is 34 pt bold at 20 pt leading.
  - With filter All it shows the Now card and then one section per project from `sidebar.grouped()`. Any other filter shows the flat `SidebarSessionList`, and so does a search ("Results").
  - Toolbar and bottom bar are iOS 26 `safeAreaBar`s, so the list gets the native soft scroll-edge fade under both.
- New `SidebarToolbar.swift` (52 pt tall, 16 pt padding):
  - A glass machine menu: 7 pt dot, name, chevron.down. The dot is filled when connected and hollow while reconnecting (monochrome). The menu lists machines, host and connection, which is what used to sit at the top of •••.
  - A glass pill holding `SidebarFilterMenu` and •••. ••• keeps Usage and Unpair.
- New `SidebarBottomBar.swift`:
  - 50 pt glass search circle that grows into a field (glass morph).
  - Label-coloured "New chat" capsule (compose icon, 8 pt gap, 18/22 padding).
  - Sits 30 pt from the screen bottom, or 8 pt above the keyboard while typing.
- `MainView.swift`, drawer geometry:
  - Sidebar is `min(width − 48, 342)` wide.
  - The chat recedes into a card 10 pt right of the sidebar, radius 36. The card runs from the safe-area top to 18 pt above the screen bottom. It's drawn with an animatable `PeekShape` clip, not a scale, so the closed chat still fills the screen edge to edge.
  - The peek has a hairline edge and no dim, as in the mockup. With Reduce Motion on, nothing moves and the dim stays.
  - The peek is a VoiceOver button, "Return to <chat title>" (id `sidebarPeek`).
- Removed the old pieces: device chips, Lucide folder rows, old ChatRow/ProjectRow/StatusGlyph/AgentPreview, old FilterButton, old search/new-chat bar.
  - The Lucide assets stay because `NewChatSheet` still uses them.
  - The unused `machineFilter` chip selection went with the chips. It never filtered anything.
- `ios/Relay.xcodeproj/project.pbxproj` is regenerated with xcodegen and includes everyone's new files.

## Measured against the mockup (iPhone 17 Pro sim, 402×874)
- Toolbar centre is 26 pt below the safe-area top.
- The title and the "Now" header are within about 4 pt of the mockup.
- Peek: x 352, top at the safe area, bottom 18 pt above the edge.
- Bottom bar: 30 pt from the bottom.
- Light and dark both checked. The closed chat shows no grouped-ground bleed under the status bar.

## Accessibility
- 44 pt targets throughout.
- Ids: `sidebarMachineMenu`, `sidebarMore`, `sidebarUsage`, `sidebarSearch`, `sidebarSearchField`, `sidebarNewChat`, `sidebarPeek`. `sidebarFilter` comes from filter-menu.
- Dynamic Type:
  - A Menu label truncates early inside a Spacer'd HStack. The machine menu gets a flexible leading frame instead, so the full name fits at default size.
  - At XXXL it truncates rather than pushing the pill off screen.
  - AX sizes reflow.
- Reduce Motion checked on the sim.

## Tests
- New `RelayUITests/SidebarShellUITests.swift`:
  - search: grow, results, no-match text, close
  - machine menu and ••• → Usage
  - the peek returns to the chat
  - New chat opens the sheet
  - Screenshots go to `TEST_RUNNER_RELAY_SHOTS` when set.
- `SidebarMotionUITests` now keys on `sidebarMachineMenu`. The filter button moved to the right, so it no longer slides off screen when the drawer closes.
- On my simulator (`Relay Shell 17 Pro`), the 4 `SidebarShellUITests` and `SidebarMotionUITests.testOpenAndCloseEveryWay` pass.

## Notes for others
- XCUITest taps an element's centre, and most of the peek is off screen. Tap `sidebarPeek` with `coordinate(withNormalizedOffset: (0.05, 0.5))`.
- `NSPredicate("frame.minX < 0")` never matches on XCUIElement. Poll `frame` instead, as `SidebarMotionUITests` does.

## QA fixes (docs/qa/round7.md)
- R7-3, Dynamic Type:
  - Toolbar and bottom bar stop growing at AX1.
  - From AX3, New chat is icon only (label "New chat").
  - The machine name scales to 0.8 before it truncates. Machine and ••• get the large content viewer.
- R7-4, VoiceOver order: the toolbar now sits above the List in a `VStack`, since sort priority didn't change the order. The tree reads toolbar, list, bottom bar.
- R7-13, top edge: the same change fixes it. Rows no longer scroll behind the status bar or the toolbar.
- R7-10, Reduce Motion: the search morph and the filter/search list animations are off.
- New tests: `SidebarShellUITests.testAccessibilityOrder` and `testAccessibilitySizes` (AX3, AX5).
- 20 sidebar, Round2, Mock and NewChat UI tests pass on my simulator.

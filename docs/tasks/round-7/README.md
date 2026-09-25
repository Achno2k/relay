# Round 7: build sidebar design "B · Grouped"

The user designed the new sidebar and chose **direction B · Grouped**. Source (local only, gitignored; it contains real chat titles, so never commit it):
- `docs/design/sidebar-b/LiveCards.dc.html`: dark (its script also holds the light palette).
- `docs/design/sidebar-b/LiveCardsLight.dc.html`: light.
- `docs/design/sidebar-b/LiveCardsFilter*.dc.html`: the filter menu open, dark and light.

These are HTML mockups at 390×844 pt: exact sizes, radii, fonts, colours and copy are in the inline styles and the `renderVals()` token table. **Match them closely.** Where HTML approximates iOS (blur and glass), use the real SwiftUI/iOS 26 equivalent: `.glassEffect`, system materials, `.buttonStyle(.glass)` / `.glassProminent`.

## The design, summarised
- **Ground:** grouped background (`#000` dark / `#F2F2F7` light). Section cards are `surface` (`#1C1C1E` / `#FFF`), radius 26, inset 16 pt from the sidebar edge. The sidebar is 342 pt wide; the chat peeks on the right (a 120 pt sliver at x 352, radius 36) with a glass "Sessions" (≡) button.
- **Colours:** system label, secondaryLabel and tertiaryLabel, and separators at 0.5 pt. **Status is monochrome**, carried by the glyphs, not colour: spinner = working, circle = idle, circle+check = completed, hand = needs input (from the filter menu), eye = ready for review. SF Pro.
- **Toolbar (52 pt tall, 16 pt side padding):**
  - Left: a glass capsule machine menu (a 7 pt dot, the machine name, a chevron.down), 44 pt tall.
  - Right: a glass pill with two 44 pt buttons, Filter (three decreasing lines) and More (•••).
- **Large title "Relay":** 34 pt bold, 20 pt leading padding.
- **"Now":**
  - Header row: "Now" semibold 17 on the left, "N running" secondary 15 on the right, padding 36 pt leading.
  - Then one card holding every running session across all projects, **capped at 4**. Each row is 60 pt: spinner, title 17, subtitle "project · 2m".
  - Then a "Show all N" row (secondary, with chevron.down) that expands the card.
- **Project sections:**
  - Header: name semibold 17 at 36 pt leading, plus a `+` (new chat in that project, 44 pt target).
  - Card rows are 50 pt: title 17 and a trailing age in secondary 15. Idle rows have no leading glyph.
  - Completed chats fold into a final row, "✓ N completed" with chevron.down, which expands them.
  - A project whose sessions are *all* shown in Now collapses into a single card row: "ranksmith · 2 running ›".
- **Bottom** (30 pt from the bottom, 16 pt inset):
  - A 50 pt glass **search circle** on the left. Tapping it expands into a search field.
  - A prominent **"New chat" capsule** on the right (label-coloured fill, inverted text, a compose icon).
  - A scroll-edge fade above them.
- **Filter menu:** a glass menu anchored under the Filter button, radius 30, 256 wide. Rows are 48 pt: a checkmark column, an icon, the label. The order is All, Needs input, Ready for review, Working, Completed, a separator, then Archived. Content behind gets a light veil and blur.

## Sessions and ownership
| Session | Owns |
|---|---|
| sidebar-shell | `ios/Relay/Features/Sidebar/SidebarView.swift` (the container), new `SidebarToolbar.swift` and `SidebarBottomBar.swift`, the drawer/peek geometry in `ios/Relay/App/MainView.swift`. It **composes** the other sessions' views. |
| sidebar-lists | New `SidebarNowCard.swift`, `SidebarProjectSection.swift`, `SessionRow.swift`, `StatusGlyph.swift`, plus any grouping and sorting logic in `ios/Relay/Stores/SidebarModel*` (Now, completed folding, "all in Now" collapse). |
| filter-menu | New `SidebarFilterMenu.swift`; rewires the existing filter/archive state to the new menu (no behaviour change: same filters, archive and remembered selection). |
| design-qa | No app code. Screenshots vs the mockups, accessibility, device run. Files findings to `docs/qa/round7.md` for the owners. |

Agree the view APIs between shell, lists and filter-menu early (herdr messages). Suggested: `SidebarNowCard(model:)`, `SidebarProjectSection(section:)`, `SidebarFilterMenu(selection:)`.

## Rules
- Keep existing behaviour: archive, filters, remembered expansion, the machines list, Usage in the ••• menu, new chat per project, search, sidebar motion, and accessibility (44 pt targets, VoiceOver labels, Dynamic Type, Reduce Motion).
- Remove the old sidebar pieces the design replaces (device chips row, Lucide folders, old search/new-chat buttons) **only once the new ones work**. Keep the Lucide assets in the catalog unless nothing uses them.
- Mock-first: update the Mock/fixtures so every state appears (7 running for "Show all", a project with completed chats, an all-in-Now project, a blocked chat).
- Tests: unit tests for the grouping logic, and mock UI tests for Show all, completed expand, filter, archive and search.
- Stay in your paths and commit only them (never `-a`/`-A`). The lead is `herd-native`. **Ask herd-native before using the iPhone.**
- Write a report to `docs/tasks/round-7/<session>-report.md`.

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Q4qo2QabVUAvi2YgczSwpD
```

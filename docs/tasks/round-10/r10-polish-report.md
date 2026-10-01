# Round 10: r10-polish report

Branch `r10/polish`. Owns `Features/Sidebar/**` and `Features/Attachments/**`. B4 also changed `Features/Composer/ComposerView.swift`, with r10-chat's OK.

## B1: padding under "Relay" (65caad1)
- **Cause:** the first section under the title kept the top gap that's meant for the space between cards. That left ~34 pt from the title's baseline to "Now", against ~15 pt from the toolbar to the title.
- **Fix:** a new environment flag, `sidebarLeadsList`, is set on whichever section comes first: machine notices, Now, the first project, or the filter/search list. With it set:
  - its header drops its top gap (`SidebarSectionHeader`);
  - a card with no header keeps only 8 pt (`SidebarGapHeader`).
- **Result:** ~14 pt from the bottom of "Relay" to the first header, the same as the toolbar gap. Checked with Now first and with an offline-machine notice first.
- **Tried first:** negative bottom padding on the title row. It clipped the "y" (list cells clip), so I dropped it.
- **Checked by r10-qa:** their first repro's "band above the title" came from a screenshot taken mid-overscroll. At rest (live bridge, iPhone 17 Pro, offline-VM card first), the title baseline to the first card went from ~26 pt to ~19 pt with 65caad1. The layout looks balanced.

## B4 / B9: Photos hit area (e9ac406, pbxproj 0e2ec7c)
- **Repro:** not reproducible in the sim. XCUITest taps on the Photos row's centre, icon, edges and bottom-left corner all opened the picker, with the keyboard up or down. r10-qa got the same result.
- **What's wrong with the layout:** Photos was the menu's bottom row. It sat exactly over the `+` button (row at 12,780 and 250×42; `+` at 12,784 and 48×48), inside the menu's rounded corner, next to the screen edge. It's most likely a device-only miss.
- **Fix (lead picked option a):** `+` is now a plain button that opens `AttachmentSheet`:
  - Camera, Photos and Files tiles, each ~110×88, where the whole tile is the target;
  - a full-width "New chat in <workspace>" row.
  - At accessibility sizes the tiles become rows with aligned icons.
  - The sheet closes first; the chosen picker opens in `onDismiss`, so two presentations never overlap.
- **Kept:**
  - `composerPlus` / "Add" on the button;
  - the DEBUG "Test image (RELAY)" / "Test PDF" buttons;
  - photo multi-select up to `remainingSlots`, with `photo-N.ext` names;
  - Camera disabled when there's no camera.
- **Moved:** ComposerView lost the picker state and modifiers. They now live in the `composerPlusSheet` modifier.

## Tests (iPhone 17, under `iphone17.lock`)
- RelayTests: pass.
- `Round2UITests/testAttachImageAndPDF`: pass.
- `SidebarShellUITests`: pass (after one launch flake on the first run).
- `A11yAuditUITests`: pass, with the sheet audited at default size and AX3.
- A throwaway UI test covered the sheet's Photos, test image and New chat paths. It isn't committed.

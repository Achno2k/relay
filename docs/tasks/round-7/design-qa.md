# design-qa: hold the build to the design

You don't write app code. Your job:
1. **Baseline.** Before the others land, screenshot the current sidebar (dark and light) on the simulator, so there's a before/after record.
2. **Render the mockups.** Open the four `docs/design/sidebar-b/*.dc.html` files in a headless browser at 390×844. They reference `./support.js`, which isn't present; if they don't render, extract the static markup and replace the `{{t.x}}` holes from the token table with a small script. Save PNGs under `docs/design/sidebar-b/render/` (gitignored path).
3. **Compare.** As sidebar-shell, sidebar-lists and filter-menu commit, build in mock mode and screenshot the same states: default, Show all expanded, completed expanded, filter menu open, and search expanded, in dark and light. Compare side by side. File concrete deltas in `docs/qa/round7.md`, e.g. "Now header leading is 20pt, design 36pt", "card radius 16, design 26", "status glyph coloured green, design is monochrome". Give each an owner session, and message the owner for anything major.
4. **Accessibility.** VoiceOver labels and order, 44 pt targets, Dynamic Type to AX3, contrast of secondary text on `surface`.
5. **Device run.** When the others report done: ask herd-native before taking the iPhone. Then run the UI tests and a manual pass on device, reinstall the normal build with `TEAM=TC56945264 scripts/install-device.sh`, and write `docs/tasks/round-7/design-qa-report.md` with before/after screenshots and a go/no-go.

# ios-polish: design pass on every screen

The references are the ChatGPT iOS app and Codex iOS (`docs/screenshots/ref/codex-home.png`, `filter-menu.png`) on iOS 26 Liquid Glass.
- Take a before-and-after screenshot of every screen, dark and light: sidebar (all filters), chat (long, empty, working, stopped, error), approval sheet (single, multi-question, free text, trust prompt), controls menus, new chat sheet, pairing, attachments, the image viewer.
- **Fix:**
  - A consistent spacing scale.
  - Type hierarchy.
  - Icon weights (SF Symbols weights next to the Lucide folders).
  - Tap targets of at least 44 pt.
  - Glass consistency: system glass where possible, and no double blur.
  - Truncation of long titles and model names.
  - Motion: springs consistent, nothing janky, Reduce Motion respected.
  - Haptics used sparingly and consistently.
- **Accessibility:** VoiceOver labels and order on every screen, Dynamic Type up to AX3 with no clipping, contrast checks for the status glyph colours.
- **Empty and error states:** these must look designed, not like defaults.
- Keep behaviour as it is: state and networking belong to ios-harden. Put every change in the report with before/after screenshots.

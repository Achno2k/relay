# Round 10: r10-files report (B2 file viewer)

## What the user gets
- Expanding a tool group shows a chevron on every Read/Write/Edit row that has something to open. Tapping the row opens a sheet.
- The sheet has two tabs when both apply:
  - **Changes**: the call's diff. Edit/MultiEdit are line diffs, one "Change n of N" header per replacement. Write is all green. codex's multi-file edit is its unified diff, with a header per file.
  - **File**: the file as it is now, from `GET /agents/:id/file`. Numbered monospaced lines scroll sideways, and Markdown renders. Images can be pinched and double-tapped to zoom.
- An Edit's diff uses real line numbers when the new text is found in the fetched file ("Line 14").
- Notices: the change failed and wasn't applied; the change was cut to the bridge caps; the file is over 1 MB; the file is outside the project.
- Errors: 403 "Outside the project", 404 "File not found" (with retry), 415 "Can't show this file", 413 "Image too large", offline (with retry). A 403 here never sends the user to re-pair.
- The `…` menu has Wrap Lines (prose wraps by default), Show Source for Markdown, Copy and Share.
- The row's context menu keeps "Show Output" for the old preview toggle.
- If an approval arrives while a file is open, the viewer closes and the approval sheet shows. SwiftUI shows only one sheet at a time, so presenting both would drop the approval.

## Rules followed
- A row is tappable only when `toolCall.path` is set (the bridge sets it only inside the cwd) or `toolCall.edit` exists. Paths go to the bridge as they are and percent-encoded (`+`, `&`, `#`, `?`, space); the bridge does the realpath check.
- An edit with no path (e.g. Claude's plan file in `~/.claude/plans/`, or a multi-file codex edit) shows Changes only. The plan itself is B7 (r10-chat).
- No third-party libraries.

## Commits (r10/files)
- `4b85af8` relaykit: `APIClient.file`, `Backend.file` (the default implementation throws 404 for test doubles), `NamespacedBackend`/`RoutingBackend` forwarding, and `FileContent`/`AgentFile` in the new `AgentFile.swift`.
- `21cf298` ios: `Features/Files/**` (sheet, code view, diff engine, request/presenter), tap wiring in `ChatRows.swift`, one `.fileViewer(store:)` line in `App/MainView.swift`, mock files/chats, and tests.
- `ac86053` xcodeproj regenerated.
- `cbcc17b` approval-vs-file sheet handling.
- Picked onto this branch only so it builds; the lead picks the originals: `a43371f`/`dbe6a50` (r10-bridge contract and fixtures), `ae9fa57`/`592b912` (r10-chat model fields and ToolStep path/edit).

## Tests
- `RelayTests/Round10FilesTests.swift` covers the file call (text, image bytes, percent-encoding, 403 not being unauthorized), the error mapping, tap targets (including the `messages-edits.json` fixture), line diff order and numbers, placing an edit in the file, MultiEdit headers, Write, unified-diff parsing (files, hunks, `/dev/null`, a removed `-- ` line), `\r\n`, long-line clipping, and the mock's errors.
- `StubURLProtocol.Stub` gained `contentType`.
- 189/189 unit tests pass on iPhone 17.
- Screenshots checked on mock data: MultiEdit diff, file view, codex diff, image, Markdown, and tappable rows.
- Checked end to end against r10-bridge's :7883 (w14:p2, read only, no prompts): the Edit diff with numbers from the real file, a Write outside the cwd, and a failed Read showing "File not found".
- `-demo file:<toolCallId>` opens a call's viewer at launch, for screenshots and r10-qa's UI tests. Identifiers: `toolStepFile`, `fileViewer`, `fileViewerTabs`, `fileViewerDiff`, `fileViewerText`, `fileViewerMarkdown`, `fileViewerImage`, `fileViewerError`, `fileViewerDone`, `fileViewerMenu`, `diffLine`, `codeLine`, `diffHunk`, `diffFile`.

## Known limits
- No syntax highlighting. `language` is only used to pick Markdown and prose wrapping.
- In no-wrap mode, lines are sized from the longest line in monospace digits, so very wide characters (CJK, emoji) can end in "…". Wrap Lines shows them in full. Lines over 4000 characters are clipped.
- Selecting text works one line at a time.

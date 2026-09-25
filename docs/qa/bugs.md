# Round 5 QA bugs

| id | severity | owner | status | title |
|---|---|---|---|---|
| QA-1 | P3 | ios-polish | closed (no repro) | Sidebar filter menu: picker items don't show up within timeout after tapping Filter (was sim contention) |
| QA-2 | P3 | tests-cleanup | fixed (round 6) | Attachment filename sanitizing drops everything before a `/` instead of replacing it with `-` |

Details below, one section per bug.

## QA-1: Sidebar filter menu picker items don't appear in time

- Severity: P3 (downgraded from initial P2 filing — see Update/Closed below)
- Owner: ios-polish (`Relay/Features/Sidebar/SidebarView.swift`, `FilterButton`)
- Repro: `xcodebuild test -scheme Relay -destination 'id=DF58D9CD-944D-48A9-8682-41AB2100C106' -only-testing:RelayUITests/Round2UITests/testFilterMenuShowsFlatSessions` (mock backend, `-agent w2:p1`). Open sidebar, tap the `sidebarFilter` button (the orange-dot "needs input" filter icon), then look for the `Ready for review` menu item.
- Expected: the filter `Menu`'s inline `Picker` items ("Ready for review", "Needs input", "Working", …) appear within ~3s of tapping the filter button (per `docs/api.md` / round-2 report expectations).
- Actual: reproduced twice, two different ways:
  1. Full-suite run: `XCTAssertTrue failed` at `Round2UITests.swift:68` — `app.buttons["Ready for review"]` never appears within 3s.
  2. Isolated run (same test alone, less machine load): the UI test runner itself hung and had to restart ("Restarting after unexpected exit, crash, or test timeout") during this test, with no clean pass/fail recorded.
- The row titles/symbols in `SidebarModel.swift`'s `SessionFilter` look correct (`readyForReview` → "Ready for review"), so this looks like a Menu-opening/timing issue in `FilterButton`, not a missing string. Possibly a `glassEffect`/animation delay pushing past the test's default open time, or a genuine hang.
- Update: ios-polish reproduced this in isolation and found the xcresult shows "Test crashed with signal kill" (watchdog SIGKILL), not an assertion failure — consistent with 5 concurrent Claude sessions hammering the same simulator/xcodebuild at once (I independently hit the same "Restarting after unexpected exit, crash, or test timeout" pattern on unrelated tests during the same window).
- Closed: ios-harden confirmed on an isolated retest (no other builds running) that `testControls` alone passes, and full `MockUITests` passes 5/5 twice in a row. No app bug. Same explanation applies to the one-off `NewChatUITests.testCodexWithModelAndEffort` failure seen in the same contended window — not filed.

## QA-2: Attachment filename sanitizing treats `/` as a path separator, not a sanitized character

- Severity: P3 (no security impact found, just a spec/behavior mismatch)
- Owner: bridge-harden (upload name sanitizing, bridge-side)
- Repro (against the live bridge on 7878, e2e-test agent `w14:p2`):
  ```
  curl -X POST -H "Authorization: Bearer $(cat ~/.relay/token)" -H "Content-Type: text/plain" \
    -H "X-Filename: a/b.txt" --data-binary @somefile \
    http://127.0.0.1:7878/agents/w14%3Ap2/attachments
  ```
- Expected (per `docs/api.md` Attachments § Name sanitising): "characters outside `A-Z a-z 0-9 . _ -` become `-`" — so `a/b.txt` should sanitize to `a-b.txt`.
- Actual: returns `name: "b.txt"` — the `/` is treated as a real path separator (lastPathComponent-style) and everything before it is silently dropped, not replaced with `-`.
- Security check done as part of this: tried `X-Filename: ../../../../tmp/relay_traversal_marker.txt` — resolves to `relay_traversal_marker.txt` with no file written outside the uploads dir. **No path traversal**; the lastPathComponent-style handling is actually safe, just undocumented/inconsistent with the stated sanitizing rule. Recommend either fixing the code to match the doc (replace `/` with `-` before sanitizing further) or updating `docs/api.md` to describe the actual (safe) behavior.
- Fixed (round 6, tests-cleanup): the code now matches api.md. `UploadStore.sanitize` no longer takes `lastPathComponent` first; `/` becomes `-` like any other disallowed character, so `a/b.txt` is `a-b.txt` and `../../etc/passwd` is `etc-passwd`. The traversal guarantee holds because only `A-Z a-z 0-9 . _ -` survive, leading/trailing `.`/`-` are trimmed, and the stored name is `<16-hex id>-<name>`. Tests: new `UploadsTests.sanitize` cases for `/`, `\`, `/` alone and `../..`; `slashNamesStayInsideTheUploadFolder` saves real files for traversal-style names and checks they land directly in the agent's upload folder; the existing 1000-name fuzz (`uploadNameSanitizeIsAlwaysSafe`) still passes. Not re-tested against the live bridge (tests-cleanup doesn't redeploy it); it ships with live-tool-bridge's next rebuild.

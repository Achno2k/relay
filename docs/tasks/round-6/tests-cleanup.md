# tests-cleanup: make the bridge test suite trustworthy, and fix QA-2

1. **The flaky suite.** The full `swift test` fails about 3 in 25 runs with cross-suite errors:
   - `NewAgentTests.swift:88`: "read: Bad file descriptor"
   - `InputClearingTests.swift:46`: "herdr closed the connection"
   - `RoutesTests.swift:106`: an empty agent list

   A partial fix is on branch `wip/fakeherdr-fd-race` (commit a78f4cc). It adds claim-once closes in FakeHerdr and makes init wait for the accept thread. It helped but didn't cure it.
   - Start from that branch's idea on `master`, and find the **actual** remaining race. Candidates:
     - socket paths reused across suites (UUID collisions or shared temp dirs);
     - fds handed to a Thread after `stop()`;
     - a connection fd closed by both the reader thread and `stop()`;
     - the bridge's own HerdrClient closing an fd twice. **If it's in production code, that's a real bug: fix it there and say so.**
   - Instrument if needed: log fd open/close with the suite name.
   - **Done means 50 consecutive full-suite runs pass** (`for i in $(seq 50); do swift test || break; done`). Don't serialise every suite as the fix.
   - When merged, delete the wip branch.
2. **QA-2** (`docs/qa/bugs.md`): attachment filename sanitising drops everything before a `/` instead of replacing it with `-`, as api.md specifies. Make the code match api.md (or fix api.md if the current behaviour is better, and say why). Add tests. Keep the no-path-traversal guarantee. Mark QA-2 fixed in bugs.md.

# live-typing report

## What changed

**Bridge** (`bridge/Sources/RelayCore/`, all new files):
- `LiveReplyParser.swift` — pure text extraction from a pane's visible screen, per agent kind:
  - claude: the last `⏺` block. Falls back to the last non-chrome paragraph when the block's own
    marker has scrolled off the read window (see "Bugs found" below — this is the main case that
    makes live typing visible at all for anything longer than a couple of lines).
  - codex: the last `• ` bullet. Codex doesn't stream — it shows a `• Working (…)` placeholder and
    then prints the whole reply at once — so in practice this fires once, just before the transcript
    message lands.
  - pi: the last non-chrome paragraph while pi's own `Working` spinner isn't on screen. Same story as
    codex: pi renders in one piece, not incrementally.
  - Shared: tool calls (`⎿`/`└` results, bare `Name(args)` signatures) are excluded; chrome (rules,
    spinners, footers, prompts, token-usage lines) is filtered before picking the tail paragraph;
    wrapped lines are rejoined, blank lines kept as paragraph breaks.
- `LiveReplyTracker.swift` — an actor: throttles to one frame per ~250ms per agent, dedupes unchanged
  text, and suppresses (then clears) the preview once the transcript's own text has caught up to or
  passed what's on screen.
- `LiveReplyMonitor.swift` — a `Service`: polls every agent currently `working` (one `agent.read` per
  agent per tick), only while `EventHub.count > 0`, feeds `LiveReplyParser` → `LiveReplyTracker`, and
  broadcasts `reply.live` on the hub.

**Hooks in shared bridge files** (all additive, all reviewed live with bridge-harden and merged into
their round-5 hardening commit `2e703b6`, credited there):
- `AgentMonitor.swift`: two optional init closures, `onSnapshots` (called from `refresh()` with the
  same snapshots it already fetched) and `onMessage` (called from the tailer callback alongside
  `hub.broadcast(.messageUpserted)`). Both default to `nil`; existing behavior and tests untouched.
- `RelayApp.swift`: constructs `LiveReplyMonitor` and wires the two closures; adds it to the service
  group.
- `Models.swift`: new `ServerEvent.replyLive(agentId:text:seq:)` case + its `Encodable` branch.

**iOS** (`ios/Relay/Features/Chat/LiveReplyView.swift`, new):
- Renders the live text via the existing `MarkdownView` (same style as a landed `.text` row), with an
  `.animation(value: markdown)` crossfade rather than forcing a full view-identity reset per update
  (an early version used `.id(markdown.count)`, which rebuilt the whole markdown tree on every ~250ms
  frame and made the simulator hang under XCTest's constant accessibility polling — see "Bugs found").

**iOS hooks** (already committed by ios-harden in `9392bca`, credited there for `AppStore.swift`;
`ChatView.swift`/`RelayState.swift` also carry it):
- `RelayKit/Models.swift`: `ServerEvent.replyLive` case + decode branch.
- `AppStore.swift`: `liveReplyText: [String: String]`, applied in `apply(_:)` with a per-agent `seq`
  guard against out-of-order frames; cleared on `.agentClosed`.
- `RelayState.swift`: `.replyLive` is a no-op in the pure reducer (compiler-forced exhaustive case).
- `ChatView.swift`: where the pulsing dot used to be unconditional while working with no assistant
  text yet, it now shows `LiveReplyView` instead when `store.liveReplyText[agent.id]` is non-empty —
  same list slot, so the real `.text` row replaces it with no layout jump once it lands.
- `Mock/MockBackend.swift`: `.replyLive` case (compiler-forced); still entangled with ios-polly's
  `.usageUpdated` addition on the same switch line as of this report — see "Left open."

**`docs/api.md`**: new "Live reply" section (written first, per the brief), plus the `reply.live`
line in the `/ws` frame list.

## Bugs found live (the interesting part)

1. **herdr refuses `recent_unwrapped` reads on a working agent.** My first implementation polled with
   `source: recentUnwrapped` (matching the brief's suggestion for handling wrapped lines) and got
   `agent_not_idle: ... its alternate-screen history can only be captured by scrolling while idle`
   on every single read — silently swallowed by `try?`, so it looked like "the whole thing does
   nothing" rather than an error. herdr's error message says exactly what to do: use `--source
   visible`. Fixed by switching to `.visible`, which is also all a *live* preview needs (the tail);
   scrollback matters for `/messages`, not for this.
2. **The claude parser only found text when the block's `⏺` marker was still on screen.** Once a
   reply grows past ~2 paragraphs the marker scrolls off a `visible`-sized window and the parser
   returned `nil` — indistinguishable from "nothing to show" — for the rest of the turn. This is
   exactly the "scrolled-off text" case the brief calls out ("it's OK to show only the tail"). Fixed
   with a fallback: when no marker is found, filter chrome (rules, spinners, the prompt echo, tool
   results) and take the last paragraph still on screen.
3. **Claude Code's "Update available!" banner can bleed into a content line.** It's drawn at a fixed
   row; once the alternate screen scrolls under it, a clean text read can return it concatenated onto
   real paragraph text instead of on its own line (seen live, screenshotted below). Chrome-line
   filtering doesn't catch a banner that's *not* its own line, so `LiveReplyParser` now also strips
   `"Update available!" onward` from every line before anything else runs. This is a best-effort
   mitigation, not a full fix — see "Left open."
4. **`.id(markdown.count)` in an early `LiveReplyView` hung the simulator.** Forcing a full SwiftUI
   subtree rebuild on every ~250ms text update, combined with XCTest's constant accessibility-tree
   polling while a UI test waits on an element, made the app appear to hang and get killed/restarted
   by the test harness ("Restarting after unexpected exit, crash, or test timeout"). Switched to a
   plain `.animation(value:)` crossfade; no more rebuilds, no more hangs.
5. **Contention on the shared `w14:p2` e2e agent.** Round 5 runs five sessions in parallel and several
   of us use the same `w14:p2`/`w14:p4`/`w14:p5` test agents; early live runs against `w14:p2` showed
   interleaved, out-of-order events from other sessions' turns. Once I switched to a dedicated throwaway
   pane (`w14:pG`, closed after), results were clean and reproducible.

## Tests

- **Parser** (`LiveReplyParserTests.swift`, 17 tests): claude growing/multi-paragraph/tool-call/
  tool-result/spinner-only/short-answer/scrolled-off-fallback/banner-bleed cases; codex working-
  placeholder/tool-call/reply-bullet/model-change-confirmation; pi working-spinner/trailing-paragraph/
  chrome-only; unknown kind. All fixtures are synthetic, built from real screen captures taken live
  against `e2e-test`/`bridge-codex`/`bridge-pi` (see "Live" below) and then reproduced as literal
  strings, not copied verbatim from any transcript.
- **Tracker** (`LiveReplyTrackerTests.swift`, 9 tests): first offer sends immediately; unchanged text
  isn't resent; throttling within `minInterval`; a landed transcript text suppresses a matching or
  shorter offer; `stopped()`/`landed()` clear and are no-ops when nothing was live; `seq` keeps
  increasing across offers and clears; independent per-agent state.
- **Monitor integration** (`LiveReplyMonitorTests.swift`, 2 tests): wires `AgentMonitor` →
  `LiveReplyMonitor` → `EventHub` against a `FakeHerdr`, the same construction `RelayApp` uses. A
  working agent's screen produces a growing `reply.live` frame; going idle clears it. This is what
  caught bug #1 could *not* have been caught by (`FakeHerdr` doesn't reject `recentUnwrapped` the way
  real herdr does) — it proved the wiring was right so I knew to keep debugging live instead of
  rewriting the pipeline.
- **iOS unit** (`LiveReplyTests.swift`, 6 tests, `@MainActor`): `AppStore.apply(.replyLive)` stores,
  grows, clears on `nil`, ignores a stale `seq`, keeps agents independent, and clears + resets its
  seq counter on `.agentClosed`; `RelayState.apply(.replyLive)` is a no-op.
- Bridge: 205/205 total (`swift test`). iOS: 84/84 unit, 5/5 `MockUITests` (no regressions from the
  `ChatView` hook).

### Live (real bridge, dedicated `w14:pG` claude agent, closed after)
- `LiveReplyUITests.swift` (`RelayUITests`, gated on `RELAY_E2E_LINK`/`RELAY_E2E_AGENT` like
  `LiveE2ETests`): sends a long no-tool prompt, asserts the `liveReply` accessibility element appears
  with text, keeps changing while the agent works, and is gone (replaced by the real row) once the
  turn finishes. Passed twice against the real bridge (bug #3's fix verified between runs).
  Screenshots: `docs/screenshots/live-typing-{1-growing,2-grown,3-landed}.png`.
- A raw WebSocket probe (not committed — a scratch script) against the same agent for a 500-word
  reply: 29 `reply.live` frames over a 14s working window, text length growing 121 → 3409 chars,
  clean `text: null` at the same instant `message.upserted` landed.
- **CPU**: sampling `relay`'s `%cpu` every 0.5s during a ~20s reply on the dedicated agent (with ~20
  other agents live on the same herdr session from the other round-5 sessions, not attributable
  solely to this feature): average 1.6%, peak 11.8%.
- **Screen recording**: attempted `xcrun simctl io recordVideo` for `docs/screenshots/live-typing.mov`
  per the brief, but another round-5 session held the simulator's (host-wide, not per-device) recording
  lock for the rest of the session, so no `.mov` shipped this round. The three PNG screenshots above are
  the visual evidence in its place.

## Left open
- **Banner-bleed mitigation is best-effort.** When "Update available!" lands on the *same* screen row
  as real text (an ANSI overlay collision, not two separate lines), the bytes can already be
  interleaved character-by-character by the time herdr hands back plain text; stripping from the
  banner's start position only cleanly recovers the common case where the two texts were merely
  concatenated, not interleaved. A full fix would need ANSI-position-aware capture, out of scope here.
- **`Mock/MockBackend.swift`** still has my `.replyLive` case on the same switch line as ios-polish's
  in-progress `.usageUpdated` (uncommitted as of this report); asked them to take the whole file in
  their next commit rather than fight `git add -p` over one line.
- **pi/codex don't stream**, so their live preview is real but typically shows for well under a
  second, right before the transcript message lands — the parser support is there and tested, but
  don't expect the same "growing paragraph" feel Claude gets.
- Did not attempt to reduce the poll interval below ~250ms or otherwise tune throttling beyond the
  brief's "about 4 per second"; 1.6% average CPU on a busy shared session suggested there was no
  pressure to.

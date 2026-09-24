# live-typing: show the reply while the agent writes it

**Problem:** Claude only writes a text block to its transcript once the block is complete, so the app shows nothing (just the pulsing dot) until a whole paragraph lands. The goal is ChatGPT-style text that grows as it's written.

**Approach:**
- While an agent is `working`, the bridge reads the pane's visible screen, extracts the in-progress assistant text (Claude's `⏺` block, codex's `•` lines, pi's output area), and cleans it: strip ANSI, spinner/footer/status lines and box-drawing, and scrub paths.
- It sends `{"type":"reply.live","agentId","text","seq"}` over `/ws`, throttled to about 4 per second, and only when the text changed. When the matching transcript message lands, or the agent stops working, it sends `reply.live` with `text: null`.
- The app renders the live text as the tail of the last assistant message, in the same markdown style, with a soft fade-in on new words. It's replaced cleanly with no jump when the real message arrives. Tool activity stays as it is.

**Rules:**
- The live text is a preview only. Never persist it, and never let it duplicate the transcript text (dedupe against what's already there).
- CPU: one `agent.read` per working agent, at most 4 per second, only while at least one `/ws` client is connected. Measure the bridge's CPU during a long reply and put the number in the report.
- Handle wrapped lines (use herdr's `recent-unwrapped` source if it helps), scrolled-off text (it's OK to show only the tail), and all three kinds.
- api.md gets a "Live reply" section first.

**Tests:**
- Parser tests on synthetic screen captures for claude/codex/pi, including a spinner, a tool call in progress, and a wrapped code block.
- Live: ask w14:p2 for a 300-word answer with no tools and assert the app shows growing text before the transcript message arrives. Record a short video to `docs/screenshots/live-typing.mov`.

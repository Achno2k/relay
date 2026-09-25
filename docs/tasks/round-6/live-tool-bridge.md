# live-tool-bridge: tool calls must not show up as flickering live text

**User bug:** in a chat, when the agent runs a tool (e.g. Bash), the command renders as normal text and "glitches back and forth". After about 5–6 s it switches to the proper "Running Bash…" row.

**Likely cause:** the live-reply screen parser (`LiveReplyParser`) picks up Claude's tool line (`⏺ Bash(ls -la)`, plus its `⎿` output and the spinner lines) as in-progress assistant text and sends it as `reply.live` text. The screen redraws the tool block while it runs, so the text flickers. Claude writes the `tool_use` to the transcript only a few seconds later, and then `message.upserted` brings the real toolCall row.

**First, reproduce and confirm:** on w14:p2, prompt a slow tool (e.g. `ping -c 8 127.0.0.1` via Bash) and record the `/ws` `reply.live` frames with timestamps next to the screen captures. Do the same for codex (w14:p5) and pi (w14:p4) tool calls.

**Fix:**
- The parser recognises tool blocks per kind:
  - claude: `⏺ Name(args)` with `⎿` output, and the "Running…"/spinner lines;
  - codex: `• Running …` / `• Ran …`;
  - pi: its tool lines.
- Tool text must **never** be sent as reply text.
- Send a structured live tool state instead:
  - `{"type":"reply.live","agentId","seq","text": <assistant prose only, or null>,"tool": {"name":"Bash","summary":"Ran ping -c 8 127.0.0.1","state":"running"} | null}`.
  - `summary` is scrubbed the same way as the transcript toolCall summaries, so the app can match them.
  - Clear `tool` once the transcript's toolCall for it lands, or the agent stops working.
- Debounce so redraws can't make the text flip-flop. Text must only grow, or be replaced once by the transcript. Never alternate between two states.
- **api.md "Live reply" first**, then tell `live-tool-ios` the exact shape.
- Tests: synthetic screen captures for claude/codex/pi mid-tool (spinner, partial output, wrapped command) and a sequence test proving there's no flip-flop.
- Live-verify, rebuild, kickstart (announce first), and tell live-tool-ios when it's live.

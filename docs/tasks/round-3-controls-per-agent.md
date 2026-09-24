# Round 3: model/effort controls for pi and codex

User report: model and effort changes work for Claude but not for **pi** or **codex** agents. Controls are Claude-specific today (`/model <alias>`, `/effort <level>`, Claude's footer, and Claude's model aliases in `GET /controls`).

Installed here: `pi` 0.85.1 (`/opt/homebrew/bin/pi`), `codex-cli` 0.156.1.
- **pi**: `--model provider/id[:thinking]`, `--thinking off|minimal|low|medium|high|xhigh|max`, `--list-models`. Interactive: check its `/model` selector, Ctrl+P model cycling, and whatever key or command changes the thinking level. The session JSONL path comes from herdr (`agent_session.kind == "path"`) and likely records model/thinking changes.
- **codex**: `-m/--model`, `-c model_reasoning_effort=...`. Interactive: `/model` opens a picker for the model and then the reasoning effort. Session rollouts live under `~/.codex/sessions/**.jsonl` and contain `turn_context` with the model and effort. Check the permission/approval mode too (`/approvals` or `/permissions`) as the "mode" equivalent.

## Bridge (herd-bridge)
1. **Investigate live first.** Use throwaway panes in the `herd-e2e` workspace only. Start `pi` and `codex` there, find exactly how each changes model and effort interactively, and how to read the current values (screen footer and/or session file). Write the findings into the bridge report before coding. Close the throwaway panes after.
2. **One driver per kind.** Refactor controls behind a `ControlDriver` protocol (`ClaudeDriver`, `PiDriver`, `CodexDriver`). Each one:
   - reads the current `model`, `modelLabel`, `effort`, and `permissionMode` if the kind has one;
   - applies a change, confirms it on screen or in the session file, and times out cleanly;
   - drives pickers with arrow/enter keys when there's no direct command, re-reading the screen after each step rather than counting blindly.
3. **Per-agent capabilities.** Add `GET /agents/:id/controls` returning `{"models":[{"id","label"}], "efforts":[...], "modes":[...], "supports":{"model":bool,"effort":bool,"mode":bool,"compact":bool,"clear":bool}}`.
   - Model lists come from the agent itself: `pi --list-models` (cache it, and let the user-configured/scoped models float to the top) and codex's `/model` picker entries, cached per kind.
   - Keep `GET /controls` as Claude's list for backward compatibility, but the app moves to the per-agent route.
   - Unsupported controls return `400 unsupported` with a clear message.
   - Update api.md first.
4. `Agent.model` / `effort` must be filled for pi and codex agents too (the app shows them in the title pill).
5. Tests use synthetic pi/codex session files and screen captures. Live-verify each control on real throwaway pi and codex panes, and record the results in the report.

## iOS (herd-ios)
- The title-pill menu uses `GET /agents/:id/controls` for the open agent (cached per agent, refreshed when the agent changes kind or session). It shows only the supported sections, with that agent's own model and effort lists (pi's list can be long: use a submenu with the current model first, then the rest, and a search sheet if there are more than about 15).
- Effort labels come from the bridge (pi: off…max, codex: minimal/low/medium/high/xhigh …).
- Compact/clear appear only if supported.
- Mock fixtures for a pi and a codex agent. Unit tests plus mock UI tests.
- Live UI test: switch model and effort on a throwaway pi pane and a throwaway codex pane (herd-bridge tells you their ids, or creates them via `POST /agents` with `kind`).
- Final UI run on the **physical device** if the Xcode account is signed in again; if it's still signed out, run on the simulator and say so.

Same process rules: api.md first, commit only your own paths, and add a "Round 3" section to each report. herd-bridge tells herd-ios when the endpoint is live on 7878 (kickstart the launchd service after a release build).

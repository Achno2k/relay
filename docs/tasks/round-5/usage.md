# usage: subscription usage limits screen

User request: a screen showing current usage limits for their subscriptions: **Claude** (Claude Code / Claude plan), **Codex** (ChatGPT plan), and pi, which runs on the same ChatGPT subscription via its `openai-codex` provider.

**Hard requirement:** it must work for *any* Relay user out of the box. No reliance on the user's custom status line, and no manual setup.

## Phase 1: research (report before building)
Find the most official, stable source each harness exposes. Write the findings to `docs/tasks/round-5/usage-research.md`: source, fields, freshness, auth needed, stability risk, and a recommendation. Then message the lead (herdr agent `herd-native`, pane w13:p1) and **wait for a go-ahead before Phase 2.** Candidates to check:
- **Codex**
  - Rollout files `~/.codex/sessions/**/rollout-*.jsonl`: `event_msg` token_count carries `rate_limits` (`primary`/`secondary` `used_percent`, `window_minutes`, `resets_at`, `plan_type`, `rate_limit_reached_type`, credits). Local and general, but only as fresh as the last turn.
  - `codex app-server` (experimental): look for an account or rate-limits method (e.g. `account/rateLimits/read`) that returns them on demand. Check `codex app-server --help`, its generated schema/TS bindings (`codex app-server generate-ts` or similar), and docs.
- **Claude Code**
  - Is there any official, non-interactive way to get plan usage? Check `claude --help`, subcommands, `-p` with `/usage` or `/status` (does print mode render them?), `--output-format json`, SDK/hooks docs, and whether hook or status-line input is the only place `rate_limits` appears.
  - The interactive `/usage` screen: what it calls. It's probably Anthropic's OAuth usage endpoint using Claude Code's stored credentials (the macOS Keychain item). Assess that option honestly: undocumented API, reading the user's OAuth token, ToS/stability risk. **Don't read the Keychain or call the endpoint during research** without the lead's OK. Documentation and reading Claude Code's own help output are fine.
  - A fallback that needs no credentials: drive `/usage` in a headless/hidden pane and parse the screen. Assess its cost and fragility.
- **pi**: confirm it shares ChatGPT limits with codex, or has its own view (`pi` commands, its session files).

## Phase 2: build (after the go-ahead)
- **Bridge:**
  - New `bridge/Sources/RelayCore/Usage*.swift`, plus `GET /usage` → `{"providers":[{"id":"claude","label":"Claude","plan":"Max","windows":[{"id":"5h","label":"5-hour","usedPercent":12.5,"resetsAt":"…"},{"id":"week",…}],"updatedAt":"…","source":"…","stale":false}]}`.
  - A `usage.updated` WS event.
  - Cache with sensible refresh intervals; never hammer a remote API.
  - api.md "Usage" section first.
- **iOS:**
  - New `ios/Relay/Features/Usage/`, with a **Usage** entry in the sidebar (coordinate the row/button placement with ios-polish).
  - One glass card per provider: 5-hour and weekly rings or bars coloured by level, a reset countdown, the plan, "updated x ago", and a clear stale/unavailable state with the reason.
  - Store code in a new `UsageStore.swift` (tell ios-harden).
- Tests plus a live check against the real data on this Mac; screenshots dark and light.

Same round-5 rules (README.md): own your paths, commit only them, and write a report at the end.

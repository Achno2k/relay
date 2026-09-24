# usage-pi: Phase 1 research

Mac: aman0singh's, 2026-09-25. Read `usage-report.md`, `usage-research.md`, api.md "Usage", `UsageModels/Monitor/Sources/Parsers.swift`, `UsageView.swift`/`UsageStore.swift`. No credential values were printed or read; only key *names* (`ls`, JSON top-level keys) and `pi auth check` status output.

## What pi is actually logged into

`~/.pi/agent/auth.json` has three top-level entries: `anthropic`, `openai-codex`, `opencode-go`. Confirmed live (`pi auth check --provider <x> --json`, no key material):

- `openai-codex`: `{"status":"ready","authType":"oauth"}` — same finding as `usage-research.md`: same ChatGPT OAuth session Codex uses. Already covered by the existing `codex` provider card; just needs `"pi"` added to its `usedBy`.
- `anthropic`: `{"status":"invalid","reason":"invalid_state"}`. This is **pi's own** `/login` → "Claude Pro/Max" OAuth flow (see `providers.md`), a separate token store from Claude Code's own login, not a shared session — pi's auth.json entry has `type/refresh/access/expires` fields but no `accountId`-equivalent to cross-check against Claude Code's session, so identity can't be confirmed either way. Moot right now: it's `invalid` on this Mac, so pi isn't actually drawing on it. **Recommendation:** don't assume it's the same subscription as the `claude` card. If/when it's valid, treat it as a separate signal until we find a comparable id; worst case it just means "pi has its own Anthropic login" shown separately, not merged into the Claude Code card.
- `opencode-go`: `{"status":"ready","authType":"api_key"}`. **Not OAuth, not a subscription plan tied to a usage-percent API** — it's a flat API key (`OPENCODE_API_KEY` env var / `opencode-go` in `auth.json`, per pi's `docs/providers.md`), same mechanism as e.g. `xai` or `groq` keys. `settings.json` confirms `defaultProvider: openai-codex` (not opencode-go), so it's a secondary/model-variety provider, not pi's main driver.

`pi --list-models` lists ~30 models under `opencode-go` (deepseek, glm, gpt-*-luna, grok, kimi, qwen, etc.) — it's the "OpenCode Go" model-access plan (like OpenCode Zen but subscription-billed), not a per-provider passthrough of e.g. Anthropic's own limits.

## OpenCode Go usage source

- `opencode` CLI **is installed** on this Mac (`/opt/homebrew/bin/opencode`, v1.18.20) and independently authenticated to "OpenCode Go" (`opencode auth list` → one credential, type `api`, stored at `~/.local/share/opencode/auth.json` — separate file from pi's, key value not read).
- `opencode --help` / `opencode stats --help`: `opencode stats` reports **local token/cost stats from opencode's own session history** (`--days`, `--models`, `--project` filters) — it's a local ledger of what *opencode itself* has spent, not a call to a remote quota/limits endpoint. It wouldn't reflect usage pi puts through the same `opencode-go` key, and opencode's docs/CLI expose no `quota`/`rate-limit`/`usage` remote-fetch subcommand (checked `opencode auth --help`; no such verb).
- pi's own docs (`providers.md`, `usage.md` under pi's npm package) don't mention a rate-limit/quota API for `opencode-go` — it's documented purely as an API-key credential like any other key-based provider. No `x-ratelimit-*` header handling or quota fields showed up in searches so far (pi's real logic lives in bundled/minified chunk files under `dist/bundle/chunks/`, not yet grepped for those headers — flagging as unfinished, see below).
- I did **not** find a documented OpenCode Go quota/usage API from OpenCode's side. The only way I found to get real numbers would be either (a) calling OpenCode's backend directly with the stored API key, or (b) grepping pi's minified JS bundle for rate-limit header handling, which is source inspection (fine) but wouldn't itself produce live usage data.

**Per the brief's stop condition: this needs the stored `opencode-go` key (from pi's or opencode's auth store) to call any real usage/quota API, so I stopped here rather than reading the key or hitting an endpoint.** No key value was read or printed.

## Recommendation for Phase 2

1. **Provider model → per-subscription, not per-tool**, as the brief asks:
   - `claude` card: unchanged source/logic; add `usedBy: ["claude"]` (pi's `anthropic` entry is currently invalid, so pi isn't included yet — see above).
   - `codex` card: relabel from "Codex / pi" to whatever the new `label`/`usedBy` convention is; add `usedBy: ["claude-code"? no — "codex","pi"]` (i.e. `usedBy: ["codex","pi"]`) since `openai-codex` is confirmed shared.
   - new `opencode-go` card: `plan: "OpenCode Go"`, `usedBy: ["pi"]`, `windows: []`, `stale: true` (or a dedicated non-stale "no data" state — worth deciding in Phase 2), `unavailableReason: "Usage not available from OpenCode"` per the brief's fallback instruction, `source: "opencode-go (api key, no usage endpoint)"` for debuggability.
2. **Don't build a live OpenCode Go usage fetch in Phase 2** unless the lead wants to explicitly approve reading the stored key and calling an OpenCode endpoint (undocumented from what I found) — recommend shipping the "not available, here's why" card now and revisiting only if OpenCode publishes a documented quota API later.
3. Model `usedBy` as a straightforward `[String]` field on `UsageProvider`, populated by which harnesses are actually authenticated for that provider on this Mac (`pi auth check` per provider, cheap, no secrets) — not a static guess.
4. Keep polling/caching/backoff exactly as documented in api.md "Usage" for `claude`/`codex`; the new `opencode-go` card doesn't need a poller since there's nothing to fetch, just a static-ish card refreshed alongside the others.

Sending this to `herd-native` now and waiting for go-ahead before Phase 2. Have not read/printed any credential values; `opencode-go` live usage remains blocked on that explicit OK per the brief.

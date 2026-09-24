# usage: Phase 1 research

Mac: aman0singh's, 2026-09-25. All checks below were run live on this Mac (Codex app-server RPC call and `claude -p "/usage"` executed; no Keychain reads, no interactive-`/usage` screen scraping).

## Codex (and pi, same account)

**Recommended source: `codex app-server`, method `account/rateLimits/read`.**

- Spawn `codex app-server` (JSON-RPC 2.0 over stdio), send `initialize`, then `{"method":"account/rateLimits/read","params":{}}`.
- Confirmed live just now:
  ```json
  {"ordinaryUsageAllowed":true,"rateLimits":{"limitId":"codex","primary":{"usedPercent":39,"windowDurationMins":43200,"resetsAt":1792847958},"secondary":null,"credits":{"hasCredits":false,"unlimited":false,"balance":null},"planType":"free","rateLimitReachedType":null},"accountId":"…"}
  ```
- Fields: `primary`/`secondary` `RateLimitWindow` (`usedPercent`, `windowDurationMins`, `resetsAt` unix seconds — nullable, not every plan has both windows), `planType` (closed enum: free/go/plus/pro/team/business/enterprise/…), `rateLimitReachedType` (why blocked, if blocked), `accountId`, `rateLimitsByLimitId` for multi-bucket accounts, `credits`/`rateLimitResetCredits` for pay-as-you-go add-ons.
- There's also a push notification `account/rateLimitsUpdatedNotification` (sparse, merge into last snapshot) if we keep the app-server connection open instead of polling.
- **Freshness:** on-demand, live from OpenAI's backend — not dependent on a recent turn.
- **Auth:** none needed from us — app-server uses Codex's own stored ChatGPT OAuth session. We never touch `~/.codex/auth.json` directly.
- **Stability risk:** command is marked `[experimental]` in `codex --help`, and the method is versioned under `v2/` in the generated TS bindings — expect it to move but not vanish; it's the same call the Codex IDE/TUI itself uses for its usage display, so it's exercised in production, not a side channel.
- **Cost of running it:** app-server spawn + one round trip took well under a second in testing. Cheap enough to poll on an interval; still shouldn't be hammered (see Phase 2 caching note).
- **Fallback (no app-server, e.g. old Codex version):** parse `rate_limits` out of the newest `event_msg` `token_count` entry across `~/.codex/sessions/**/rollout-*.jsonl` (path per Codex's own dated layout). Same shape (`primary`/`secondary` `used_percent`, `window_minutes`, `resets_at`, `plan_type`) but only as fresh as the user's last Codex turn, and requires picking the right, most-recent file. Use only if `app-server` is missing or errors.

**pi:** `pi auth check --provider openai-codex --json` → `{"status":"ready","provider":"openai-codex","authType":"oauth"}` — confirms pi authenticates through the same `openai-codex` OAuth provider, i.e. the same ChatGPT account/session as Codex. No separate pi usage view exists or is needed; one `account/rateLimits/read` call covers both Codex and pi. Label it "Codex / pi (ChatGPT plan)" or similar rather than showing two identical cards.

## Claude Code

**Recommended source: `claude -p "/usage" --output-format stream-json --verbose`.**

- `/usage` is a *local command* (no model call — `"model":"<synthetic>"`, zero tokens/cost) and, unlike `/status`, it **does** run under `--print`. Confirmed live:
  - Human-readable form (`--output-format json`) returns a `result` string with session/weekly percentages and reset times as prose — parseable but brittle.
  - `--output-format stream-json --verbose` additionally emits a structured field on the assistant message, `usage_report.rate_limits`:
    ```json
    {
      "limits": [
        {"kind":"session","group":"session","percent":11,"resets_at":"2026-09-24T23:00:00...+00:00","scope":null,"severity":"normal","is_active":true},
        {"kind":"weekly_all","group":"weekly","percent":11,"resets_at":"2026-09-29T12:00:00...+00:00","scope":null,"severity":"normal","is_active":false},
        {"kind":"weekly_scoped","group":"weekly","percent":0,"resets_at":"...","scope":{"model":{"display_name":"Fable"}},"severity":"normal","is_active":false}
      ],
      "extra_usage": {"is_enabled":false,"monthly_limit":5000,"used_credits":0,"utilization":0,"currency":"USD"}
    }
    ```
  - This is the same data the interactive `/usage` screen renders, already typed and percent-based — no text scraping needed.
- **Freshness:** live call, on demand (not cached from a stale prior turn).
- **Auth:** none needed from us — the `claude` binary uses its own already-logged-in session (`claude auth status` shows `loggedIn`/`subscriptionType` for context, but carries no percentages). We never read the Keychain or call Anthropic's OAuth usage endpoint directly — this stays inside the CLI's own supported surface.
- **Stability risk:** `/usage` as a slash command isn't documented as a stable non-interactive API; it's a UI feature we're driving via print-mode, and its output shape could change between Claude Code releases. Still far more stable than screen-scraping the interactive TUI or reading Keychain + hitting an undocumented endpoint ourselves.
- **Cost:** spawning `claude -p` end to end took ~5.7s wall time in testing (process startup, hooks, etc.) for zero API cost. Too slow to call per-request from the iOS app; fine for the bridge to poll on an interval and cache.
- **Rejected:** driving `/usage` in a hidden interactive pane — same information, much more fragile (ANSI parsing, timing, an extra visible pane) for no data-quality gain over the print-mode call above.

## Recommendation for Phase 2

- Bridge polls both sources on an interval (propose 60–120s, plus refresh on explicit pull-to-refresh from iOS) and caches; never call per iOS request.
- Codex/pi: keep a long-lived `codex app-server` stdio connection if practical (get push updates for free); otherwise spawn-call-exit each poll — it's cheap.
- Claude: spawn `claude -p "/usage" --output-format stream-json --verbose`, parse the `usage_report.rate_limits.limits[]` array off the assistant message, map `kind`/`group` → our `windows[]` (`session`→ our "5h"-equivalent bucket — note Claude's session window isn't fixed at 5h, use its own `resets_at` — , `weekly_all`→"week"). Treat `weekly_scoped` (per-model, e.g. Fable) as an extra optional window, not required.
- Both sources can fail/be absent (Codex not logged in, Claude CLI not on PATH, etc.) — surface `stale`/unavailable with the reason per the `/usage` response shape already sketched in the brief, don't block the whole screen.

Sending this to `herd-native` (w13:p1) now and waiting for go-ahead before Phase 2.

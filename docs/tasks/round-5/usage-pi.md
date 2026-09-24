# usage-pi: show every subscription pi uses

User feedback: the Usage screen assumes pi = ChatGPT/Codex. Actually pi is logged into several providers (`~/.pi/agent/auth.json` keys; never print their values): `anthropic`, `openai-codex` and `opencode-go`. The default provider is `openai-codex` (`settings.json`). `pi --list-models` groups models by provider. For pi, the Usage screen must show whatever subscription(s) are actually in use, including **OpenCode Go**.

Read first: `docs/tasks/round-5/usage-report.md`, `usage-research.md`, the api.md "Usage" section, `bridge/Sources/RelayCore/Usage*.swift`, and `ios/Relay/Features/Usage/`.

## Phase 1: research (report to the lead, herd-native, before building)
- **Model the data per subscription, not per tool.** Cards: Claude (Anthropic plan), ChatGPT (Codex plan), OpenCode Go, and any other provider pi or the other harnesses are logged into. Each card lists "Used by: Claude Code · pi" etc. It shows only providers that are actually authenticated/configured on this Mac.
- **pi's anthropic provider:** is it the same Claude subscription/OAuth as Claude Code? If yes, it's the same card with pi added to "Used by". Confirm without printing secrets (e.g. compare the account/org id if pi exposes one, or just document the assumption).
- **OpenCode Go usage:** find the most official source.
  - The `opencode` CLI, if installed (`opencode --help`, `opencode stats`, `opencode auth`).
  - OpenCode's docs for Go plan limits and any usage/quota endpoint.
  - pi's own opencode-go provider code (under `/opt/homebrew/lib/node_modules/...`, or wherever pi is installed), and whether pi reads rate-limit headers or quota.
  - Response headers from a normal request (e.g. `x-ratelimit-*`), which pi may already log in its session files.
  - Plan name and limits.
- **Credentials:** if the only way needs the opencode-go key from pi's auth.json (or any stored credential) to call an API, **stop and report**. Don't read the key or call the API until the lead confirms the user approved. Reading docs, CLI help and pi's source is fine.
- If no usage source exists, the card still shows the plan/provider with "Usage not available from OpenCode", and says why.
- Write the findings to `docs/tasks/round-5/usage-pi-research.md` and message herd-native with the recommendation. **Wait for a go-ahead.**

## Phase 2: build (after the go-ahead)
- Extend `GET /usage` to per-subscription providers with a `usedBy: ["claude","pi"]` list. Keep it backward compatible where cheap. api.md first.
- iOS: card titles by subscription, plus a small "Used by" line with the agent-kind icons, and the OpenCode Go card. Keep the new minimal bar style (`UsageView.swift`, commit bd61787).
- Tests, a live check on this Mac, screenshots dark and light, and a report at `docs/tasks/round-5/usage-pi-report.md`.

Same round-5 rules (README.md). Owned paths: `bridge/Sources/RelayCore/Usage*.swift`, `ios/Relay/Features/Usage/`, `ios/Relay/Stores/UsageStore.swift`, their tests, and api.md "Usage".

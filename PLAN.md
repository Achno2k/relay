# Plan · v1

## Goal
`agents init` sets up an EC2 box with harnesses, herdr, and a dev env for a repo. A Slack bot on the box drives agents in herdr panes. Laptop attaches to the same panes.

## Decisions (final)
- Go 1.24, cobra, charmbracelet (huh, bubbletea, lipgloss), aws-sdk-go-v2, slack-go socketmode, modernc sqlite, BurntSushi/toml.
- Bot runs on the box under systemd. Socket Mode, no inbound ports.
- One thread = one session. Mention in unbound thread creates session; reply continues. `@bot new`, `@bot attach <name>` override.
- Bot posts: status emoji on thread root, trimmed final reply (~1500 chars), one "picked up on <host>" line on terminal takeover. Files only on `@bot diff|show|full|pr`.
- Worktree per session at ~/work/<repo>/<id>. Agent name `<repo>-<id>`.
- Env plan: harness generates JSON plan once, user approves, CLI replays. Failed step → harness repairs, 3 tries. Cached at ~/.agents/plans/<repo>.json.
- Base tools (ours, no LLM): git, gh, mise, herdr, harnesses. Runtimes via mise.
- TTL 24h idle → park (kill agent, keep worktree). Max 5 live. Never delete dirty worktree.
- Ubuntu 24.04 only. Single user. Slack user allowlist.

## UI direction
Minimal. Monochrome base, one accent (soft orange, lipgloss "#E07A4F" / adaptive). Green ✓, red ✗, amber !. No borders or boxes. Aligned columns. Step runner matches the reference in internal/ui/ui.go doc comment.

## Work split (one session each)
| session | package(s) | deliverable |
|---|---|---|
| ui | internal/ui | theme + every function in ui.go, plus hidden `agents ui-demo` that exercises all of them |
| aws | internal/awsx, internal/sshx | profile/SSO picker, region, instance picker, ssh/ssm runner, `agents ssh` command |
| bootstrap | internal/bootstrap, internal/harness (install/login parts) | bootstrap.sh, systemd units, harness login relay, `agents init` orchestration |
| env | internal/envplan, internal/harness (HeadlessJSON) | generator, executor, cache, `agents env` (setup/verify/regen/show) |
| core | internal/herdr, internal/state, internal/worktree | herdr client, sqlite store, worktree ops, `agents sessions` (list/kill/park) |
| bot | internal/slackbot | socket mode bot, router, prompt builder, watcher, buttons, `agents bot` |
| tooling | scripts, .goreleaser.yaml, Makefile, README, `agents attach`, `agents version` | install script, release, attach flow (writes attached_by, execs herdr --remote) |

## Commands
- `agents init`           full first-run flow
- `agents env [verify|regen|show]`
- `agents ssh`            plain shell on the box
- `agents attach <session|agent>`
- `agents sessions [list|kill|park] `
- `agents bot`            runs on the box
- `agents ui-demo`        hidden

## init flow (bootstrap session orchestrates, calls others)
1. awsx: profile → region → instance. Save config.
2. sshx: connectivity check.
3. ui.MultiSelect harnesses.
4. bootstrap: upload + run bootstrap.sh (step runner). Installs base tools, herdr server unit, agents binary.
5. harness login over interactive ssh, one per selected harness. Skip if IsLoggedInCmd passes.
6. repo: detect from cwd `git remote get-url origin`, confirm, clone on box.
7. envplan: generate → approve → execute → verify.
8. Slack tokens (optional): ui.Input ×2, write ~/.agents/slack.env on box, enable agents-bot unit.
9. Print summary: ssh line, attach line, bot status.

## Contract changes
(append here: date, who, what)

## Box-side vs laptop-side
Commands that need harness auth or the herdr socket run ON THE BOX. When run on the laptop they proxy themselves: `sshx.Interactive("agents <same args>")`. Detect box with env AGENTS_ON_BOX=1 (set in the box's shell profile and systemd units by bootstrap). Applies to: `agents env`, `agents sessions`, `agents bot`.
`agents attach <id>` (laptop): marks attached_by over ssh, then execs `herdr --remote <user>@<host>`.

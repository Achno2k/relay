# Handoff: Relay (native iOS app for driving herdr agents)

## What this is
- **Relay** is the user's personal iOS app for driving the coding agents (Claude Code, Codex, pi) in their **herdr** terminal session from the phone. Repo: `~/projects/relay`, branch `main`, remote `origin` = `github.com/Achno2k/relay` (**public**, SSH alias `github-personal`). Released as `v0.1.0`.
- `bridge/`: the Go binary `relay`. Module `relay`, `cmd/relay`, packages in `internal/`.
  - As the bridge (`relay serve`) it talks to herdr's unix socket, tails transcripts, and serves REST + WS. Runs on macOS and Linux.
  - Since round 11 it is also the laptop CLI that sets up an EC2 box (`init`, `doctor`, `ssh`, `attach`, `deploy`, `reset`, `env`). This was agents-cli, now merged in. See `docs/relay-cli.md`.
- `ios/`: the SwiftUI app.
  - iOS 26, Liquid Glass, ChatGPT feel.
  - XcodeGen: `ios/project.yml` → `Relay.xcodeproj`, scheme `Relay`. Shared code lives in the `RelayKit` package.
  - The bundle id stays `dev.amansingh.herd`.
- Docs:
  - `docs/api.md`: the contract. Change it first.
  - `AGENTS.md`: repo rules.
  - `docs/remote-setup.md`: Linux VM setup.
  - `docs/tasks/round-*/`: briefs and reports.
  - `docs/qa/`: bugs.
  - `docs/research/relay-competition.md`: the monetization research.

## Running system (2026-10-02)
- **Bridge (Mac):**
  - LaunchAgent `com.relay.bridge` runs `bridge/bin/relay serve --port 7878 --require-tailscale` (the binary is gitignored).
  - It binds the Mac's Tailscale IP and localhost.
  - Data lives in `~/.relay` (token, uploads, `relay.log`).
  - Rebuild: `cd bridge && go build -trimpath -o bin/relay ./cmd/relay && launchctl kickstart -k gui/$(id -u)/com.relay.bridge`.
  - Rebuild ldflags: add `-X relay/internal/api.Version=<v> -X relay/internal/bootstrap.BuildSourceDir=$PWD` (see `bridge/Makefile`; don't `make build`, it writes `bin/relay` too).
  - Pairing QR: `bridge/bin/relay pair` (`--url` overrides the address).
- **EC2 test box** (the user's, eu-north-1, Ubuntu 26.04 x86_64, `relay-server-0`):
  - SSH: alias `relay_ssh` in `~/.zshrc` (key `~/.ssh/relay-server-key.pem`, user `ubuntu`). Laptop config: `~/.relay/config.toml`, so `relay ssh` / `relay deploy` / `relay doctor` work from the Mac.
  - Set up with both paths: `relay pair` (Path 1), then `relay init` from `~/projects/agents-cli` (Path 2). Runs `relay` v0.1.0 (installed with `install.sh`), `relay.service` + `herdr-server.service` (system units), Tailscale on the user's tailnet.
  - herdr workspace `w1` = `~/work/agents-cli/main`. Claude signed in; codex and pi not signed in (the app greys them out).
  - The user also attaches to it from the `box` tab. Never type into that pane; use your own pane (`lead-box` tab).
- **Phone:**
  - The user's iPhone 17 (Developer Mode on; the UDID is in `xcrun devicectl list devices`) has the round-11 build from 2026-10-01. It expires about **2026-10-08**.
  - Reinstall (plugged in): `TEAM=TC56945264 scripts/install-device.sh`. This is the free Personal Team, so builds last 7 days and there are no App Groups (`Relay-FreeTeam.entitlements`).
  - If Xcode says "No Accounts", the user must sign in again under Xcode → Settings → Accounts.
- **Checks:**
  - Bridge: `cd bridge && gofmt -l . && go vet ./... && GOOS=linux go vet ./... && go test -race ./...`
  - Linux binaries: `scripts/build-linux.sh`.
  - CI (`.github/workflows/ci.yml`) runs the bridge checks on macOS and Ubuntu for pushes to `main`. `release.yml` runs goreleaser on `v*` tags; tagging is the user's call (auto mode blocks it).
  - iOS unit tests (212): `xcodebuild test -scheme Relay -destination 'platform=iOS Simulator,name=iPhone 17' -only-testing:RelayTests`.
  - UI tests: `-only-testing:RelayUITests`, on the iPhone 17 Pro simulator. The live suites need `TEST_RUNNER_RELAY_*` env vars (see each test file).
  - A second "Test VM" bridge for multi-machine tests: `scripts/second-bridge.sh` (port 7881).
  - Diff two bridges: `bridge/parity/run.sh`.
- **Test agents:** herdr workspace `herd-e2e`, cwd `~/.relay/e2e`.
  - `w14:p2` claude, `w14:p4` pi, `w14:p5` codex.
  - Never touch the user's other agents.

## What's built (rounds 1–11, all on main and verified)
- Chats from transcripts, with:
  - a live "Thinking…" shimmer and live replies;
  - approvals, including plan-mode plans shown in full;
  - model / effort / mode controls;
  - attachments, through an attachment sheet with big tiles;
  - a file viewer: tap a Read/Write/Edit row for the file or a diff;
  - a usage screen;
  - the "B · Grouped" sidebar.
- **Round 8:** the bridge was ported from Swift to Go. The cutover finished on 2026-10-01; Swift is only in git history.
- **Round 9:** multiple machines, P1.
  - The app pairs with one bridge per machine.
  - The machine menu has "All" plus each machine.
  - A Machines screen handles add, rename, re-pair and remove.
  - Everything is keyed `<machineId>/<id>`.
  - Remote setup is manual (`docs/remote-setup.md`, systemd user unit).
- **Round 10:** the user's own bug list (B1–B9) plus a crash on socket death, all fixed and verified.
- New app icon: a geometric R, with light, dark and tinted variants.
- **Round 11 (`docs/tasks/round-11/`):** agents-cli merged in as the laptop CLI, with its history. Slack and the per-session worktrees are gone.
  - **Path 1:** `curl -fsSL https://raw.githubusercontent.com/Achno2k/relay/main/scripts/install.sh | sh`, then `relay pair` on a Linux VM. That installs herdr + `herdr-server.service`, the herdr integrations for installed CLIs, Tailscale (login QR or `--authkey`) and `relay.service`, then prints the pairing QR. Running it again changes nothing.
  - **Path 2:** `relay init` from the laptop: the AWS pickers, bootstrap (claude, codex, pi), logins, GitHub, clone + trust, env plan, `relay pair --yes` over SSH, and a herdr workspace per repo.
  - `GET /kinds` + `409 not_signed_in`/`not_installed`: the app greys out agent kinds that aren't signed in on that machine.
  - Claude Code now defaults to auto mode on its own. Codex auto settings exist (`bs_auto_codex`) but aren't wired in (the auto-mode classifier blocked it; the user said leave it).
  - History was rewritten before going public (real paths, tailnet IPs, device id and work email replaced; author is `Achno2k`). The old agents-cli GitHub repo is deleted.

## Open items
1. **The user is testing the round-11 build** on the phone against the Mac and the EC2 box. Wait for feedback.
2. **Follow-ups from the box test:**
   - The box binary is 51 MB (mostly the AWS SDK), so uploads are slow. Build the Linux binary without the AWS code (build tags).
   - `relay deploy`'s upload has no timeout or progress. One upload stalled for 19 minutes.
   - `/agents/:id/controls` reports `permissionMode: null` while Claude's footer says auto mode is on. Not investigated.
3. **Later phases:**
   - P2: version warnings in the app when a bridge is older than the app expects. `relay deploy` covers updates from the laptop.
   - P3: pair-once discovery, and usage merged across machines.
4. **Parked decisions:**
   - The pi/Codex default models. The user's ChatGPT is on the Free plan, and `~/.pi/agent/settings.json` / `~/.codex/config.toml` default to models it can't use.
   - A paid Apple account (TestFlight, push, Live Activities).
5. `docs/relay-system-design.html` is untracked and belongs to the user. Leave it alone.
6. Local branch `r10/qa-int` still holds the pre-rewrite history. Never push it. The user may delete it.

## How the user wants you to work
- Follow `~/.claude/CLAUDE.md`:
  - load `herdr --skill` and `unslop`;
  - be very concise, in points;
  - the context limit is 400k: offer a handoff when you reach it.
- **Workers:**
  - Delegate multi-step work to herdr worker sessions, using the `herdr-orchestra` skill: one worktree per worker, status files, a background watcher.
  - **Opus only:** `herdr agent start <name> --kind claude --pane <id> -- --model opus`.
  - **More than 3 workers go in their own tab**, never split into the lead's tab.
  - Verify every worker claim yourself (tests on the merged branch), merge onto master, then remove the worktrees and close the tab.
- **Simulator:**
  - **UI tests run headless.** The Simulator app stays closed, because it steals the user's keyboard focus.
  - **Never use OS-level input** (cliclick, osascript keystrokes). Drive the app only through XCUITest, `simctl`, or the REST API.
- **The iPhone:** ask the user before any worker uses it.
- **Git and data:**
  - Never `git commit -a` / `-A`.
  - Fixtures are synthetic: never commit real transcripts, chat titles or paths.
  - Full paths never cross the wire.
- **Blocked actions:** auto mode blocks destructive or "deploy" actions such as deleting dirs or `launchctl` reloads, unless the user has just asked for them explicitly. When blocked, give the user the exact commands.
- **The repo is public now.** Before every push, grep the diff for real paths (`/Users/<name>`), tailnet IPs, device ids, hostnames and work emails. Push to `origin main` only; tags and history rewrites are the user's call.
- **Agent sessions:** none running. All round 8–11 workers are closed, their worktrees removed and branches deleted.

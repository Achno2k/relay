# Handoff: Relay (native iOS app for driving herdr agents)

## What this is
- **Relay** is the user's personal iOS app for driving the coding agents (Claude Code, Codex, pi) in their **herdr** terminal session from the phone. Repo: `~/projects/relay`, branch `master`, no remote.
- `bridge/`: the Go daemon `relay`. Module `relay`, `cmd/relay`, packages in `internal/`. It talks to herdr's unix socket, tails transcripts, and serves REST + WS. Runs on macOS and Linux.
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

## Running system (2026-10-01)
- **Bridge:**
  - LaunchAgent `com.relay.bridge` runs `bridge/bin/relay serve --port 7878 --require-tailscale` (the binary is gitignored).
  - It binds the Tailscale IP `100.101.102.103` and localhost.
  - Data lives in `~/.relay` (token, uploads, `relay.log`).
  - Rebuild: `cd bridge && go build -trimpath -o bin/relay ./cmd/relay && launchctl kickstart -k gui/$(id -u)/com.relay.bridge`.
  - Pairing QR: `bridge/bin/relay pair` (`--url` overrides the address).
- **Phone:**
  - The user's iPhone 17 (`00008150-0000000000000000`, Developer Mode on) has the round-10 build from 2026-10-01. It expires about **2026-10-08**.
  - Reinstall (plugged in): `TEAM=TC56945264 scripts/install-device.sh`. This is the free Personal Team, so builds last 7 days and there are no App Groups (`Relay-FreeTeam.entitlements`).
  - If Xcode says "No Accounts", the user must sign in again under Xcode → Settings → Accounts.
- **Checks:**
  - Bridge: `cd bridge && gofmt -l . && go vet ./... && GOOS=linux go vet ./... && go test -race ./...`
  - Linux binaries: `scripts/build-linux.sh`.
  - iOS unit tests (204): `xcodebuild test -scheme Relay -destination 'platform=iOS Simulator,name=iPhone 17' -only-testing:RelayTests`.
  - UI tests: `-only-testing:RelayUITests`, on the iPhone 17 Pro simulator. The live suites need `TEST_RUNNER_RELAY_*` env vars (see each test file).
  - A second "Test VM" bridge for multi-machine tests: `scripts/second-bridge.sh` (port 7881).
  - Diff two bridges: `bridge/parity/run.sh`.
- **Test agents:** herdr workspace `herd-e2e`, cwd `~/.relay/e2e`.
  - `w14:p2` claude, `w14:p4` pi, `w14:p5` codex.
  - Never touch the user's other agents.

## What's built (rounds 1–10, all on master and verified)
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

## Open items
1. **The user is testing the round-10 build on the phone.** Wait for feedback.
   - B4 (Photos hitbox) was never reproduced in the simulator; it's fixed by the new attachment sheet.
2. **Cloud VM (multi-machine P1, real test):** not done.
   - Needs from the user: the VM's OS and CPU, and Tailscale on it.
   - Then follow `docs/remote-setup.md`, or do it over SSH if the user gives access.
3. **Later phases:**
   - P2: `relay remote add/update <ssh-host>` run from the Mac, plus version warnings.
   - P3: pair-once discovery, and usage merged across machines.
4. **Parked decisions:**
   - The pi/Codex default models. The user's ChatGPT is on the Free plan, and `~/.pi/agent/settings.json` / `~/.codex/config.toml` default to models it can't use.
   - A paid Apple account (TestFlight, push, Live Activities).
   - Open source vs personal. The research says don't sell it; open-source it as "the native iOS herdr client" if anything.
5. `docs/relay-system-design.html` is untracked and belongs to the user. Leave it alone.

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
- **Agent sessions:** none running. All round 8–10 workers are closed and their worktrees removed.

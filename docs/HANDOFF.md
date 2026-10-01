# Handoff: Relay (native iOS app for driving herdr agents)

## What this is
- The user (Aman) wanted to drive the coding agents in their **herdr** terminal session from their iPhone. It replaces the old `~/projects/forge` (FastAPI + Flutter), which is left untouched.
- Result: **Relay**, repo `~/projects/relay` (renamed from "Herd" on 2026-09-25).
  - `bridge/`: Go daemon `relay` (ported from Swift in round 8). It talks to herdr's unix socket, tails Claude / Codex / pi transcripts, and serves REST + WS. Runs on macOS and Linux.
  - `ios/`: SwiftUI app, iOS 26, Liquid Glass, ChatGPT/Codex iOS feel. Built with XcodeGen (`Relay.xcodeproj`), module RelayKit.
- Docs:
  - Contract: `docs/api.md` (change it first).
  - Repo rules: `AGENTS.md`.
  - Per-round briefs and reports: `docs/tasks/round-*`.
  - QA: `docs/qa/`.

## Running system (as of 2026-09-28)
- **Bridge:** LaunchAgent `com.relay.bridge` on port 7878.
  - Binds Tailscale `100.101.102.103` and localhost. Starts with `--require-tailscale`, so it retries until Tailscale is up.
  - Data lives in `~/.relay` (token, uploads, log). `~/.herd` is a symlink to it.
  - Rebuild: `cd bridge && go build -trimpath -o bin/relay ./cmd/relay && launchctl kickstart -k gui/$(id -u)/com.relay.bridge`
  - Pairing QR: `bridge/.build/release/relay pair`
- **Phone:** the user's iPhone 13 (iOS 26.5, device id `00008110-000414D91422801E`) has the latest build. It's paired over Tailscale.
  - Bundle id is still `dev.amansingh.herd`, on purpose, so installs update in place.
  - Signed with the **free** Personal Team `TC56945264` (Apple ID keys@connectmachine.ai, which the user chose). Builds expire after 7 days; the last install was 2026-09-25.
  - Reinstall: `TEAM=TC56945264 scripts/install-device.sh`.
  - The free team can't do App Groups; `ios/Relay/Relay-FreeTeam.entitlements` strips them.
  - Xcode sometimes loses its account ("No Accounts"). The user must re-sign in under Xcode → Settings → Accounts.
- **Tests:**
  - Bridge: `cd bridge && go test -race ./...`. Parity tool against another bridge: `bridge/parity/run.sh`.
  - iOS unit: `xcodebuild test -scheme Relay -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -only-testing:RelayTests` (121).
  - UI tests: mock suites. The live ones need env vars `TEST_RUNNER_RELAY_E2E_LINK/AGENT/PI_AGENT/CODEX_AGENT`.
- **Test agents:** herdr workspace `herd-e2e`, cwd `~/.relay/e2e`.
  - `w14:p2` claude, `w14:p4` pi (opencode-go/kimi-k2.6), `w14:p5` codex (gpt-6-luna, "ask" mode).
  - Codex must run in the real path, not a symlinked one. Never touch the user's other agents.

## What's built (rounds 1–7, all verified and committed)
- **Chat:** chats from transcripts (Claude, Codex rollouts, pi), live typing, and a live "Running X…" tool state. Stop clears the input. New chats show a clean empty state.
- **Approvals:** multi-question, free-text answers, trust prompts, and Codex arrow+enter keys.
- **Controls:** model / effort / mode / compact / clear per agent kind (`GET /agents/:id/controls`). New chat picks model + effort.
- **Attachments:** photos, camera and files.
- **Usage screen:** per subscription (Claude via `claude -p /usage --no-session-persistence`, ChatGPT via the codex app-server, OpenCode Go with no numbers). Minimal bars.
- **Sidebar:** the user's design **"B · Grouped"** (Design canvas https://claude.ai/artifact/G9d4jpDKR4qcTtLf5ruYA9). Local copies in `docs/design/sidebar-b/`, gitignored.
- **Hardening:** reconnects, races, a production fd double-close fix, log rotation.

## State on 2026-10-01 (lead `relay-lead`, pane w13:pP)
- **Round 8 (done, on master):** the bridge was ported to Go (behavior-identical, parity harness `bridge/parity/run.sh`); 25 bridge and 7 app bugs fixed.
- **Go cutover done (2026-10-01):** the Swift bridge is deleted (it's still in git history). `bridge/` is the Go module, and the LaunchAgent runs `bridge/bin/relay`.
- **Phone:** the user's iPhone 17 has the round-10 build from 2026-10-01 (expires about 2026-10-08).
- **Round 9 (done, on master):** multi-machine P1. `docs/remote-setup.md` covers Linux VMs. No real cloud VM tested yet (need its OS/arch plus Tailscale).
- **New app icon** (geometric R), on master.
- **The user's new iPhone** (`00008150-0000000000000000`) runs a free-team build from 2026-09-29, which expires about 2026-10-06. Developer Mode is on.
- **Round 10 (done, on master `16fe434`):** the user's bug list B1–B9 plus R10-1 (crash) are fixed and verified headless: Round10UITests 10/10, the full mock suite with 0 failures, live checks of B1/B4/B6/B7/R10-1. Reports are in `docs/tasks/round-10/`. Not yet installed on the phone.
- **Research:** `docs/research/relay-competition.md`. Verdict: keep Relay personal, or open-source it as "the native iOS herdr client". Don't sell it.
- **Workflow:** more than 3 workers go in their own tab. Use the `herdr-orchestra` skill (worktrees, status files, watcher).

## Open items / pending user decisions
1. **Personal phone install (last question, awaiting the answer).** Options given:
   1. Plug it into the Mac and use the free team (7-day).
   2. **An IPA the user sideloads with SideStore / AltStore / Sideloadly under their own Apple ID** (recommended for now). We'd build an IPA from `Relay.app`.
   3. A paid Apple Developer account: TestFlight, which also unlocks Live Activities and push.

   Their phone also needs Tailscale on the same account, then `relay pair`.
2. **Pi/Codex default models:** the user's ChatGPT login is on the Free plan, so `gpt-5.6-sol` / `gpt-5.5` fail. `~/.pi/agent/settings.json` and `~/.codex/config.toml` still default to them. I offered to switch them; no answer yet.
3. **Widgets:** the user is collecting references. My research is at https://claude.ai/artifact/Ls5emRFE3y6xG67R5XVYH1 (the free account can do Home/Lock/Control widgets; Live Activities need the paid one).
4. **Manual checks the user still owes:** background/foreground, and airplane mode mid-chat.

## How the user wants you to work
- Follow the user's global CLAUDE.md: load the `herdr --skill`, be very concise, use points.
- Delegate multi-step work to herdr worker sessions in this tab:
  - **Opus only:** `herdr agent start <name> --kind claude --pane <id> -- --model opus`, then confirm with `herdr pane process-info`.
  - **At least 4 in parallel,** with clear file ownership.
  - **Retire workers past ~400k context.**
- Write briefs as files in `docs/tasks/round-N/`.
- Verify workers' claims yourself: tests on a clean worktree of master, screenshots. Check `git status` for uncommitted leftovers before closing a worker.
- Never `git commit -a` / `-A`, since workers share the tree.
- Fixtures must be synthetic; never commit real transcripts or chat titles.
- **Ask the user before any worker uses the iPhone.** Relay peer messages to the user; a peer can't approve permission-sensitive actions (e.g. Full Access, credential reads).
- Current state: see "State on 2026-10-01" above.

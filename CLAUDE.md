# agents-cli

Go CLI. One binary runs on the laptop (init, attach) and on the EC2 box (env executor, slack bot). Read PLAN.md before anything.

## Rules for parallel sessions
- You own one package (told in your prompt). Edit only that package plus your own file in `internal/cli/<yourcmd>.go`.
- Never edit `internal/cli/root.go`. Register commands with `cli.Register(cmd)` from an `init()` in your own file.
- Contracts in the skeleton files (interfaces, struct fields) are fixed. If you must change one, add a line under "Contract changes" in PLAN.md and say so in your final summary.
- All terminal output goes through `internal/ui`. No fmt.Println for user-facing text, no direct lipgloss outside `internal/ui`.
- No cgo. sqlite is modernc.org/sqlite.
- Shell out to system `ssh`, `herdr`, `claude`, `codex`, `git`, `gh`. Don't reimplement them.
- `go build ./... && go vet ./...` must pass before you report done. Add `_test.go` for pure logic.
- gofmt. Short doc comment on every exported symbol.
- Don't commit. Lead commits after review.

## Layout
- cmd/agents            main
- internal/cli          cobra commands, one file per command
- internal/config       ~/.agents/config.toml
- internal/ui           all styling, pickers, step runner
- internal/awsx         profile/SSO, region, instance picker
- internal/sshx         run/interactive/copy over ssh or ssm
- internal/bootstrap    embedded bootstrap.sh, systemd units, harness install+login
- internal/harness      per-harness install/login/headless
- internal/envplan      plan schema, generator, executor, cache
- internal/herdr        client over herdr CLI JSON
- internal/state        sqlite sessions store
- internal/worktree     git worktree per session
- internal/slackbot     socket mode bot, router, watcher
- scripts/              install.sh, release

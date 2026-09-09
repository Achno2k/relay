package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/you/agents-cli/internal/ui"
)

func init() {
	var plain, noPrompts bool

	cmd := &cobra.Command{
		Use:    "ui-demo",
		Short:  "Exercise every internal/ui renderer so it can be eyeballed",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if plain {
				os.Setenv("AGENTS_UI_PLAIN", "1")
			}
			return runUIDemo(cmd.Context(), noPrompts)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "force the non-tty renderer (no animation, numbered prompts)")
	cmd.Flags().BoolVar(&noPrompts, "no-prompts", false, "skip the pickers so the demo never blocks on stdin")

	Register(cmd)
}

func runUIDemo(ctx context.Context, noPrompts bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ui.Title("Text helpers")
	ui.Info("Info: plain line, monochrome base.")
	ui.Success("Success: connected to i-0abc123 in eu-west-1")
	ui.Warn("Warn: two harnesses selected, login is one prompt each")
	ui.Fail("Fail: goes to stderr, not stdout")
	ui.Muted("Muted: dim, for things the user rarely needs")

	ui.Title("Key / value")
	ui.KV(
		"profile", "work-dev",
		"region", "eu-west-1",
		"instance", "i-0abc123def456789 (t4g.xlarge)",
		"repo", "you/agents-cli",
	)

	ui.Title("Copyable command")
	ui.Code("agents attach agents-cli-3")

	err := ui.RunSteps(ctx, "Bootstrapping the box...", demoSteps())
	if err != nil {
		ui.Fail("bootstrap failed: " + err.Error())
	}

	failed, err := ui.RunChecks(ctx, "Running verification...", demoChecks())
	if err != nil {
		return err
	}
	if failed == 0 {
		ui.Info("Environment ready.")
	} else {
		ui.Warn(fmt.Sprintf("%d check(s) failed.", failed))
	}

	ui.Title("Spinner")
	if err := ui.Spinner(ctx, "Fetching instance list", func(ctx context.Context) error {
		return sleepCtx(ctx, 1600*time.Millisecond)
	}); err != nil {
		return err
	}
	if err := ui.Spinner(ctx, "Reaching the herdr socket", func(ctx context.Context) error {
		if err := sleepCtx(ctx, 1100*time.Millisecond); err != nil {
			return err
		}
		return errors.New("dial unix /run/herdr.sock: no such file")
	}); err != nil {
		ui.Muted("  " + err.Error())
	}

	if noPrompts {
		ui.Title("Pickers")
		ui.Muted("skipped (--no-prompts)")
		return nil
	}
	return demoPickers()
}

func demoPickers() error {
	ui.Title("Pickers")

	regions := []string{"us-east-1", "us-west-2", "eu-west-1", "ap-south-1"}
	i, err := ui.Select("Region", regions)
	if err != nil {
		return quietCancel(err)
	}
	ui.Success("region " + regions[i])

	harnesses := []string{"claude", "codex", "gemini", "amp"}
	picked, err := ui.MultiSelect("Harnesses to install", harnesses, []int{0, 1})
	if err != nil {
		return quietCancel(err)
	}
	names := make([]string, 0, len(picked))
	for _, p := range picked {
		names = append(names, harnesses[p])
	}
	ui.Success(fmt.Sprintf("harnesses %v", names))

	repo, err := ui.Input("Repo to clone", "you/agents-cli")
	if err != nil {
		return quietCancel(err)
	}
	if repo == "" {
		repo = "you/agents-cli"
	}
	ui.Success("repo " + repo)

	ok, err := ui.Confirm("Enable the Slack bot on this box?", true)
	if err != nil {
		return quietCancel(err)
	}
	if ok {
		ui.Success("slack bot enabled")
	} else {
		ui.Muted("slack bot left off")
	}

	ui.Title("Summary")
	ui.KV("ssh", "agents ssh", "attach", "agents attach <session>", "bot", "systemctl status agents-bot")
	return nil
}

func demoSteps() []ui.Step {
	fake := func(d time.Duration, lines ...string) func(context.Context, io.Writer) error {
		return func(ctx context.Context, log io.Writer) error {
			for _, l := range lines {
				fmt.Fprintln(log, l)
			}
			return sleepCtx(ctx, d)
		}
	}
	return []ui.Step{
		{Name: "Installing Git", Run: fake(600*time.Millisecond, "apt-get install -y git", "git version 2.43.0")},
		{Name: "Installing Go 1.25", Run: fake(900*time.Millisecond, "mise use -g go@1.25")},
		{Name: "Installing herdr", Run: fake(700 * time.Millisecond)},
		{Name: "Installing claude", Run: fake(800 * time.Millisecond)},
		{Name: "Installing codex", Run: func(ctx context.Context, log io.Writer) error {
			for i := 1; i <= 30; i++ {
				fmt.Fprintf(log, "npm http fetch GET 200 https://registry.npmjs.org/codex chunk %d\n", i)
			}
			fmt.Fprintln(log, "npm ERR! code EACCES")
			fmt.Fprintln(log, "npm ERR! syscall mkdir")
			fmt.Fprintln(log, "npm ERR! path /usr/lib/node_modules/codex")
			if err := sleepCtx(ctx, 900*time.Millisecond); err != nil {
				return err
			}
			return errors.New("npm install codex: exit status 243")
		}},
		{Name: "Writing systemd units", Run: fake(400 * time.Millisecond)},
		{Name: "Cloning the repo", Run: fake(400 * time.Millisecond)},
		{Name: "Starting herdr", Run: fake(400 * time.Millisecond)},
	}
}

func demoChecks() []ui.Check {
	pass := func(context.Context) error { return nil }
	return []ui.Check{
		{Name: "Go", Run: pass},
		{Name: "Node", Run: pass},
		{Name: "herdr", Run: pass},
		{Name: "codex", Run: func(context.Context) error { return errors.New("not on PATH") }},
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// quietCancel turns a picker abort into a clean exit.
func quietCancel(err error) error {
	if errors.Is(err, ui.ErrCancelled) {
		ui.Muted("cancelled")
		return nil
	}
	return err
}

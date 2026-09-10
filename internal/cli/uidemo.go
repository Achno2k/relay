package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/ui"
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
		"repo", "Achno2k/agents-cli",
	)

	ui.Title("Copyable command")
	ui.Code("agents attach agents-cli-3")

	if err := demoPhases(ctx); err != nil {
		ui.Fail("phase demo: " + err.Error())
	}

	ui.Title("Step runner")
	ui.Muted("each step streams its last 3 log lines, then collapses them")
	ui.Muted("step 7 hangs on purpose, so ui.StepTimeout cuts it off at " + demoStepTimeout.String())

	// Restore the caller's setting: this is a package level knob.
	prevTimeout := ui.StepTimeout
	ui.StepTimeout = demoStepTimeout
	defer func() { ui.StepTimeout = prevTimeout }()

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
		return sleepCtx(ctx, demoSpinnerHold)
	}); err != nil {
		return err
	}
	if err := ui.Spinner(ctx, "Reaching the herdr socket", func(ctx context.Context) error {
		if err := sleepCtx(ctx, demoSpinnerHold); err != nil {
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

	repo, err := ui.Input("Repo to clone", "Achno2k/agents-cli")
	if err != nil {
		return quietCancel(err)
	}
	if repo == "" {
		repo = "Achno2k/agents-cli"
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

// demoPhases shows a phase inside a phase, with a checklist at the bottom.
// The point is that every loader above the checklist keeps turning while the
// checklist runs, which is only possible because one renderer owns the tree.
func demoPhases(ctx context.Context) error {
	ui.Title("Nested phases")
	ui.Muted("both loaders keep turning while the checklist below them runs")

	return ui.Phase(ctx, "Setting up dev environment", func(ctx context.Context) error {
		ui.Muted("reading go.mod, package.json, Makefile")
		if err := sleepCtx(ctx, demoSpinnerHold); err != nil {
			return err
		}

		if err := ui.Phase(ctx, "Inferring steps", func(ctx context.Context) error {
			return ui.RunSteps(ctx, "", []ui.Step{
				{Name: "Installing Go", Run: streamer(
					"mise use -g go@1.25",
					"go version go1.25.0 linux/arm64",
				)},
				{Name: "Installing Python", Run: streamer(
					"mise use -g python@3.13",
					"Python 3.13.1",
				)},
				{Name: "go mod download", Run: streamer(
					"downloading github.com/spf13/cobra v1.10.2",
					"downloading github.com/slack-go/slack v0.17.3",
				)},
			})
		}); err != nil {
			return err
		}

		return ui.Phase(ctx, "Verifying", func(ctx context.Context) error {
			failed, err := ui.RunChecks(ctx, "", []ui.Check{
				{Name: "go", Run: func(context.Context) error { return nil }},
				{Name: "python", Run: func(context.Context) error { return nil }},
			})
			if err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return sleepCtx(ctx, time.Second)
		})
	})
}

// demoStepHold is how long every fake step holds the loader. Long enough that
// the animation and the live tail are unmistakable rather than a flicker.
const demoStepHold = 2500 * time.Millisecond

// demoStepTimeout is what the demo sets ui.StepTimeout to, so the timeout path
// is visible without making the run drag.
const demoStepTimeout = 10 * time.Second

// demoSpinnerHold keeps each spinner up for two seconds.
const demoSpinnerHold = 2 * time.Second

// streamer returns a step that holds the loader for demoStepHold, writing its
// lines spread evenly across that window so the live tail keeps moving.
func streamer(lines ...string) func(context.Context, io.Writer) error {
	return func(ctx context.Context, log io.Writer) error {
		gap := demoStepHold / time.Duration(len(lines)+1)
		for _, l := range lines {
			if err := sleepCtx(ctx, gap); err != nil {
				return err
			}
			fmt.Fprintln(log, l)
		}
		return sleepCtx(ctx, gap)
	}
}

func demoSteps() []ui.Step {
	return []ui.Step{
		{Name: "Installing Git", Run: streamer(
			"apt-get install -y git",
			"Reading package lists...",
			"Setting up git (1:2.43.0-1ubuntu7)",
			"git version 2.43.0",
		)},
		{Name: "Installing Go 1.25", Run: streamer(
			"mise use -g go@1.25",
			"downloading go1.25.0.linux-arm64.tar.gz",
			"verifying checksum",
			"extracting to ~/.local/share/mise/installs/go/1.25.0",
			"go version go1.25.0 linux/arm64",
		)},
		{Name: "Installing herdr", Run: streamer(
			"curl -fsSL https://herdr.dev/install.sh | sh",
			"installing to /usr/local/bin/herdr",
			"herdr 0.9.2",
		)},
		{Name: "Installing claude", Run: streamer(
			"npm install -g @anthropic-ai/claude-code",
			"added 1 package in 3s",
			"claude 2.1.260",
		)},
		{Name: "Installing codex", Run: streamer(
			"npm install -g @openai/codex",
			"added 1 package in 4s",
			"codex 0.149.1",
		)},
		{Name: "Writing systemd units", Run: streamer(
			"agents-bot.service",
			"herdr.service",
			"systemctl --user daemon-reload",
		)},
		// Deliberately outlasts ui.StepTimeout, so the demo shows what a step
		// that never finishes looks like. It reports progress with carriage
		// returns, the way git actually does, which the live tail collapses to
		// a single updating line.
		{Name: "Cloning the repo", Run: func(ctx context.Context, log io.Writer) error {
			fmt.Fprintln(log, "git clone --filter=blob:none git@github.com:Achno2k/agents-cli")
			for pct := 0; ; pct = (pct + 3) % 100 {
				if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
					return err
				}
				fmt.Fprintf(log, "Receiving objects: %2d%% (of 41243), 12.40 MiB | 2.1 MiB/s\r", pct)
			}
		}},
		{Name: "Starting herdr", Run: streamer("systemctl --user start herdr")},
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

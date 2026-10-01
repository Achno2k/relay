package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"relay/internal/ui"
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
				os.Setenv("RELAY_UI_PLAIN", "1")
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

	ui.Banner("v0.1.0-demo")

	ui.Title("Text helpers")
	ui.Muted("purple is the accent; green, red and orange carry status")
	ui.Info("Info: plain line, monochrome base.")
	ui.Success("Success: connected to i-0abc123 in eu-west-1")
	ui.Warn("Warn: two harnesses selected, login is one prompt each")
	ui.Fail("Fail: goes to stderr, not stdout")
	ui.Muted("Muted: dim, for things the user rarely needs")

	ui.Title("Key / value")
	ui.KV(
		"profile", "dev",
		"region", "eu-west-1",
		"instance", "i-0abc123def456789 (t4g.xlarge)",
		"repo", "example/webapp",
	)

	ui.Title("Copyable command")
	ui.Code("relay attach")

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
	ui.Muted("● is chosen, ○ is not; the accent circle is the cursor")
	ui.Muted("long lists scroll one row at a time; / filters a single choice list")
	ui.Muted("every answer then collapses to one line")

	// Longer than the ten row cap on purpose: the list should hold still while
	// the circle moves, and only shift once the cursor reaches an edge.
	regions := []string{
		"us-east-1", "us-east-2", "us-west-1", "us-west-2",
		"eu-west-1", "eu-west-2", "eu-central-1", "eu-north-1",
		"ap-south-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
		"sa-east-1", "ca-central-1",
	}
	// SelectOrOther: the list carries an "other…" row that opens a text field,
	// and backing out of that field returns to the list.
	if _, err := ui.SelectOrOther("Region", regions, "me-central-1"); err != nil {
		return quietCancel(err)
	}

	// Marked options are how `relay init` shows what the box already has.
	harnesses := []string{"claude", "codex", "gemini", "amp"}
	marks := []string{"installed", "", "installed", ""}
	if _, err := ui.MultiSelectMarked("Harnesses to install", harnesses, marks, []int{0, 2}); err != nil {
		return quietCancel(err)
	}

	if _, err := ui.Input("Repo to clone", "example/webapp"); err != nil {
		return quietCancel(err)
	}

	// The transcript shows the length and nothing else.
	if _, err := ui.Secret("GitHub token"); err != nil {
		return quietCancel(err)
	}

	if _, err := ui.Confirm("Clone it on the box?", true); err != nil {
		return quietCancel(err)
	}

	demoReady()
	return nil
}

// demoReady is the block that ends a real `relay init`.
func demoReady() {
	ui.Ready("Machine ready",
		"Box", "ubuntu@box.example.com (i-0abc123def456789)",
		"Region", "eu-west-1",
		"Repo", "~/work/webapp/main",
		"Harnesses", "claude, codex",
	)
	ui.Commands(
		"relay attach",
		"relay ssh",
	)
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
					"downloading github.com/BurntSushi/toml v1.6.0",
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
			"herdr-server.service",
			"systemctl daemon-reload",
		)},
		// Deliberately outlasts ui.StepTimeout, so the demo shows what a step
		// that never finishes looks like. It reports progress with carriage
		// returns, the way git actually does, which the live tail collapses to
		// a single updating line.
		{Name: "Cloning the repo", Run: func(ctx context.Context, log io.Writer) error {
			fmt.Fprintln(log, "git clone --filter=blob:none git@github.com:example/webapp")
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

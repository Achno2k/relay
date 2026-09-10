// Package bootstrap brings a fresh Ubuntu 24.04 box up to spec: base tools,
// mise + node, herdr, the coding harnesses, the agents binary, and the systemd
// units that keep the herdr server and the Slack bot alive.
//
// scripts/bootstrap.sh holds one idempotent bash function per install. Each one
// is run separately so it renders as its own ui.Step.
package bootstrap

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
)

//go:embed scripts/bootstrap.sh
var bootstrapSH string

// Task is one bootstrap.sh function, shown to the user as a single step.
type Task struct {
	Name string // "Installing GitHub CLI"
	Func string // "bs_gh"
}

// BaseTasks are the installs every box gets, in dependency order.
func BaseTasks() []Task {
	return []Task{
		{"Checking passwordless sudo", "bs_sudo_check"},
		{"Installing base packages", "bs_apt_basics"},
		{"Installing curl", "bs_curl"},
		{"Installing unzip", "bs_unzip"},
		{"Installing git", "bs_git"},
		{"Installing GitHub CLI", "bs_gh"},
		{"Installing mise + node 22", "bs_mise"},
		{"Installing herdr", "bs_herdr"},
	}
}

// HarnessTasks returns the install task for each named harness, skipping names
// with no bootstrap function.
func HarnessTasks(names []string) []Task {
	byName := map[string]Task{
		"claude": {"Installing Claude Code", "bs_claude"},
		"codex":  {"Installing Codex", "bs_codex"},
	}
	var out []Task
	for _, n := range names {
		if t, ok := byName[strings.ToLower(n)]; ok {
			out = append(out, t)
		}
	}
	return out
}

// ProfileTask writes ~/.agents and the AGENTS_ON_BOX marker. It runs last so a
// half-finished box is never marked ready.
func ProfileTask() Task { return Task{"Writing ~/.agents and shell profile", "bs_agents_home"} }

// Script returns bootstrap.sh with a call to fn appended. sshx.Runner feeds a
// script on stdin and has no argv, so the call travels with the source.
func Script(fn string) string {
	return bootstrapSH + "\n" + fn + "\n"
}

// Steps turns tasks into ui.Steps that run over r.
func Steps(r sshx.Runner, tasks []Task) []ui.Step {
	steps := make([]ui.Step, 0, len(tasks))
	for _, t := range tasks {
		fn := t.Func
		steps = append(steps, ui.Step{
			Name: t.Name,
			Run: func(ctx context.Context, log io.Writer) error {
				return r.Run(ctx, Script(fn), log, log)
			},
		})
	}
	return steps
}

// Options controls what Install puts on the box.
type Options struct {
	Harnesses []string // "claude", "codex"
	GoArch    string   // amd64 | arm64, for the cross-compiled agents binary
	User      string   // box login user, "ubuntu"
	Home      string   // that user's home, defaults to /home/<User>
	WithBot   bool     // also install the agents-bot unit (tokens come later)
}

// Install runs the whole bootstrap as one numbered checklist: bash functions,
// then the cross-compiled agents binary, then the systemd units.
func Install(ctx context.Context, r sshx.Runner, o Options) error {
	o = o.withDefaults()

	tasks := append(BaseTasks(), HarnessTasks(o.Harnesses)...)
	tasks = append(tasks, ProfileTask())
	steps := Steps(r, tasks)

	steps = append(steps, ui.Step{
		Name: "Building and uploading agents binary",
		Run: func(ctx context.Context, log io.Writer) error {
			path, err := BuildForBox(ctx, o.GoArch)
			if err != nil {
				return err
			}
			fmt.Fprintf(log, "built %s\n", path)
			return InstallBinary(ctx, r, path)
		},
	})

	steps = append(steps, ui.Step{
		Name: "Installing systemd units",
		Run: func(ctx context.Context, log io.Writer) error {
			if err := InstallUnits(ctx, r, o.User, o.Home, log); err != nil {
				return err
			}
			return EnableHerdrServer(ctx, r, log)
		},
	})

	return ui.RunSteps(ctx, "Setting up the box", steps)
}

func (o Options) withDefaults() Options {
	if o.User == "" {
		o.User = "ubuntu"
	}
	if o.Home == "" {
		o.Home = "/home/" + o.User
	}
	if o.GoArch == "" {
		o.GoArch = "amd64"
	}
	return o
}

//go:embed scripts/reset.sh
var resetScript string

// ResetScript is the bash that undoes everything bootstrap installed.
func ResetScript() string { return resetScript }

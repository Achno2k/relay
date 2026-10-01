// Package bootstrap brings a fresh Ubuntu 24.04 box up to spec: base tools,
// mise + node, herdr, the coding harnesses and the relay binary. The systemd
// units are `relay pair`'s job, which init runs on the box afterwards.
//
// scripts/bootstrap.sh holds one idempotent bash function per install. Each one
// is run separately so it renders as its own ui.Step.
package bootstrap

import (
	"context"
	_ "embed"
	"io"
	"strings"

	"relay/internal/sshx"
	"relay/internal/ui"
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
		"pi":     {"Installing pi", "bs_pi"},
	}
	var out []Task
	for _, n := range names {
		if t, ok := byName[strings.ToLower(n)]; ok {
			out = append(out, t)
		}
	}
	return out
}

// WorkspaceTask creates the herdr workspace for a repo, with its checkout under
// workDir as the cwd.
func WorkspaceTask(workDir, repo string) Task {
	return Task{
		Name: "herdr workspace " + repo,
		Func: "bs_workspace " + shellQuote(repo) + " " + shellPath(CheckoutDir(workDir, repo)),
	}
}

// ProfileTask writes ~/.relay and the RELAY_ON_BOX marker. It runs last so a
// half-finished box is never marked ready.
func ProfileTask() Task { return Task{"Writing ~/.relay and shell profile", "bs_relay_home"} }

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
	Harnesses []string // "claude", "codex", "pi"
	GoArch    string   // amd64 | arm64, the box's relay binary
	User      string   // box login user, "ubuntu"
	Home      string   // that user's home, defaults to /home/<User>
}

// Install runs the whole bootstrap as one numbered checklist: bash functions,
// then the relay binary.
func Install(ctx context.Context, r sshx.Runner, o Options) error {
	o = o.withDefaults()

	tasks := append(BaseTasks(), HarnessTasks(o.Harnesses)...)
	tasks = append(tasks, ProfileTask())
	steps := Steps(r, tasks)

	steps = append(steps, ui.Step{
		Name: "Installing relay to /usr/local/bin/relay",
		Run: func(ctx context.Context, log io.Writer) error {
			path, err := BoxBinary(ctx, o.GoArch, log)
			if err != nil {
				return err
			}
			return InstallBinary(ctx, r, path)
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

// ResetPhases are the box-side reset phases, in the order they must run.
// Each maps to one function in reset.sh and one ui.Step.
func ResetPhases() []Task {
	return []Task{
		{"Stopping units", "rs_units"},
		{"Signing out of the harnesses and GitHub", "rs_signout"},
		{"Removing binaries", "rs_binaries"},
		{"Removing runtimes and state", "rs_runtimes"},
		{"Cleaning shell profiles", "rs_profiles"},
	}
}

// ResetScript returns reset.sh with a call to one phase appended, the same way
// Script works for bootstrap.sh.
func ResetScript(phase string) string {
	return resetScript + "\n" + phase + "\n"
}

// ResetSteps turns the reset phases into ui.Steps that run over r. Every phase
// writes to the step's log writer, so nothing lands on the terminal directly
// and a failure shows its own tail.
func ResetSteps(r sshx.Runner) []ui.Step {
	phases := ResetPhases()
	steps := make([]ui.Step, 0, len(phases))
	for _, p := range phases {
		phase := p.Func
		steps = append(steps, ui.Step{
			Name: p.Name,
			Run: func(ctx context.Context, log io.Writer) error {
				return r.Run(ctx, ResetScript(phase), log, log)
			},
		})
	}
	return steps
}

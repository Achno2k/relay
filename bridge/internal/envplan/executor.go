package envplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Achno2k/agents-cli/internal/ui"
)

// maxRepairs is how many times a failed step is handed back to the harness.
const maxRepairs = 3

// localExecutor replays a plan with bash on this machine. The ui and shell
// hooks are fields so the flow can be tested without a terminal.
type localExecutor struct {
	runSteps   func(ctx context.Context, title string, steps []ui.Step) error
	runChecks  func(ctx context.Context, title string, checks []ui.Check) (int, error)
	shell      func(ctx context.Context, script, dir string, out io.Writer) error
	fail       func(string)
	info       func(string)
	maxRepairs int
}

// NewLocalExecutor runs steps with os/exec on this machine (the box).
func NewLocalExecutor() Executor {
	return &localExecutor{
		runSteps:   ui.RunSteps,
		runChecks:  ui.RunChecks,
		shell:      bashRun,
		fail:       ui.Fail,
		info:       ui.Info,
		maxRepairs: maxRepairs,
	}
}

// bashRun executes a script with bash -c in dir, streaming both streams to out.
func bashRun(ctx context.Context, script, dir string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// Execute runs every step through the ui step runner, repairing failures with
// the generator up to maxRepairs times. Repaired steps are written back into
// p.Steps so the caller can cache them.
func (e *localExecutor) Execute(ctx context.Context, p Plan, repoDir string, gen Generator) error {
	steps := make([]ui.Step, 0, len(p.Steps))
	for i := range p.Steps {
		st := &p.Steps[i]
		steps = append(steps, ui.Step{
			Name: st.Name,
			Run: func(ctx context.Context, log io.Writer) error {
				return e.runStep(ctx, st, repoDir, gen, log)
			},
		})
	}
	title := "Setting up " + p.Repo
	if err := e.runSteps(ctx, title, steps); err != nil {
		e.fail("Environment setup failed: " + err.Error())
		return err
	}
	return nil
}

// runStep runs one step, then asks the generator for a replacement each time
// it fails, up to maxRepairs attempts.
func (e *localExecutor) runStep(ctx context.Context, st *Step, repoDir string, gen Generator, log io.Writer) error {
	var last error
	for attempt := 0; ; attempt++ {
		var captured bytes.Buffer
		out := io.MultiWriter(log, &captured)
		err := e.attempt(ctx, *st, repoDir, out)
		if err == nil {
			return nil
		}
		last = err
		if gen == nil || attempt >= e.maxRepairs || ctx.Err() != nil {
			break
		}
		fmt.Fprintf(log, "\nstep failed: %v\nasking the harness for a fix (%d/%d)\n", err, attempt+1, e.maxRepairs)
		fixed, rerr := gen.Repair(ctx, repoDir, *st, captured.String())
		if rerr != nil {
			fmt.Fprintf(log, "repair failed: %v\n", rerr)
			break
		}
		*st = fixed
	}
	return last
}

// attempt runs a step once: skip when its verify already passes, otherwise run
// it and check verify afterwards.
func (e *localExecutor) attempt(ctx context.Context, st Step, repoDir string, out io.Writer) error {
	if strings.TrimSpace(st.Verify) != "" {
		if err := e.shell(ctx, st.Verify, repoDir, out); err == nil {
			fmt.Fprintln(out, "already satisfied, skipping")
			return nil
		}
	}
	if err := e.shell(ctx, st.Run, repoDir, out); err != nil {
		return err
	}
	if strings.TrimSpace(st.Verify) == "" {
		return nil
	}
	if err := e.shell(ctx, st.Verify, repoDir, out); err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	return nil
}

// VerifyOnly runs the plan's verify list and reports whether the environment
// is ready.
func (e *localExecutor) VerifyOnly(ctx context.Context, p Plan, repoDir string) error {
	if len(p.Verify) == 0 {
		e.info("No verification checks in the plan.")
		return nil
	}
	checks := make([]ui.Check, 0, len(p.Verify))
	for _, c := range p.Verify {
		cmd := c.Cmd
		checks = append(checks, ui.Check{
			Name: c.Name,
			Run: func(ctx context.Context) error {
				return e.shell(ctx, cmd, repoDir, io.Discard)
			},
		})
	}
	failed, err := e.runChecks(ctx, "Running verification...", checks)
	if err != nil {
		return err
	}
	if failed > 0 {
		err := fmt.Errorf("%d of %d verification checks failed", failed, len(checks))
		e.fail(err.Error())
		return err
	}
	e.info("Environment ready.")
	return nil
}

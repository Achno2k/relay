package envplan

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Achno2k/agents-cli/internal/ui"
)

// fakeGen hands back a canned replacement step and counts repair calls.
type fakeGen struct {
	replacement Step
	plan        Plan
	err         error
	calls       int
	generates   int
	lastStderr  string
}

func (g *fakeGen) Generate(ctx context.Context, repoDir string) (Plan, error) {
	g.generates++
	if len(g.plan.Steps) > 0 {
		return g.plan, nil
	}
	return Plan{Repo: "api", Generated: "fake", Steps: []Step{g.replacement}}, nil
}

func (g *fakeGen) Repair(ctx context.Context, repoDir string, failed Step, stderr string) (Step, error) {
	g.calls++
	g.lastStderr = stderr
	if g.err != nil {
		return Step{}, g.err
	}
	return g.replacement, nil
}

// testExec builds a localExecutor whose ui hooks are recorded in memory and
// whose shell is the given fake.
type testExec struct {
	*localExecutor
	fails []string
	infos []string
	ran   []string
}

func newTestExec(shell func(script string) error) *testExec {
	te := &testExec{}
	te.localExecutor = &localExecutor{
		runSteps: func(ctx context.Context, title string, steps []ui.Step) error {
			for _, s := range steps {
				if err := s.Run(ctx, io.Discard); err != nil {
					return err
				}
			}
			return nil
		},
		runChecks: func(ctx context.Context, title string, checks []ui.Check) (int, error) {
			failed := 0
			for _, c := range checks {
				if err := c.Run(ctx); err != nil {
					failed++
				}
			}
			return failed, nil
		},
		shell: func(ctx context.Context, script, dir string, out io.Writer) error {
			te.ran = append(te.ran, script)
			err := shell(script)
			if err != nil {
				// Real bash writes its diagnostics to the step log; do the
				// same so the repair path sees a stderr tail.
				io.WriteString(out, err.Error()+"\n")
			}
			return err
		},
		fail:       func(s string) { te.fails = append(te.fails, s) },
		info:       func(s string) { te.infos = append(te.infos, s) },
		maxRepairs: maxRepairs,
	}
	return te
}

func TestExecuteRepairsFailedStepThenCompletes(t *testing.T) {
	installed := false
	te := newTestExec(func(script string) error {
		switch script {
		case "go version":
			if installed {
				return nil
			}
			return errors.New("go: command not found")
		case "bad-install":
			return errors.New("exit status 1")
		case "good-install":
			installed = true
			return nil
		}
		return errors.New("unexpected script: " + script)
	})

	gen := &fakeGen{replacement: Step{Name: "Install Go (fixed)", Run: "good-install", Verify: "go version"}}
	p := Plan{Repo: "api", Steps: []Step{{Name: "Install Go", Run: "bad-install", Verify: "go version"}}}

	if err := te.Execute(context.Background(), p, "/repo", gen); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gen.calls != 1 {
		t.Fatalf("repair calls = %d, want 1", gen.calls)
	}
	if !strings.Contains(gen.lastStderr, "exit status 1") {
		t.Fatalf("repair did not get the failed step's output: %q", gen.lastStderr)
	}
	if p.Steps[0].Run != "good-install" {
		t.Fatalf("repaired step not written back: %+v", p.Steps[0])
	}
	if len(te.fails) != 0 {
		t.Fatalf("ui.Fail called on a successful run: %v", te.fails)
	}
}

func TestExecuteStopsAfterThreeRepairs(t *testing.T) {
	te := newTestExec(func(script string) error { return errors.New("still broken") })
	gen := &fakeGen{replacement: Step{Name: "Install Go (try again)", Run: "also-bad", Verify: "go version"}}
	p := Plan{Repo: "api", Steps: []Step{{Name: "Install Go", Run: "bad-install", Verify: "go version"}}}

	err := te.Execute(context.Background(), p, "/repo", gen)
	if err == nil {
		t.Fatal("expected Execute to fail")
	}
	if gen.calls != maxRepairs {
		t.Fatalf("repair calls = %d, want %d", gen.calls, maxRepairs)
	}
	if len(te.fails) != 1 {
		t.Fatalf("ui.Fail calls = %d, want 1", len(te.fails))
	}
}

func TestExecuteStopsWhenRepairItselfFails(t *testing.T) {
	te := newTestExec(func(script string) error { return errors.New("broken") })
	gen := &fakeGen{err: errors.New("harness offline")}
	p := Plan{Repo: "api", Steps: []Step{{Name: "Install Go", Run: "bad-install"}}}

	if err := te.Execute(context.Background(), p, "/repo", gen); err == nil {
		t.Fatal("expected Execute to fail")
	}
	if gen.calls != 1 {
		t.Fatalf("repair calls = %d, want 1", gen.calls)
	}
}

func TestExecuteSkipsStepWhoseVerifyAlreadyPasses(t *testing.T) {
	te := newTestExec(func(script string) error {
		if script == "go version" {
			return nil
		}
		return errors.New("should not have run " + script)
	})
	p := Plan{Repo: "api", Steps: []Step{{Name: "Install Go", Run: "mise use -g go@1.25", Verify: "go version"}}}

	if err := te.Execute(context.Background(), p, "/repo", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(te.ran) != 1 || te.ran[0] != "go version" {
		t.Fatalf("ran %v, want only the verify command", te.ran)
	}
}

func TestExecuteReportsVerifyFailureWithoutGenerator(t *testing.T) {
	te := newTestExec(func(script string) error {
		if script == "go version" {
			return errors.New("not found")
		}
		return nil
	})
	p := Plan{Repo: "api", Steps: []Step{{Name: "Install Go", Run: "true", Verify: "go version"}}}

	err := te.Execute(context.Background(), p, "/repo", nil)
	if err == nil || !strings.Contains(err.Error(), "verify failed") {
		t.Fatalf("err = %v, want a verify failure", err)
	}
}

func TestVerifyOnly(t *testing.T) {
	te := newTestExec(func(script string) error { return nil })
	p := Plan{Repo: "api", Verify: []Check{{Name: "Go", Cmd: "go version"}}}
	if err := te.VerifyOnly(context.Background(), p, "/repo"); err != nil {
		t.Fatalf("VerifyOnly: %v", err)
	}
	if len(te.infos) == 0 || !strings.Contains(te.infos[len(te.infos)-1], "Environment ready") {
		t.Fatalf("infos = %v, want an Environment ready line", te.infos)
	}

	bad := newTestExec(func(script string) error { return errors.New("nope") })
	if err := bad.VerifyOnly(context.Background(), p, "/repo"); err == nil {
		t.Fatal("expected VerifyOnly to fail")
	}
	if len(bad.fails) != 1 {
		t.Fatalf("ui.Fail calls = %d, want 1", len(bad.fails))
	}
}

package envplan

import (
	"context"
	"errors"
	"os"
	"testing"
)

// stubExecutor fails on the first step it is given, and records whether the
// plan was already cached at the moment execution started.
type stubExecutor struct {
	executed   bool
	planOnDisk bool
	err        error
}

func (e *stubExecutor) Execute(ctx context.Context, p Plan, repoDir string, gen Generator) error {
	e.executed = true
	_, statErr := os.Stat(PlanPath(p.Repo))
	e.planOnDisk = statErr == nil
	return e.err
}

func (e *stubExecutor) VerifyOnly(ctx context.Context, p Plan, repoDir string) error { return nil }

// stubSetup swaps the Setup seams for the duration of a test.
func stubSetup(t *testing.T, gen Generator, ex Executor, approve bool) {
	t.Helper()
	oldGen, oldExec, oldSpin, oldConfirm := newGenerator, newExecutor, spinnerFn, confirmFn
	t.Cleanup(func() {
		newGenerator, newExecutor, spinnerFn, confirmFn = oldGen, oldExec, oldSpin, oldConfirm
	})
	newGenerator = func(string) (Generator, error) { return gen, nil }
	newExecutor = func() Executor { return ex }
	spinnerFn = func(ctx context.Context, label string, fn func(ctx context.Context) error) error {
		return fn(ctx)
	}
	confirmFn = func(string, bool) (bool, error) { return approve, nil }
}

func TestSetupCachesPlanBeforeExecuting(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())

	generated := Plan{
		Repo:      "ignored",
		Generated: "fake",
		Steps: []Step{
			{Name: "Install Go", Run: "bad-install", Verify: "go version"},
			{Name: "Deps", Run: "go mod download"},
		},
		Verify: []Check{{Name: "Go", Cmd: "go version"}},
	}
	gen := &fakeGen{plan: generated}
	ex := &stubExecutor{err: errors.New("step 1 failed")}
	stubSetup(t, gen, ex, true)

	if _, err := os.Stat(PlanPath("api")); !os.IsNotExist(err) {
		t.Fatalf("plan should not exist yet: %v", err)
	}

	err := Setup(context.Background(), "api", "/repo", "claude", false)
	if err == nil {
		t.Fatal("expected Setup to return the execution error")
	}
	if !ex.executed {
		t.Fatal("executor was never called")
	}
	if !ex.planOnDisk {
		t.Fatal("plan was not cached before execution started; a ctrl-c mid-run would lose it")
	}

	cached, found, lerr := Load("api")
	if lerr != nil || !found {
		t.Fatalf("plan not cached after a failed run: found=%v err=%v", found, lerr)
	}
	if cached.Repo != "api" || len(cached.Steps) != 2 || cached.Steps[0].Run != "bad-install" {
		t.Fatalf("cached plan is wrong: %+v", cached)
	}
	if len(cached.Verify) != 1 {
		t.Fatalf("cached plan lost its verify list: %+v", cached.Verify)
	}

	// A second run replays the cache instead of asking the harness again.
	gen.generates = 0
	ex2 := &stubExecutor{}
	stubSetup(t, gen, ex2, true)
	if err := Setup(context.Background(), "api", "/repo", "claude", false); err != nil {
		t.Fatalf("second Setup: %v", err)
	}
	if gen.generates != 0 {
		t.Fatalf("cached plan should not regenerate, Generate called %d times", gen.generates)
	}
}

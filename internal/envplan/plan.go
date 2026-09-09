// Package envplan: harness writes a plan once, CLI replays it forever.
// Owner: session "env". Plans are cached at ~/.agents/plans/<repo>.json.
package envplan

import "context"

type Step struct {
	Name      string `json:"name"`   // "Install Go 1.25"
	Run       string `json:"run"`    // bash, run on the box
	Verify    string `json:"verify"` // bash, exit 0 = ok; may be empty
	NeedsSudo bool   `json:"needs_sudo"`
}

type Check struct {
	Name string `json:"name"` // "Go"
	Cmd  string `json:"cmd"`  // "go version"
}

type Plan struct {
	Repo      string  `json:"repo"`
	Generated string  `json:"generated_by"` // "claude" | "codex"
	Steps     []Step  `json:"steps"`
	Verify    []Check `json:"verify"`
}

// Generator asks a harness to inspect repoDir and return a Plan.
type Generator interface {
	Generate(ctx context.Context, repoDir string) (Plan, error)
	// Repair returns a replacement for a failed step given its stderr.
	Repair(ctx context.Context, repoDir string, failed Step, stderr string) (Step, error)
}

// Executor runs a plan on the box with the ui step runner.
type Executor interface {
	Execute(ctx context.Context, p Plan, repoDir string, gen Generator) error
	VerifyOnly(ctx context.Context, p Plan, repoDir string) error
}

// ---- high level API used by `agents init` and `agents env` ----

// Plans are stored on the machine that runs the executor (the box).
func PlanPath(repo string) string                      { panic("TODO envplan") }
func Load(repo string) (p Plan, found bool, err error) { panic("TODO envplan") }
func Save(p Plan) error                                { panic("TODO envplan") }

// NewGenerator wraps a harness (by name: "claude"|"codex") as a Generator.
func NewGenerator(harnessName string) (Generator, error) { panic("TODO envplan") }

// NewLocalExecutor runs steps with os/exec on this machine (the box).
func NewLocalExecutor() Executor { panic("TODO envplan") }

// Setup is the whole flow: load cached plan or generate, show for approval
// via ui, execute, verify. Runs locally on the box.
func Setup(ctx context.Context, repo, repoDir, harnessName string, regen bool) error {
	panic("TODO envplan")
}

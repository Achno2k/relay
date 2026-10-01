// Package envplan: harness writes a plan once, CLI replays it forever.
// Owner: session "env". Plans are cached at ~/.agents/plans/<repo>.json.
package envplan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Achno2k/agents-cli/internal/config"
)

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

// PlanPath returns the cache file for a repo: ~/.agents/plans/<repo>.json.
func PlanPath(repo string) string {
	return filepath.Join(config.Dir(), "plans", slug(repo)+".json")
}

// slug makes a repo name safe to use as a file name.
func slug(repo string) string {
	repo = strings.TrimSuffix(strings.Trim(repo, "/"), ".git")
	repo = strings.NewReplacer("/", "-", string(filepath.Separator), "-", " ", "-", ":", "-").Replace(repo)
	if repo == "" {
		return "repo"
	}
	return repo
}

// Load reads the cached plan for a repo. found is false when there is none.
func Load(repo string) (p Plan, found bool, err error) {
	b, err := os.ReadFile(PlanPath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return Plan{}, false, nil
	}
	if err != nil {
		return Plan{}, false, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return Plan{}, false, err
	}
	return p, true, nil
}

// Save writes a plan to its cache file.
func Save(p Plan) error {
	path := PlanPath(p.Repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

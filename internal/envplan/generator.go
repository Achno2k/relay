package envplan

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Achno2k/agents-cli/internal/harness"
)

// planSchema is the JSON schema the harness must answer with for a full plan.
const planSchema = `{
  "type": "object",
  "required": ["steps", "verify"],
  "properties": {
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "run"],
        "properties": {
          "name": {"type": "string"},
          "run": {"type": "string"},
          "verify": {"type": "string"},
          "needs_sudo": {"type": "boolean"}
        }
      }
    },
    "verify": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "cmd"],
        "properties": {"name": {"type": "string"}, "cmd": {"type": "string"}}
      }
    }
  }
}`

// stepSchema is the JSON schema for a single replacement step.
const stepSchema = `{
  "type": "object",
  "required": ["name", "run"],
  "properties": {
    "name": {"type": "string"},
    "run": {"type": "string"},
    "verify": {"type": "string"},
    "needs_sudo": {"type": "boolean"}
  }
}`

// generatePrompt asks the harness to inspect the repo and write a plan.
func generatePrompt(repoDir string) string {
	return `Inspect the repository at ` + repoDir + ` and write a plan that makes a fresh
Ubuntu 24.04 machine able to build, test and run it.

Read the repo first: package manifests, lockfiles, tool-version files, Dockerfile,
docker-compose, CI workflows, Makefile and the README. Base every step on what you
actually find, not on guesses.

Rules for the plan:
- Assume git, gh, curl, build-essential and mise are already installed. Do not
  reinstall them.
- Install language runtimes and their versions with mise (` + "`mise use -g <tool>@<version>`" + `),
  pinned to the versions the repo asks for. Use apt only for system libraries that
  mise cannot provide.
- Every step must be idempotent: safe to re-run on a machine where it already ran.
- Every step should carry a "verify" command: bash that exits 0 when that step is
  already satisfied.
- Set "needs_sudo": true for any step that changes system state, and write the
  sudo into the "run" command itself (non-interactive, e.g. ` + "`sudo -n apt-get -y ...`" + `).
- Include steps for project dependencies (install, vendor, tidy) and for anything
  the app needs locally, such as a database or a .env file copied from an example.
- Do not start long-running servers, do not run the test suite, do not clone the
  repo, and never do anything destructive.
- End with a "verify" list: one short command per tool or service that proves the
  environment is ready (for example "go version", "node --version", "pg_isready").
- Keep it under 15 steps and order them so dependencies come first.`
}

// repairPrompt asks the harness for a replacement for one failed step.
func repairPrompt(failed Step, stderr string) string {
	return `A step from the environment plan failed on Ubuntu 24.04. Work out why from the
output and return a single replacement step that does the same job and succeeds.

Failed step name: ` + failed.Name + `
Failed step run:
` + failed.Run + `

Failed step verify:
` + failed.Verify + `

Output tail:
` + tailLines(stderr, 40) + `

Rules: keep the same intent, stay idempotent, keep a verify command, write any
sudo into the run command, and do not ask questions. Return the replacement step
only.`
}

// tailLines returns at most the last n lines of s.
func tailLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "(no output)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// harnessGenerator drives a harness through its headless JSON mode.
type harnessGenerator struct{ h harness.Harness }

// NewGenerator wraps a harness (by name: "claude"|"codex") as a Generator.
func NewGenerator(harnessName string) (Generator, error) {
	h, ok := harness.Registry[harnessName]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", harnessName)
	}
	return harnessGenerator{h: h}, nil
}

// Generate asks the harness to inspect repoDir and returns the parsed plan.
func (g harnessGenerator) Generate(ctx context.Context, repoDir string) (Plan, error) {
	out, err := g.h.HeadlessJSON(ctx, repoDir, generatePrompt(repoDir), planSchema)
	if err != nil {
		return Plan{}, err
	}
	p, err := parsePlan(out)
	if err != nil {
		return Plan{}, err
	}
	p.Generated = g.h.Name()
	if p.Repo == "" {
		p.Repo = filepath.Base(strings.TrimRight(repoDir, "/"))
	}
	return p, nil
}

// Repair asks the harness for a replacement for a step that failed.
func (g harnessGenerator) Repair(ctx context.Context, repoDir string, failed Step, stderr string) (Step, error) {
	out, err := g.h.HeadlessJSON(ctx, repoDir, repairPrompt(failed, stderr), stepSchema)
	if err != nil {
		return Step{}, err
	}
	return parseStep(out)
}

// parsePlan decodes and sanity-checks a generated plan.
func parsePlan(s string) (Plan, error) {
	var p Plan
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &p); err != nil {
		return Plan{}, fmt.Errorf("plan is not valid JSON: %w", err)
	}
	if len(p.Steps) == 0 {
		return Plan{}, fmt.Errorf("plan has no steps")
	}
	for i, st := range p.Steps {
		if strings.TrimSpace(st.Run) == "" {
			return Plan{}, fmt.Errorf("plan step %d (%q) has no run command", i+1, st.Name)
		}
		if strings.TrimSpace(st.Name) == "" {
			p.Steps[i].Name = fmt.Sprintf("Step %d", i+1)
		}
	}
	return p, nil
}

// parseStep decodes and sanity-checks a repaired step.
func parseStep(s string) (Step, error) {
	var st Step
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &st); err != nil {
		return Step{}, fmt.Errorf("repaired step is not valid JSON: %w", err)
	}
	if strings.TrimSpace(st.Run) == "" {
		return Step{}, fmt.Errorf("repaired step has no run command")
	}
	if strings.TrimSpace(st.Name) == "" {
		st.Name = "Repaired step"
	}
	return st, nil
}

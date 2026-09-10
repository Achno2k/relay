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

// alreadyOnBox lists what `agents init` has already installed. The generator
// is told to leave every one of these alone.
const alreadyOnBox = `git, gh, curl, unzip, mise, node 22 (installed via mise), herdr in
/usr/local/bin, the claude and codex CLIs, the agents binary, ~/.agents and its
config, the systemd units, the aws cli and session-manager-plugin`

// generatePrompt asks the harness to inspect the repo and write a plan.
func generatePrompt(repoDir string) string {
	return `Inspect the repository at ` + repoDir + ` and write the plan that makes this
Ubuntu 24.04 box able to build and test it.

Read the repo first: package manifests, lockfiles, tool-version files, Dockerfile,
docker-compose, CI workflows, Makefile and the README. Base every step on what you
actually find in the repo, not on guesses.

ALREADY INSTALLED AND WORKING ON THIS BOX. Do not install, upgrade, reinstall,
reconfigure or verify any of it, and do not add a step that merely checks for it:
` + alreadyOnBox + `.

So the plan covers repo-level needs only:
- language runtimes and versions this repo pins, installed with mise
  (` + "`mise use -g <tool>@<version>`" + `) — but never node unless the repo pins a
  version other than 22
- system libraries the build genuinely needs (apt, only when mise cannot provide it)
- downloading the project's dependencies
- any code generation the build requires
- one final step that builds or typechecks the project to prove the environment works

Rules:
- At most 10 steps. Fewer is better. If the repo needs nothing beyond what is
  already on the box, return the build step alone.
- Every step must be idempotent: safe to re-run on a box where it already ran.
- Every step should carry a "verify" command: bash that exits 0 when that step is
  already satisfied.
- Set "needs_sudo": true for any step that changes system state, and write the
  sudo into the "run" command itself (non-interactive, e.g. ` + "`sudo -n apt-get -y ...`" + `).
- Never edit shell profiles (.bashrc, .profile, .zshrc or anything in /etc/profile.d)
  and never touch ~/.agents. mise is already activated for this shell.
- Do not start long-running servers, do not run the test suite, do not clone the
  repo, and never do anything destructive.
- End with a "verify" list: one short command per runtime or service that proves the
  environment is ready (for example "go version", "python --version", "pg_isready").
- Order the steps so dependencies come first.`
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

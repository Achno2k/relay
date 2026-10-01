package envplan

import (
	"context"
	"fmt"
	"strings"

	"github.com/Achno2k/agents-cli/internal/ui"
)

// Seams for tests: the real ui and executor by default.
var (
	newGenerator = NewGenerator
	newExecutor  = NewLocalExecutor
	phaseFn      = ui.Phase
)

// Setup is the whole flow: load the cached plan or infer one, run it, verify.
// It runs unattended: nothing is printed but progress, and nothing is asked.
// Use `agents env show` to read the plan itself.
func Setup(ctx context.Context, repo, repoDir, harnessName string, regen bool) error {
	return phaseFn(ctx, "Setting up dev environment", func(ctx context.Context) error {
		return setup(ctx, repo, repoDir, harnessName, regen)
	})
}

// setup is Setup's body, run inside the outer phase.
func setup(ctx context.Context, repo, repoDir, harnessName string, regen bool) error {
	p, cached, err := Load(repo)
	if err != nil {
		return err
	}
	if regen {
		cached = false
	}
	if harnessName == "" {
		harnessName = p.Generated
	}
	if harnessName == "" {
		harnessName = "claude"
	}
	gen, err := newGenerator(harnessName)
	if err != nil {
		return err
	}

	title := "Inferring steps"
	if cached {
		title = "Loading cached plan"
	}
	err = phaseFn(ctx, title, func(ctx context.Context) error {
		if cached {
			return nil
		}
		made, err := gen.Generate(ctx, repoDir)
		if err != nil {
			return err
		}
		made.Repo = repo
		p = made
		return nil
	})
	if err != nil {
		return fmt.Errorf("infer steps: %w", err)
	}

	// Cache the plan before running it, so a crash or ctrl-c part-way
	// through replays this plan instead of inferring a new one.
	if err := Save(p); err != nil {
		ui.Warn("could not cache plan: " + err.Error())
	}

	ex := newExecutor()
	// Execute writes repaired steps back into p.Steps, so re-cache either way.
	execErr := ex.Execute(ctx, p, repoDir, gen)
	if execErr != nil {
		if err := Save(p); err != nil {
			ui.Warn("could not cache plan: " + err.Error())
		}
		return execErr
	}

	verifyErr := ex.VerifyOnly(ctx, p, repoDir)
	if err := Save(p); err != nil {
		return fmt.Errorf("cache plan: %w", err)
	}
	return verifyErr
}

// PrintPlan shows a plan as an aligned list with sudo steps marked.
func PrintPlan(p Plan) {
	ui.Title(fmt.Sprintf("Environment plan for %s (by %s)", p.Repo, p.Generated))
	pairs := make([]string, 0, len(p.Steps)*2)
	for i, s := range p.Steps {
		pairs = append(pairs, fmt.Sprintf("%d. %s", i+1, s.Name), summarise(s))
	}
	ui.KV(pairs...)
	if len(p.Verify) > 0 {
		names := make([]string, 0, len(p.Verify))
		for _, c := range p.Verify {
			names = append(names, c.Name)
		}
		ui.Muted("verify: " + strings.Join(names, ", "))
	}
}

// summarise renders a step's command on one line, marking sudo steps.
func summarise(s Step) string {
	line := firstLine(s.Run)
	if len(line) > 72 {
		line = line[:71] + "…"
	}
	if s.NeedsSudo {
		return "sudo · " + line
	}
	return line
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

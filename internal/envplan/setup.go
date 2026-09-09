package envplan

import (
	"context"
	"fmt"
	"strings"

	"github.com/Achno2k/agents-cli/internal/ui"
)

// Setup is the whole flow: load cached plan or generate, show for approval
// via ui, execute, verify. Runs locally on the box.
func Setup(ctx context.Context, repo, repoDir, harnessName string, regen bool) error {
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
	gen, err := NewGenerator(harnessName)
	if err != nil {
		return err
	}

	if !cached {
		var made Plan
		err := ui.Spinner(ctx, "Asking "+harnessName+" to plan the environment", func(ctx context.Context) error {
			var err error
			made, err = gen.Generate(ctx, repoDir)
			return err
		})
		if err != nil {
			return fmt.Errorf("generate plan: %w", err)
		}
		p = made
		p.Repo = repo
	} else {
		ui.Muted("Using cached plan " + PlanPath(repo))
	}

	PrintPlan(p)

	approved, err := ui.Confirm(fmt.Sprintf("Run these %d steps?", len(p.Steps)), true)
	if err != nil {
		return err
	}
	chosen := allIndices(len(p.Steps))
	if !approved {
		chosen, err = ui.MultiSelect("Pick the steps to run", stepLabels(p), chosen)
		if err != nil {
			return err
		}
		if len(chosen) == 0 {
			ui.Warn("No steps selected, nothing to do.")
			return nil
		}
	}

	run := p
	run.Steps = make([]Step, 0, len(chosen))
	for _, i := range chosen {
		run.Steps = append(run.Steps, p.Steps[i])
	}

	ex := NewLocalExecutor()
	execErr := ex.Execute(ctx, run, repoDir, gen)
	// Fold repaired steps back into the full plan before caching.
	for j, i := range chosen {
		p.Steps[i] = run.Steps[j]
	}
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

// stepLabels renders one picker row per step.
func stepLabels(p Plan) []string {
	out := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		label := s.Name
		if s.NeedsSudo {
			label += " (sudo)"
		}
		out = append(out, label)
	}
	return out
}

func allIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

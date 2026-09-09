package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/envplan"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
)

var (
	envHarness string
	envRegen   bool
	envDir     string
)

func init() {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Plan, run and check a repo's dev environment on the box",
		Long: "The plan is written once by a harness, approved by you, then replayed.\n" +
			"Runs on the box; from the laptop it proxies itself over ssh.",
	}

	setup := &cobra.Command{
		Use:   "setup <repo>",
		Short: "Generate or replay the environment plan for a repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return envRun(c.Context(), args[0], func(repo, dir string) error {
				return envplan.Setup(c.Context(), repo, dir, envHarness, envRegen)
			})
		},
	}
	setup.Flags().StringVar(&envHarness, "harness", "", "harness that writes the plan: claude|codex")
	setup.Flags().BoolVar(&envRegen, "regen", false, "ignore the cached plan and generate a new one")

	verify := &cobra.Command{
		Use:   "verify <repo>",
		Short: "Run the plan's verification checks",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return envRun(c.Context(), args[0], func(repo, dir string) error {
				p, err := envLoadPlan(repo)
				if err != nil {
					return err
				}
				return envplan.NewLocalExecutor().VerifyOnly(c.Context(), p, dir)
			})
		},
	}

	show := &cobra.Command{
		Use:   "show <repo>",
		Short: "Print the cached plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return envRun(c.Context(), args[0], func(repo, dir string) error {
				p, err := envLoadPlan(repo)
				if err != nil {
					return err
				}
				envplan.PrintPlan(p)
				ui.Muted("cached at " + envplan.PlanPath(repo))
				return nil
			})
		},
	}

	regen := &cobra.Command{
		Use:   "regen <repo>",
		Short: "Throw away the cached plan and generate a fresh one",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return envRun(c.Context(), args[0], func(repo, dir string) error {
				return envplan.Setup(c.Context(), repo, dir, envHarness, true)
			})
		},
	}
	regen.Flags().StringVar(&envHarness, "harness", "", "harness that writes the plan: claude|codex")

	for _, sub := range []*cobra.Command{setup, verify, show, regen} {
		sub.Flags().StringVar(&envDir, "dir", "", "path to the repo checkout (default <work_dir>/<repo>)")
		cmd.AddCommand(sub)
	}
	Register(cmd)
}

// envRun proxies to the box when this is the laptop, otherwise resolves the
// repo directory and runs fn locally.
func envRun(ctx context.Context, repoArg string, fn func(repo, dir string) error) error {
	if os.Getenv("AGENTS_ON_BOX") != "1" {
		return envProxyToBox(ctx)
	}
	repo, dir, err := envResolveRepo(repoArg)
	if err != nil {
		return err
	}
	return fn(repo, dir)
}

// envProxyToBox re-runs this exact command on the box over ssh.
func envProxyToBox(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
		return config.ErrNotInitialised
	}
	r := sshx.New(sshx.Target{
		Host:       cfg.Box.Host,
		User:       cfg.Box.User,
		Transport:  cfg.Box.Transport,
		KeyPath:    cfg.Box.KeyPath,
		InstanceID: cfg.AWS.InstanceID,
		Profile:    cfg.AWS.Profile,
		Region:     cfg.AWS.Region,
	})
	return r.Interactive(ctx, "agents "+envQuoteArgs(os.Args[1:]))
}

// envLoadPlan reads the cached plan or explains how to make one.
func envLoadPlan(repo string) (envplan.Plan, error) {
	p, found, err := envplan.Load(repo)
	if err != nil {
		return p, err
	}
	if !found {
		return p, fmt.Errorf("no plan for %q yet, run `agents env setup %s`", repo, repo)
	}
	return p, nil
}

// envResolveRepo turns the command argument into a repo name and a checkout
// directory on the box.
func envResolveRepo(arg string) (repo, dir string, err error) {
	repo = strings.TrimSuffix(filepath.Base(strings.TrimRight(arg, "/")), ".git")
	switch {
	case envDir != "":
		dir = envExpand(envDir)
	case strings.ContainsAny(arg, "/.~") && envIsDir(envExpand(arg)):
		dir = envExpand(arg)
	default:
		cfg, cerr := config.Load()
		if cerr != nil && cerr != config.ErrNotInitialised {
			return "", "", cerr
		}
		work := cfg.Box.WorkDir
		if work == "" {
			work = "~/work"
		}
		dir = filepath.Join(envExpand(work), arg, "main") // main checkout, see PLAN.md box layout
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	if !envIsDir(dir) {
		return "", "", fmt.Errorf("repo checkout %s does not exist, pass --dir", dir)
	}
	return repo, dir, nil
}

func envIsDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// envExpand resolves a leading ~ against the home directory.
func envExpand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// envQuoteArgs joins argv into a single shell-safe command string.
func envQuoteArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, envQuote(a))
	}
	return strings.Join(out, " ")
}

func envQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || r == '=' || r == '+' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

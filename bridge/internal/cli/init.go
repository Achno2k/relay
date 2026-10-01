package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"relay/internal/awsx"
	"relay/internal/bootstrap"
	"relay/internal/config"
	"relay/internal/harness"
	"relay/internal/sshx"
	"relay/internal/ui"
)

func init() { Register(newInitCmd()) }

func newInitCmd() *cobra.Command {
	var fresh bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up the EC2 box: tools, harnesses and a repo",
		Long: "Runs the full first-run flow from your laptop: pick the instance, install the\n" +
			"base toolchain and harnesses, sign in, clone a repo and build its environment.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.Context(), fresh)
		},
	}
	cmd.Flags().BoolVar(&fresh, "fresh", false, "ignore the saved box and pick an instance again")
	return cmd
}

func runInit(ctx context.Context, fresh bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if os.Getenv("RELAY_ON_BOX") == "1" {
		return errors.New("`relay init` runs on your laptop, not on the box")
	}

	ui.Banner(Version)

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNotInitialised) {
		return err
	}

	// 0. offer to pick up where a previous run left off. Every later step is
	// idempotent, so resuming only skips the two questions we already have
	// answers for.
	resume := false
	if !fresh && cfg.AWS.InstanceID != "" {
		yes, err := ui.Confirm(resumePrompt(cfg), true)
		if err != nil {
			return err
		}
		resume = yes
	}

	// 1. profile → region → instance.
	if !resume {
		ui.Title("AWS")
		aws, box, err := awsx.PickTarget(ctx)
		if err != nil {
			return err
		}
		cfg.AWS, cfg.Box = aws, box
	}
	if cfg.Box.WorkDir == "" {
		cfg.Box.WorkDir = config.Default().Box.WorkDir
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	runner := sshx.New(sshx.Target{
		Host:       cfg.Box.Host,
		User:       cfg.Box.User,
		Transport:  cfg.Box.Transport,
		KeyPath:    cfg.Box.KeyPath,
		InstanceID: cfg.AWS.InstanceID,
		Profile:    cfg.AWS.Profile,
		Region:     cfg.AWS.Region,
	})

	// 2. connectivity.
	if err := ui.Spinner(ctx, "Connecting to "+cfg.Box.User+"@"+cfg.Box.Host, runner.Ping); err != nil {
		return fmt.Errorf("cannot reach the box: %w", err)
	}
	ui.Success("Connected")

	// 3. which harnesses. A resumed run keeps the saved list, unless the
	// previous run never got as far as answering.
	if !resume || len(cfg.Harness) == 0 {
		ui.Title("Harnesses")
		known := bootstrap.KnownHarnesses()

		// Ask the box what it already has, so a rerun does not look like a
		// fresh install and the obvious answer is preselected.
		var marks []string
		var installed []int
		if err := ui.Spinner(ctx, "Checking installed harnesses", func(ctx context.Context) error {
			marks, installed = harnessMarks(ctx, runner, known)
			return nil
		}); err != nil {
			return err
		}

		ui.Muted("space toggles, enter confirms")
		picked, err := ui.MultiSelectMarked("Which harnesses should this box run?", known, marks, installed)
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			return errors.New("pick at least one harness")
		}
		cfg.Harness = pick(known, picked)
		if err := config.Save(cfg); err != nil {
			return err
		}
	}

	// 4. bootstrap: base tools, herdr, harnesses, relay binary, systemd units.
	ui.Title("Box setup")
	goarch, err := awsx.InstanceArch(ctx, cfg.AWS)
	if err != nil {
		return err
	}
	if err := bootstrap.Install(ctx, runner, bootstrap.Options{
		Harnesses: cfg.Harness,
		GoArch:    goarch,
		User:      cfg.Box.User,
		Home:      "/home/" + cfg.Box.User,
	}); err != nil {
		return err
	}

	// 5. harness login, one interactive session each.
	ui.Title("Sign in")
	signedIn, err := bootstrap.Login(ctx, runner, cfg.Harness)
	if err != nil {
		return err
	}
	if strings.Join(signedIn, ",") != strings.Join(cfg.Harness, ",") {
		cfg.Harness = signedIn
		if err := config.Save(cfg); err != nil {
			return err
		}
	}

	// 6. repo: detect here, confirm, clone there.
	repo, err := initRepo(ctx, runner, &cfg)
	if err != nil {
		return err
	}

	if err := bootstrap.SyncConfig(ctx, runner, boxHome(cfg), cfg); err != nil {
		return err
	}

	// 7. environment plan. It needs harness auth and runs on the box.
	if repo != "" {
		ui.Title("Environment")
		if err := runner.Interactive(ctx, bootstrap.InteractiveCmd("relay env setup "+repo)); err != nil {
			return fmt.Errorf("env setup: %w", err)
		}
	}

	// 8. summary.
	initSummary(cfg, repo)
	return nil
}

// initRepo detects the repo the user is standing in, confirms it, and clones it
// on the box. Returns the short repo name, or "" when the user declines.
func initRepo(ctx context.Context, runner sshx.Runner, cfg *config.Config) (string, error) {
	ui.Title("Repository")
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	origin, err := bootstrap.DetectOrigin(ctx, wd)
	if err != nil {
		ui.Warn("No git origin here, skipping the repo step. Run `relay init` from a repo to add one.")
		return "", nil
	}

	ok, err := ui.Confirm("Clone "+origin.URL+" on the box?", true)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}

	if !bootstrap.GitHubReady(ctx, runner) {
		if err := initGitHubAuth(ctx, runner); err != nil {
			return "", err
		}
	}

	// Clone, and on an auth-shaped failure offer to sign in to GitHub with a
	// different account and retry. "Repository not found" over https is what
	// GitHub returns for a private repo the current token cannot see.
	for attempt := 0; ; attempt++ {
		var cloneLog bytes.Buffer
		err = ui.RunSteps(ctx, "Cloning "+origin.Name, []ui.Step{{
			Name: "git clone into " + bootstrap.DisplayPath(bootstrap.CheckoutDir(cfg.Box.WorkDir, origin.Name)),
			Run: func(ctx context.Context, log io.Writer) error {
				return bootstrap.Clone(ctx, runner, cfg.Box.WorkDir, origin, io.MultiWriter(log, &cloneLog))
			},
		}})
		if err == nil {
			break
		}
		if attempt > 0 || !bootstrap.LooksLikeGitAuthFailure(cloneLog.String()) {
			return "", err
		}
		ui.Warn("GitHub says " + origin.Name + " does not exist or is not visible to the account signed in on the box.")
		ui.Info("If the repo belongs to another account or org, sign in with that one now. gh keeps both accounts and uses the new one for this clone.")
		again, cerr := ui.Confirm("Sign in to GitHub with a different account and retry?", true)
		if cerr != nil {
			return "", cerr
		}
		if !again {
			return "", err
		}
		if err := initGitHubAuth(ctx, runner); err != nil {
			return "", err
		}
	}

	if cfg.Repos == nil {
		cfg.Repos = map[string]config.Repo{}
	}
	cfg.Repos[origin.Name] = config.Repo{URL: origin.URL, DefaultBranch: origin.DefaultBranch}
	if err := config.Save(*cfg); err != nil {
		return "", err
	}
	return origin.Name, nil
}

// initGitHubAuth gives the box credentials for GitHub. A fine-grained token is
// the default because a browser login hands the box the user's whole account.
func initGitHubAuth(ctx context.Context, runner sshx.Runner) error {
	ui.Warn("gh is not signed in on the box; a private repo will fail to clone.")

	const (
		optToken = iota
		optBrowser
		optSkip
	)
	choice, err := ui.Select("How should the box authenticate to GitHub?", []string{
		"Personal access token, fine-grained or classic (recommended)",
		"Browser login (full account access)",
		"Skip (public repos only)",
	})
	if err != nil {
		return err
	}

	switch choice {
	case optToken:
		return initGitHubToken(ctx, runner)
	case optBrowser:
		if err := bootstrap.GitHubLogin(ctx, runner); err != nil {
			return fmt.Errorf("gh auth login: %w", err)
		}
		return nil
	default:
		ui.Warn("Skipped. The box can only clone public repos until you run `gh auth login` on it.")
		return nil
	}
}

// initGitHubToken walks the user through creating a fine-grained token and
// hands it to the box. One retry, since a mistyped or under-scoped token is the
// likely failure.
func initGitHubToken(ctx context.Context, runner sshx.Runner) error {
	ui.Info("Fine-grained token, scoped to chosen repos of one account:")
	ui.Code(bootstrap.TokenURL)
	for _, scope := range bootstrap.TokenScopes {
		ui.Muted("  " + scope)
	}
	ui.Info("Classic token, for every repo and org you can reach (ghp_...):")
	ui.Code(bootstrap.ClassicTokenURL)
	for _, scope := range bootstrap.ClassicTokenScopes {
		ui.Muted("  " + scope)
	}
	ui.Muted("  Orgs with SAML SSO: click 'Configure SSO' next to the classic token and authorize each org.")

	for attempt := 0; attempt < 2; attempt++ {
		token, err := ui.Secret("Paste the token")
		if err != nil {
			return err
		}
		loginErr := bootstrap.GitHubTokenLogin(ctx, runner, token)
		if loginErr == nil {
			ui.Success("GitHub token accepted on the box")
			return nil
		}
		ui.Fail("The box rejected that token: " + loginErr.Error())
		if attempt == 1 {
			return fmt.Errorf("gh auth login --with-token: %w", loginErr)
		}

		again, err := ui.Confirm("Try another token?", true)
		if err != nil {
			return err
		}
		if !again {
			ui.Warn("Continuing without GitHub credentials; only public repos will clone.")
			return nil
		}
	}
	return nil
}

// initSummary prints the handful of lines the user actually needs afterwards.
func initSummary(cfg config.Config, repo string) {
	box := cfg.Box.User + "@" + cfg.Box.Host
	if cfg.AWS.InstanceID != "" {
		box += " (" + cfg.AWS.InstanceID + ")"
	}
	repoPath := "none yet"
	if repo != "" {
		repoPath = bootstrap.DisplayPath(bootstrap.CheckoutDir(cfg.Box.WorkDir, repo))
	}
	harnesses := strings.Join(cfg.Harness, ", ")
	if harnesses == "" {
		harnesses = "none"
	}

	ui.Ready("Machine ready",
		"Box", box,
		"Region", cfg.AWS.Region,
		"Repo", repoPath,
		"Harnesses", harnesses,
	)

	ui.Commands(
		"relay attach",
		"relay ssh",
	)
}

// resumePrompt describes the saved box so the user can tell at a glance whether
// it is the one they mean.
func resumePrompt(cfg config.Config) string {
	harnesses := strings.Join(cfg.Harness, ", ")
	if harnesses == "" {
		harnesses = "none yet"
	}
	profile := cfg.AWS.Profile
	if profile == "" {
		profile = "default"
	}
	region := cfg.AWS.Region
	if region == "" {
		region = "unknown region"
	}
	return fmt.Sprintf("Resume with %s in %s (profile %s, harnesses %s)?",
		cfg.AWS.InstanceID, region, profile, harnesses)
}

// harnessMarks asks the box which harnesses are already on it. Returns the
// per-option mark for the picker ("installed" or empty) and the indices to
// preselect. An unreachable check just reads as not installed.
func harnessMarks(ctx context.Context, runner sshx.Runner, known []string) (marks []string, installed []int) {
	marks = make([]string, len(known))
	for i, name := range known {
		h, ok := harness.Registry[name]
		if !ok || !bootstrap.IsInstalled(ctx, runner, h) {
			continue
		}
		marks[i] = "installed"
		installed = append(installed, i)
	}
	return marks, installed
}

func pick(all []string, idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(all) {
			out = append(out, all[i])
		}
	}
	return out
}

// boxHome is the box user's home directory.
func boxHome(cfg config.Config) string {
	if cfg.Box.User == "" || cfg.Box.User == "root" {
		return "/root"
	}
	return "/home/" + cfg.Box.User
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/awsx"
	"github.com/Achno2k/agents-cli/internal/bootstrap"
	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
)

func init() { Register(newInitCmd()) }

func newInitCmd() *cobra.Command {
	var skipSlack, fresh bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up the EC2 box: tools, harnesses, a repo and the Slack bot",
		Long: "Runs the full first-run flow from your laptop: pick the instance, install the\n" +
			"base toolchain and harnesses, sign in, clone a repo, build its environment,\n" +
			"and optionally start the Slack bot.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.Context(), skipSlack, fresh)
		},
	}
	cmd.Flags().BoolVar(&skipSlack, "skip-slack", false, "don't ask for Slack tokens")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "ignore the saved box and pick an instance again")
	return cmd
}

func runInit(ctx context.Context, skipSlack, fresh bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if os.Getenv("AGENTS_ON_BOX") == "1" {
		return errors.New("`agents init` runs on your laptop, not on the box")
	}

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
		// Nothing preselected: an accidental Enter must not install every harness.
		ui.Muted("space toggles, enter confirms")
		picked, err := ui.MultiSelect("Which harnesses should this box run?", known, nil)
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

	// 4. bootstrap: base tools, herdr, harnesses, agents binary, systemd units.
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
		if err := runner.Interactive(ctx, bootstrap.InteractiveCmd("agents env setup "+repo)); err != nil {
			return fmt.Errorf("env setup: %w", err)
		}
	}

	// 8. Slack, optional.
	if !skipSlack {
		if err := initSlack(ctx, runner, cfg); err != nil {
			return err
		}
	}

	// 9. summary.
	if err := bootstrap.SyncConfig(ctx, runner, boxHome(cfg), cfg); err != nil {
		return err
	}
	if err := bootstrap.RestartBotIfActive(ctx, runner); err != nil {
		ui.Warn("could not restart the bot to pick up the new config: " + err.Error())
	}
	initSummary(ctx, runner, cfg, repo)
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
		ui.Warn("No git origin here, skipping the repo step. Run `agents init` from a repo to add one.")
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
		"Fine-grained token (recommended)",
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
	ui.Info("Create a fine-grained personal access token here:")
	ui.Code(bootstrap.TokenURL)
	ui.Info("Give it:")
	for _, scope := range bootstrap.TokenScopes {
		ui.Muted("  " + scope)
	}

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

// initSlack collects the two socket-mode tokens, proves they work before
// anything is written to the box, takes the user allowlist, and starts the bot.
func initSlack(ctx context.Context, runner sshx.Runner, cfg config.Config) error {
	ui.Title("Slack")
	want, err := ui.Confirm("Set up the Slack bot now?", false)
	if err != nil || !want {
		if err == nil {
			ui.Muted("Skipped. Run `agents init` again to add it later.")
		}
		return err
	}

	botToken, appToken, err := askSlackTokens(ctx)
	if err != nil {
		return err
	}

	allowed, err := askSlackAllowlist()
	if err != nil {
		return err
	}
	cfg.Slack.AllowedUserIDs = allowed
	if err := config.Save(cfg); err != nil {
		return err
	}

	home := boxHome(cfg)
	return ui.RunSteps(ctx, "Starting the bot", []ui.Step{
		{
			Name: "Writing " + bootstrap.SlackEnvPath(home),
			Run: func(ctx context.Context, _ io.Writer) error {
				return bootstrap.WriteSlackEnv(ctx, runner, home, botToken, appToken)
			},
		},
		{
			Name: "Syncing config to the box",
			Run: func(ctx context.Context, _ io.Writer) error {
				return bootstrap.SyncConfig(ctx, runner, home, cfg)
			},
		},
		{
			Name: "Enabling agents-bot.service",
			Run: func(ctx context.Context, log io.Writer) error {
				return bootstrap.EnableBot(ctx, runner, log)
			},
		},
	})
}

// askSlackTokens reads both tokens and checks them against Slack from the
// laptop. A token Slack rejects here would otherwise reach the box and leave
// systemd restart-looping on invalid_auth, which says nothing useful.
func askSlackTokens(ctx context.Context) (botToken, appToken string, err error) {
	const attempts = 3
	for attempt := 1; ; attempt++ {
		botToken, err = ui.Input("Bot token", "xoxb-...")
		if err != nil {
			return "", "", err
		}
		appToken, err = ui.Input("App-level token", "xapp-...")
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(botToken) == "" || strings.TrimSpace(appToken) == "" {
			ui.Fail("Both a bot token and an app-level token are required.")
			if attempt == attempts {
				return "", "", errors.New("no usable Slack tokens after " + strconv.Itoa(attempts) + " attempts")
			}
			continue
		}

		var team, user string
		checkErr := ui.Spinner(ctx, "Checking tokens", func(ctx context.Context) error {
			var err error
			team, user, err = bootstrap.CheckSlackTokens(ctx, botToken, appToken)
			return err
		})
		if checkErr == nil {
			ui.Success("Signed in as " + user + " in " + team)
			return botToken, appToken, nil
		}

		ui.Fail(checkErr.Error())
		var tokErr *bootstrap.SlackTokenError
		if errors.As(checkErr, &tokErr) {
			ui.Muted(tokErr.Hint)
		} else {
			ui.Muted(bootstrap.BotTokenHint)
			ui.Muted(bootstrap.AppTokenHint)
		}
		if attempt == attempts {
			return "", "", checkErr
		}
	}
}

// askSlackAllowlist reads the Slack member IDs allowed to drive the bot. The
// bot ignores everyone else, so an empty list would make it useless.
func askSlackAllowlist() ([]string, error) {
	ui.Muted("Slack profile → three dots → Copy member ID")
	for {
		raw, err := ui.Input("Slack member IDs allowed to use the bot (comma separated)", "U012ABCDEF, U345GHIJKL")
		if err != nil {
			return nil, err
		}
		if ids := bootstrap.ParseUserIDs(raw); len(ids) > 0 {
			return ids, nil
		}
		ui.Fail("At least one member ID is required; the bot answers nobody else.")
	}
}

// initSummary prints the handful of lines the user actually needs afterwards.
func initSummary(ctx context.Context, runner sshx.Runner, cfg config.Config, repo string) {
	ui.Title("Ready")
	pairs := []string{
		"Instance", cfg.AWS.InstanceID + " (" + cfg.AWS.Region + ")",
		"Box", cfg.Box.User + "@" + cfg.Box.Host,
		"Harnesses", strings.Join(cfg.Harness, ", "),
		"Bot", bootstrap.BotStatus(ctx, runner),
	}
	if repo != "" {
		pairs = append(pairs, "Repo", bootstrap.DisplayPath(bootstrap.CheckoutDir(cfg.Box.WorkDir, repo)))
	}
	ui.KV(pairs...)

	ui.Info("Shell on the box")
	ui.Code("agents ssh")
	ui.Info("Attach to the box's herdr session")
	ui.Code("herdr --remote " + cfg.Box.User + "@" + cfg.Box.Host)
	if repo != "" {
		ui.Info("Start work from Slack by mentioning the bot, or locally")
		ui.Code("agents sessions list")
	}
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

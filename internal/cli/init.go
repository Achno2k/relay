package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	var skipSlack bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up the EC2 box: tools, harnesses, a repo and the Slack bot",
		Long: "Runs the full first-run flow from your laptop: pick the instance, install the\n" +
			"base toolchain and harnesses, sign in, clone a repo, build its environment,\n" +
			"and optionally start the Slack bot.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.Context(), skipSlack)
		},
	}
	cmd.Flags().BoolVar(&skipSlack, "skip-slack", false, "don't ask for Slack tokens")
	return cmd
}

func runInit(ctx context.Context, skipSlack bool) error {
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

	// 1. profile → region → instance.
	ui.Title("AWS")
	aws, box, err := awsx.PickTarget(ctx)
	if err != nil {
		return err
	}
	cfg.AWS, cfg.Box = aws, box
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

	// 3. which harnesses.
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
	if err := bootstrap.Login(ctx, runner, cfg.Harness); err != nil {
		return err
	}

	// 6. repo: detect here, confirm, clone there.
	repo, err := initRepo(ctx, runner, &cfg)
	if err != nil {
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
		ui.Warn("gh is not signed in on the box; a private repo will fail to clone.")
		yes, err := ui.Confirm("Sign in to GitHub on the box now?", true)
		if err != nil {
			return "", err
		}
		if yes {
			if err := bootstrap.GitHubLogin(ctx, runner); err != nil {
				return "", fmt.Errorf("gh auth login: %w", err)
			}
		}
	}

	err = ui.RunSteps(ctx, "Cloning "+origin.Name, []ui.Step{{
		Name: "git clone into " + bootstrap.CheckoutDir(cfg.Box.WorkDir, origin.Name),
		Run: func(ctx context.Context, log io.Writer) error {
			return bootstrap.Clone(ctx, runner, cfg.Box.WorkDir, origin, log)
		},
	}})
	if err != nil {
		return "", err
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

// initSlack collects the two socket-mode tokens and starts the bot unit.
func initSlack(ctx context.Context, runner sshx.Runner, cfg config.Config) error {
	ui.Title("Slack")
	want, err := ui.Confirm("Set up the Slack bot now?", false)
	if err != nil || !want {
		if err == nil {
			ui.Muted("Skipped. Run `agents init` again to add it later.")
		}
		return err
	}

	botToken, err := ui.Input("Bot token", "xoxb-...")
	if err != nil {
		return err
	}
	appToken, err := ui.Input("App-level token", "xapp-...")
	if err != nil {
		return err
	}
	if strings.TrimSpace(botToken) == "" || strings.TrimSpace(appToken) == "" {
		return errors.New("both a bot token and an app-level token are required")
	}

	home := "/home/" + cfg.Box.User
	return ui.RunSteps(ctx, "Starting the bot", []ui.Step{
		{
			Name: "Writing " + bootstrap.SlackEnvPath(home),
			Run: func(ctx context.Context, _ io.Writer) error {
				return bootstrap.WriteSlackEnv(ctx, runner, home, botToken, appToken)
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
		pairs = append(pairs, "Repo", bootstrap.CheckoutDir(cfg.Box.WorkDir, repo))
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


func pick(all []string, idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(all) {
			out = append(out, all[i])
		}
	}
	return out
}

package cli

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/herdr"
	"github.com/Achno2k/agents-cli/internal/slackbot"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/state"
	"github.com/Achno2k/agents-cli/internal/ui"
)

func init() {
	cmd := &cobra.Command{
		Use:   "bot",
		Short: "Run the Slack bot",
		Long: "One Slack thread is one agent session. Runs on the box under systemd;\n" +
			"from the laptop it proxies itself over ssh.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return botRun(c.Context()) },
	}
	Register(cmd)
}

func botRun(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if os.Getenv("AGENTS_ON_BOX") != "1" {
		return botProxyToBox(ctx)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Slack.AllowedUserIDs) == 0 {
		ui.Warn("slack.allowed_user_ids is empty, so every mention will be ignored")
	}

	st, err := state.Open(state.DefaultPath())
	if err != nil {
		return err
	}
	defer st.Close()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	bot, err := slackbot.NewFromEnv(herdr.New(), st, cfg, logger)
	if err != nil {
		if errors.Is(err, slackbot.ErrNoTokens) {
			ui.Fail("SLACK_BOT_TOKEN and SLACK_APP_TOKEN are not set")
			ui.Muted("bootstrap writes them to ~/.agents/slack.env, which the agents-bot unit loads")
		}
		return err
	}

	ui.Title("Slack bot")
	ui.KV(
		"harness", botHarness(cfg),
		"work dir", cfg.Box.WorkDir,
		"max live", botItoa(cfg.Sessions.MaxLive),
		"idle ttl", botItoa(cfg.Sessions.IdleTTLHours)+"h",
		"allowlist", botItoa(len(cfg.Slack.AllowedUserIDs))+" user(s)",
	)
	ui.Info("Listening on Socket Mode. Ctrl-C to stop.")

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	ui.Muted("stopped")
	return nil
}

// botProxyToBox re-runs `agents bot` on the box over ssh.
func botProxyToBox(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
		return config.ErrNotInitialised
	}
	ui.Muted("running on the box over ssh")
	r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	return r.Interactive(ctx, "agents "+botQuoteArgs(os.Args[1:]))
}

func botQuoteArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(out, " ")
}

func botHarness(cfg config.Config) string {
	if len(cfg.Harness) > 0 {
		return cfg.Harness[0]
	}
	return "claude"
}

// botItoa renders a policy number, calling out the ones left at their default.
func botItoa(n int) string {
	if n == 0 {
		return "default"
	}
	return strconv.Itoa(n)
}

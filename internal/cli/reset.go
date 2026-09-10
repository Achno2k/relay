package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/bootstrap"
	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
)

func init() {
	var yes, local bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove everything agents installed on the box",
		Long: "Stops the bot and herdr units, signs out of Claude, Codex and GitHub,\n" +
			"deletes the runtimes, caches, clones and worktrees, and the agents config\n" +
			"on the box. The instance, OS and your ssh access stay. Uncommitted work in\n" +
			"~/work is lost. For a truly fresh machine, terminate the instance instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReset(cmd.Context(), yes, local)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation")
	cmd.Flags().BoolVar(&local, "local", false, "also delete ~/.agents on this laptop")
	Register(cmd)
}

func runReset(ctx context.Context, yes, local bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
		return errors.New("no box in config")
	}
	target := cfg.Box.User + "@" + cfg.Box.Host
	if !yes {
		ui.Warn("This wipes " + target + ": units, harness logins, runtimes, clones and worktrees. Uncommitted work is lost.")
		typed, err := ui.Input("Type the instance id to confirm", cfg.AWS.InstanceID)
		if err != nil {
			return err
		}
		if typed != cfg.AWS.InstanceID {
			return errors.New("aborted")
		}
	}
	runner := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	if err := ui.Spinner(ctx, "Resetting "+target, func(ctx context.Context) error {
		return runner.Run(ctx, bootstrap.ResetScript(), os.Stdout, os.Stderr)
	}); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	if local {
		if err := os.RemoveAll(config.Dir()); err != nil {
			return err
		}
		ui.Success("removed " + filepath.Clean(config.Dir()))
	}
	ui.Info("Run `agents init` to set the box up again.")
	return nil
}

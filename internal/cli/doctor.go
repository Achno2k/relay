package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() { Register(doctorCmd) }

// doctorCmd runs local dependency, config, and connectivity checks.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check local dependencies, config, and connectivity to the box",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, cfgErr := config.Load()
	needPlugin := cfg.Box.Transport == "ssm"
	_, pluginErr := exec.LookPath("session-manager-plugin")

	checks := []ui.Check{
		{Name: "ssh", Run: func(context.Context) error { return onPath("ssh") }},
		{Name: "aws cli", Run: func(context.Context) error { return onPath("aws") }},
		{Name: "herdr", Run: func(context.Context) error { return onPath("herdr") }},
		{Name: "session-manager-plugin", Run: func(context.Context) error {
			if pluginErr == nil {
				return nil
			}
			if needPlugin {
				return errors.New("not on PATH (required for SSM transport)")
			}
			return errors.New("not on PATH (optional, only needed for SSM transport)")
		}},
		{Name: "config", Run: func(context.Context) error { return cfgErr }},
		{Name: "box reachable", Run: func(context.Context) error {
			if cfgErr != nil {
				return cfgErr
			}
			if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
				return errors.New("no host in config")
			}
			if err := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box)).Ping(ctx); err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			return nil
		}},
	}

	failed, err := ui.RunChecks(ctx, "agents doctor", checks)
	if err != nil {
		return err
	}
	// A missing session-manager-plugin is soft when not using SSM.
	pluginOptional := pluginErr != nil && !needPlugin
	if failed > 0 && !(failed == 1 && pluginOptional) {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	if pluginOptional {
		ui.Muted("optional failure ignored: session-manager-plugin (only needed for SSM transport)")
	}
	return nil
}

// onPath returns nil when name resolves to an executable on PATH.
func onPath(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return errors.New("not on PATH")
	}
	return nil
}

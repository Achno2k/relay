package cli

import (
	"context"
	"strings"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/spf13/cobra"
)

func init() { Register(newSSHCmd()) }

func newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [command...]",
		Short: "Open an interactive shell on the box",
		Long:  "Open an interactive shell on the configured box. With arguments, runs them on the box instead.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
				return config.ErrNotInitialised
			}
			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
			return r.Interactive(ctx, strings.Join(args, " "))
		},
	}
}

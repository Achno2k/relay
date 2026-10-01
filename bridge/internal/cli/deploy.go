package cli

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"relay/internal/awsx"
	"relay/internal/bootstrap"
	"relay/internal/config"
	"relay/internal/sshx"
	"relay/internal/ui"
)

func init() {
	Register(&cobra.Command{
		Use:   "deploy",
		Short: "Build relay, upload it to the box, sync config and restart the bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context())
		},
	})
}

func runDeploy(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
		return errors.New("no box in config, run `relay init`")
	}
	runner := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	goarch, err := awsx.InstanceArch(ctx, cfg.AWS)
	if err != nil {
		return err
	}
	var bin string
	return ui.RunSteps(ctx, "Deploying to "+cfg.Box.User+"@"+cfg.Box.Host, []ui.Step{
		{Name: "Building relay for linux/" + goarch, Run: func(ctx context.Context, _ io.Writer) error {
			bin, err = bootstrap.BuildForBox(ctx, goarch)
			return err
		}},
		{Name: "Uploading binary", Run: func(ctx context.Context, _ io.Writer) error {
			return bootstrap.InstallBinary(ctx, runner, bin)
		}},
		{Name: "Syncing config", Run: func(ctx context.Context, _ io.Writer) error {
			return bootstrap.SyncConfig(ctx, runner, boxHome(cfg), cfg)
		}},
		{Name: "Restarting relay.service", Run: func(ctx context.Context, _ io.Writer) error {
			return bootstrap.RestartRelayIfActive(ctx, runner)
		}},
	})
}

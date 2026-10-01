package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"relay/internal/awsx"
	"relay/internal/bootstrap"
	"relay/internal/config"
	"relay/internal/sshx"
	"relay/internal/ui"
)

// resetConfirmWord is what the user types to go ahead. Exact, case sensitive.
const resetConfirmWord = "Delete"

func init() {
	var yes, local bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove everything relay installed on the box",
		Long: "Stops the relay and herdr units, signs out of Claude, Codex and GitHub,\n" +
			"deletes the runtimes, caches, clones and worktrees, and the relay config\n" +
			"on the box. The instance, OS and your ssh access stay. Uncommitted work in\n" +
			"~/work is lost. For a truly fresh machine, terminate the instance instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReset(cmd.Context(), yes, local)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation")
	cmd.Flags().BoolVar(&local, "local", false, "also delete the relay config (~/.relay/config.toml) on this laptop")
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

	// 1. which instance. Profile and region come from config; only the
	// instance is asked, with the configured one first.
	if err := pickResetInstance(ctx, &cfg); err != nil {
		return err
	}
	target := cfg.Box.User + "@" + cfg.Box.Host

	// 2. confirm.
	if !yes {
		ui.Warn("This wipes " + target + " (" + cfg.AWS.InstanceID + "): units, harness logins, runtimes, clones and worktrees. Uncommitted work is lost.")
		typed, err := ui.Input("Type "+resetConfirmWord+" to confirm", resetConfirmWord)
		if err != nil {
			return err
		}
		if typed != resetConfirmWord {
			return errors.New("aborted")
		}
	}

	// 3. one step per phase. A failed phase shows its own log tail and the
	// rest still run, so a box is never left half reset.
	runner := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	steps := bootstrap.ResetSteps(runner)
	if local {
		// Only the laptop-side files: ~/.relay also holds the bridge token
		// when this Mac runs `relay serve`.
		paths := []string{filepath.Clean(config.ConfigPath()), filepath.Clean(sshx.ControlDir())}
		steps = append(steps, ui.Step{
			Name: "Removing the relay config on this laptop",
			Run: func(_ context.Context, log io.Writer) error {
				for _, p := range paths {
					if err := os.RemoveAll(p); err != nil {
						return err
					}
					fmt.Fprintf(log, "removed %s\n", p)
				}
				return nil
			},
		})
	}

	err = ui.RunSteps(ctx, "Resetting "+target, steps)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	ui.Info("Run `relay init` to set the box up again.")
	return nil
}

// pickResetInstance asks which instance to wipe, using the profile and region
// already in config and offering the configured instance first. A box that AWS
// cannot be asked about (no profile, or the call fails) keeps what config says.
func pickResetInstance(ctx context.Context, cfg *config.Config) error {
	if cfg.AWS.Profile == "" || cfg.AWS.Region == "" {
		return nil
	}

	prof := awsResetProfile(cfg.AWS.Profile)
	awsCfg, err := awsx.EnsureCreds(ctx, prof, cfg.AWS.Region)
	if err != nil {
		ui.Warn("Could not reach AWS (" + err.Error() + "); using the box in config.")
		return nil
	}

	var insts []awsx.Instance
	if err := ui.Spinner(ctx, "Listing running instances", func(ctx context.Context) error {
		var lerr error
		insts, lerr = awsx.ListInstances(ctx, awsCfg)
		return lerr
	}); err != nil {
		ui.Warn("Could not list instances (" + err.Error() + "); using the box in config.")
		return nil
	}
	if len(insts) == 0 {
		return fmt.Errorf("no running EC2 instances in %s (profile %s)", cfg.AWS.Region, cfg.AWS.Profile)
	}

	insts = orderForReset(insts, cfg.AWS.InstanceID)
	idx, err := ui.Select("Instance to reset", resetInstanceLabels(insts, cfg.AWS.InstanceID))
	if err != nil {
		return err
	}
	chosen := insts[idx]

	if chosen.ID != cfg.AWS.InstanceID {
		ui.Warn("That is not the box in your config; the configured ssh user and key will be used for it.")
	}
	cfg.AWS.InstanceID = chosen.ID
	if host := instanceHost(chosen); host != "" {
		cfg.Box.Host = host
	}
	return nil
}

// awsResetProfile looks the configured profile up so an SSO one can refresh its
// credentials. An unknown name is still usable as a plain profile.
func awsResetProfile(name string) awsx.Profile {
	profs, err := awsx.Profiles()
	if err == nil {
		for _, p := range profs {
			if p.Name == name {
				return p
			}
		}
	}
	return awsx.Profile{Name: name}
}

// orderForReset sorts instances by name and floats the configured one to the
// top so it is what Enter picks.
func orderForReset(insts []awsx.Instance, current string) []awsx.Instance {
	out := append([]awsx.Instance(nil), insts...)
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].ID == current) != (out[j].ID == current) {
			return out[i].ID == current
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// resetInstanceLabels renders one picker row per instance, marking the one in
// config.
func resetInstanceLabels(insts []awsx.Instance, current string) []string {
	labels := make([]string, len(insts))
	for i, in := range insts {
		name := in.Name
		if name == "" {
			name = "(unnamed)"
		}
		parts := []string{name, in.ID, in.Type}
		if host := instanceHost(in); host != "" && host != in.ID {
			parts = append(parts, host)
		}
		if in.ID == current {
			parts = append(parts, "· configured box")
		}
		labels[i] = strings.Join(parts, "  ")
	}
	return labels
}

// instanceHost is the address to ssh to: public DNS, then public IP.
func instanceHost(in awsx.Instance) string {
	if in.PublicDNS != "" {
		return in.PublicDNS
	}
	return in.PublicIP
}

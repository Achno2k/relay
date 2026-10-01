package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"relay/internal/config"
	"relay/internal/sshx"
	"relay/internal/ui"
)

func init() { Register(doctorCmd) }

// doctorCmd runs local dependency, config, and connectivity checks.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check local dependencies, config, the box and its bridge",
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
		{Name: "box disk", Run: func(context.Context) error {
			if err := boxConfigured(cfg, cfgErr); err != nil {
				return err
			}
			var out bytes.Buffer
			r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
			if err := r.Run(ctx, "df -k --output=avail,pcent / | tail -1", &out, io.Discard); err != nil {
				return fmt.Errorf("df: %w", err)
			}
			f := strings.Fields(out.String())
			if len(f) < 2 {
				return fmt.Errorf("unexpected df output %q", out.String())
			}
			availKB, _ := strconv.ParseInt(f[0], 10, 64)
			freeGB := float64(availKB) / (1024 * 1024)
			if freeGB < 2 {
				return fmt.Errorf("%.1fG free (%s used), grow the root volume", freeGB, f[1])
			}
			return nil
		}},
		{Name: "box tailscale", Run: func(context.Context) error {
			if err := boxConfigured(cfg, cfgErr); err != nil {
				return err
			}
			out, err := boxOutput(ctx, cfg, "if command -v tailscale >/dev/null; then tailscale status --json 2>/dev/null; else echo missing; fi")
			if err != nil {
				return err
			}
			return tailscaleOK(out)
		}},
		{Name: "box bridge", Run: func(context.Context) error {
			if err := boxConfigured(cfg, cfgErr); err != nil {
				return err
			}
			active, _ := boxOutput(ctx, cfg, "systemctl is-active relay.service 2>/dev/null || true")
			if s := strings.TrimSpace(active); s != "active" {
				if s == "" || (s == "inactive" && !boxHasUnit(ctx, cfg)) {
					return errors.New("relay.service is not installed; run `relay ssh relay pair`")
				}
				return fmt.Errorf("relay.service is %s; see `relay ssh journalctl -u relay.service`", s)
			}
			health, err := boxOutput(ctx, cfg, "curl -fsS --max-time 5 http://127.0.0.1:7878/health")
			if err != nil {
				return errors.New("relay.service is active but /health does not answer on port 7878")
			}
			_, herdr, err := parseHealth(health)
			if err != nil {
				return err
			}
			if herdr != "connected" {
				return fmt.Errorf("bridge is up but herdr is %s", herdr)
			}
			return nil
		}},
		{Name: "box version", Run: func(context.Context) error {
			if err := boxConfigured(cfg, cfgErr); err != nil {
				return err
			}
			bin, err := boxOutput(ctx, cfg, "/usr/local/bin/relay --version")
			if err != nil {
				return errors.New("no /usr/local/bin/relay on the box; run `relay deploy`")
			}
			// The running bridge can lag the binary until it restarts.
			running := ""
			if health, err := boxOutput(ctx, cfg, "curl -fsS --max-time 5 http://127.0.0.1:7878/health"); err == nil {
				running, _, _ = parseHealth(health)
			}
			return versionMatch(Version, strings.TrimSpace(bin), running)
		}},
	}

	failed, err := ui.RunChecks(ctx, "relay doctor", checks)
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

// boxConfigured reports why the box checks cannot run, or nil.
func boxConfigured(cfg config.Config, cfgErr error) error {
	if cfgErr != nil || (cfg.Box.Host == "" && cfg.AWS.InstanceID == "") {
		return errors.New("skipped, box not configured")
	}
	return nil
}

// boxOutput runs script on the box and returns its stdout.
func boxOutput(ctx context.Context, cfg config.Config, script string) (string, error) {
	var out bytes.Buffer
	r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	if err := r.Run(ctx, script, &out, io.Discard); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// boxHasUnit reports whether relay.service is installed on the box.
func boxHasUnit(ctx context.Context, cfg config.Config) bool {
	_, err := boxOutput(ctx, cfg, "systemctl cat relay.service >/dev/null 2>&1")
	return err == nil
}

// tailscaleOK reads `tailscale status --json` (or "missing") from the box.
func tailscaleOK(out string) error {
	out = strings.TrimSpace(out)
	if out == "missing" {
		return errors.New("tailscale is not installed; run `relay ssh relay pair`")
	}
	var st struct {
		BackendState string
		Self         struct{ TailscaleIPs []string }
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.BackendState == "" {
		return errors.New("tailscaled is not answering; see `relay ssh systemctl status tailscaled`")
	}
	if st.BackendState != "Running" {
		return fmt.Errorf("tailscale is %s; run `relay ssh relay pair` to log in", st.BackendState)
	}
	return nil
}

// parseHealth reads the bridge's /health body.
func parseHealth(body string) (version, herdr string, err error) {
	var h struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Herdr   string `json:"herdr"`
	}
	if err := json.Unmarshal([]byte(body), &h); err != nil || !h.OK {
		return "", "", errors.New("/health answered with something other than the bridge")
	}
	return h.Version, h.Herdr, nil
}

// versionMatch compares this binary's version with the box's binary and, when
// known, the bridge that is running there.
func versionMatch(local, box, running string) error {
	if box != local {
		return fmt.Errorf("box has relay %s, this is %s; run `relay deploy`", box, local)
	}
	if running != "" && running != box {
		return fmt.Errorf("bridge still runs %s; run `relay deploy` to restart it", running)
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

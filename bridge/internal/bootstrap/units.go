package bootstrap

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	"relay/internal/config"
	"relay/internal/sshx"
)

//go:embed scripts/herdr-server.service
var unitFS embed.FS

// Units are the systemd units bootstrap installs, in the order they start.
var Units = []string{"herdr-server.service"}

// Unit renders one embedded unit template for the given box user.
func Unit(name, user, home string) (string, error) {
	b, err := unitFS.ReadFile("scripts/" + name)
	if err != nil {
		return "", fmt.Errorf("no embedded unit %q: %w", name, err)
	}
	t, err := template.New(name).Parse(string(b))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, struct{ User, Home string }{user, home}); err != nil {
		return "", err
	}
	return out.String(), nil
}

// InstallUnits writes every unit to /etc/systemd/system and reloads systemd.
// It enables nothing.
func InstallUnits(ctx context.Context, r sshx.Runner, user, home string, log io.Writer) error {
	var script strings.Builder
	script.WriteString("set -e\n")
	for _, name := range Units {
		body, err := Unit(name, user, home)
		if err != nil {
			return err
		}
		fmt.Fprintf(&script, "sudo -n tee /etc/systemd/system/%s >/dev/null <<'RELAY_UNIT_EOF'\n%s\nRELAY_UNIT_EOF\n", name, strings.TrimRight(body, "\n"))
		fmt.Fprintf(&script, "echo 'wrote /etc/systemd/system/%s'\n", name)
	}
	script.WriteString("sudo -n systemctl daemon-reload\n")
	return r.Run(ctx, script.String(), log, log)
}

// EnableHerdrServer enables and starts the headless herdr server. `herdr server`
// stays in the foreground and needs no tty, so plain Type=simple works.
func EnableHerdrServer(ctx context.Context, r sshx.Runner, log io.Writer) error {
	script := "set -e\n" +
		"sudo -n systemctl enable --now herdr-server.service\n" +
		"sleep 1\n" +
		"systemctl is-active herdr-server.service\n" +
		"herdr status server || true\n"
	return r.Run(ctx, script, log, log)
}

// run executes a script and wraps any failure with what it was doing.
func run(ctx context.Context, r sshx.Runner, script, what string) error {
	var errBuf bytes.Buffer
	if err := r.Run(ctx, script, io.Discard, &errBuf); err != nil {
		if s := strings.TrimSpace(errBuf.String()); s != "" {
			return fmt.Errorf("%s: %w\n%s", what, err, s)
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// shellQuote wraps s in single quotes so it survives bash intact.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SyncConfig writes the parts of the laptop config the box needs to
// ~/.relay/config.toml on the box: harnesses, repos and work dir. AWS and ssh details are blanked so the box never tries to
// proxy commands back over ssh.
func SyncConfig(ctx context.Context, r sshx.Runner, home string, cfg config.Config) error {
	boxCfg := cfg
	boxCfg.AWS = config.AWS{}
	boxCfg.Box = config.Box{User: cfg.Box.User, WorkDir: cfg.Box.WorkDir}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(boxCfg); err != nil {
		return err
	}
	path := home + "/.relay/config.toml"
	script := "set -e\n" +
		"umask 077\n" +
		"mkdir -p " + shellQuote(home+"/.relay") + "\n" +
		"cat > " + shellQuote(path) + " <<'RELAY_CONFIG_EOF'\n" +
		buf.String() +
		"RELAY_CONFIG_EOF\n" +
		"chmod 0600 " + shellQuote(path) + "\n"
	return run(ctx, r, script, "write config.toml")
}

// RestartRelayIfActive restarts relay.service when it is running, so a
// freshly uploaded binary takes effect. A stopped unit is left alone.
func RestartRelayIfActive(ctx context.Context, r sshx.Runner) error {
	script := "if systemctl is-active --quiet relay.service; then sudo -n systemctl restart relay.service; fi\n"
	return r.Run(ctx, script, io.Discard, io.Discard)
}

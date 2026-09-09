package bootstrap

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/Achno2k/agents-cli/internal/sshx"
)

//go:embed scripts/herdr-server.service scripts/agents-bot.service
var unitFS embed.FS

// Units are the systemd units bootstrap installs, in the order they start.
var Units = []string{"herdr-server.service", "agents-bot.service"}

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
// It enables nothing: agents-bot only starts once slack.env exists.
func InstallUnits(ctx context.Context, r sshx.Runner, user, home string, log io.Writer) error {
	var script strings.Builder
	script.WriteString("set -e\n")
	for _, name := range Units {
		body, err := Unit(name, user, home)
		if err != nil {
			return err
		}
		fmt.Fprintf(&script, "sudo -n tee /etc/systemd/system/%s >/dev/null <<'AGENTS_UNIT_EOF'\n%s\nAGENTS_UNIT_EOF\n", name, strings.TrimRight(body, "\n"))
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

// SlackEnvPath is where the bot's tokens live on the box.
func SlackEnvPath(home string) string { return home + "/.agents/slack.env" }

// WriteSlackEnv writes the bot and app tokens to ~/.agents/slack.env with 0600
// permissions. Both tokens are required by the socket-mode bot.
func WriteSlackEnv(ctx context.Context, r sshx.Runner, home, botToken, appToken string) error {
	path := SlackEnvPath(home)
	script := "set -e\n" +
		"umask 077\n" +
		"mkdir -p " + shellQuote(home+"/.agents") + "\n" +
		"cat > " + shellQuote(path) + " <<'AGENTS_SLACK_EOF'\n" +
		"SLACK_BOT_TOKEN=" + strings.TrimSpace(botToken) + "\n" +
		"SLACK_APP_TOKEN=" + strings.TrimSpace(appToken) + "\n" +
		"AGENTS_SLACK_EOF\n" +
		"chmod 0600 " + shellQuote(path) + "\n"
	return run(ctx, r, script, "write slack.env")
}

// EnableBot enables and starts the Slack bot unit.
func EnableBot(ctx context.Context, r sshx.Runner, log io.Writer) error {
	script := "set -e\n" +
		"sudo -n systemctl enable --now agents-bot.service\n" +
		"sleep 1\n" +
		"systemctl is-active agents-bot.service\n"
	return r.Run(ctx, script, log, log)
}

// BotStatus returns systemctl's one-word state for the bot unit, e.g. "active"
// or "inactive". It never fails the caller: an unreachable box reads "unknown".
func BotStatus(ctx context.Context, r sshx.Runner) string {
	var out bytes.Buffer
	if err := r.Run(ctx, "systemctl is-active agents-bot.service || true", &out, io.Discard); err != nil {
		return "unknown"
	}
	s := strings.TrimSpace(out.String())
	if s == "" {
		return "unknown"
	}
	return s
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

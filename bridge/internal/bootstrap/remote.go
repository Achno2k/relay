package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"

	"relay/internal/config"
	"relay/internal/sshx"
)

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

// RestartRelay restarts relay.service so a freshly uploaded binary takes
// effect. A box without the unit is told to run `relay pair`, which installs it.
func RestartRelay(ctx context.Context, r sshx.Runner) error {
	script := "if ! systemctl cat relay.service >/dev/null 2>&1; then\n" +
		"  echo 'relay.service is not installed; run `relay pair` on the box' >&2; exit 1\n" +
		"fi\n" +
		"sudo -n systemctl restart relay.service\n"
	return run(ctx, r, script, "restart relay.service")
}

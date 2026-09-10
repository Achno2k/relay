package bootstrap

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Achno2k/agents-cli/internal/harness"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
)

// Login signs in to each named harness on the box and returns the names that
// ended up authenticated. Already-authenticated harnesses are skipped. For the
// rest the user is asked first and may skip; a failed or abandoned login is a
// warning, not a fatal error, so one missing account never blocks init.
// Login is interactive by design: the harness prints a URL, the user opens it
// locally and pastes the code back, so the command runs on the local tty
// through sshx.Interactive rather than as a captured script.
func Login(ctx context.Context, r sshx.Runner, names []string) ([]string, error) {
	var ok []string
	for _, name := range names {
		h, found := harness.Registry[strings.ToLower(name)]
		if !found {
			return ok, fmt.Errorf("unknown harness %q", name)
		}
		if IsLoggedIn(ctx, r, h) {
			ui.Success(h.Name() + " already signed in")
			ok = append(ok, h.Name())
			continue
		}
		yes, err := ui.Confirm("Sign in to "+h.Name()+" now? (No skips it and drops it from this box)", true)
		if err != nil {
			return ok, err
		}
		if !yes {
			ui.Warn(h.Name() + " skipped")
			continue
		}
		ui.Info("Signing in to " + h.Name() + ". Open the URL it prints and paste the code back here. Ctrl-C to skip.")
		if err := r.Interactive(ctx, InteractiveCmd(h.LoginCmd())); err != nil {
			if ctx.Err() != nil {
				return ok, ctx.Err()
			}
			ui.Warn(h.Name() + " login did not complete, skipping")
			continue
		}
		if !IsLoggedIn(ctx, r, h) {
			ui.Warn(h.Name() + " still reports signed out, skipping")
			continue
		}
		ui.Success(h.Name() + " signed in")
		ok = append(ok, h.Name())
	}
	if len(ok) == 0 {
		return ok, fmt.Errorf("no harness is signed in; at least one is needed")
	}
	return ok, nil
}

// IsLoggedIn reports whether the harness has valid auth on the box.
func IsLoggedIn(ctx context.Context, r sshx.Runner, h harness.Harness) bool {
	cmd := h.IsLoggedInCmd()
	if cmd == "" {
		return false
	}
	return r.Run(ctx, loginShell(cmd), io.Discard, io.Discard) == nil
}

// IsInstalled reports whether the harness binary is on the box's PATH.
func IsInstalled(ctx context.Context, r sshx.Runner, h harness.Harness) bool {
	cmd := h.IsInstalledCmd()
	if cmd == "" {
		return false
	}
	return r.Run(ctx, loginShell(cmd), io.Discard, io.Discard) == nil
}

// KnownHarnesses lists the registered harness names, sorted, for the picker.
func KnownHarnesses() []string {
	names := make([]string, 0, len(harness.Registry))
	for n := range harness.Registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// loginShell prefixes a command with the PATH bootstrap writes to ~/.profile.
// `ssh host bash -s` gets a non-login shell, so the mise shims are not on PATH
// unless we put them there.
func loginShell(cmd string) string {
	return `export AGENTS_ON_BOX=1 PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:/usr/local/bin:$PATH"` + "\n" + cmd + "\n"
}

// InteractiveCmd wraps a command for sshx.Interactive with the same PATH.
func InteractiveCmd(cmd string) string {
	return `export AGENTS_ON_BOX=1 PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:/usr/local/bin:$PATH"; ` + cmd
}

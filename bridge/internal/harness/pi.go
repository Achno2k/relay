package harness

// Install and login for pi (npm @earendil-works/pi-coding-agent). Verified
// against pi 0.99.x: there is no login subcommand, so the login runs the TUI
// and the user types /login there. Credentials land in ~/.pi/agent/auth.json.

// InstallScript installs pi globally with the mise-managed node.
func (Pi) InstallScript() string {
	return `npm install -g --no-fund --no-audit @earendil-works/pi-coding-agent && mise reshim >/dev/null 2>&1 || true`
}

// IsInstalledCmd exits 0 when the pi binary is on PATH.
func (Pi) IsInstalledCmd() string { return `command -v pi >/dev/null 2>&1` }

// LoginCmd opens pi's TUI. It needs a tty; LoginHint tells the user what to
// type there.
func (Pi) LoginCmd() string { return `pi` }

// LoginHint is shown before LoginCmd runs.
func (Pi) LoginHint() string {
	return "pi opens. Type /login, pick a provider and finish its sign-in, then /quit."
}

// IsLoggedInCmd exits 0 when auth.json holds at least one provider. `pi auth
// check` needs a provider name, and the user may have picked any of them.
func (Pi) IsLoggedInCmd() string {
	return `jq -e 'type == "object" and length > 0' "${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}/auth.json" >/dev/null 2>&1`
}

package harness

// Install and login for Claude Code. Verified against claude 2.1.x:
// `claude auth login` prints a URL and takes a pasted code, which is exactly
// what an ssh session can carry; `claude auth status --json` reports loggedIn.

// InstallScript installs Claude Code globally with the mise-managed node.
func (Claude) InstallScript() string {
	return `npm install -g --no-fund --no-audit @anthropic-ai/claude-code && mise reshim >/dev/null 2>&1 || true`
}

// IsInstalledCmd exits 0 when the claude binary is on PATH.
func (Claude) IsInstalledCmd() string { return `command -v claude >/dev/null 2>&1` }

// LoginCmd is the interactive OAuth login. It needs a tty: run it through
// sshx.Interactive so the user sees the URL and can paste the code back.
func (Claude) LoginCmd() string { return `claude auth login` }

// IsLoggedInCmd exits 0 only when auth is actually valid. `claude auth status`
// can exit 0 while logged out, so the JSON flag is checked directly.
func (Claude) IsLoggedInCmd() string {
	return `claude auth status --json 2>/dev/null | grep -q '"loggedIn"[[:space:]]*:[[:space:]]*true'`
}

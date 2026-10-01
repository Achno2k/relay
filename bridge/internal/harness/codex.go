package harness

// Install and login for the OpenAI Codex CLI. Verified against codex 0.153.x:
// `codex login` prints a URL for the ChatGPT sign-in; `codex login status`
// exits non-zero when there are no stored credentials.

// InstallScript installs Codex globally with the mise-managed node.
func (Codex) InstallScript() string {
	return `npm install -g --no-fund --no-audit @openai/codex && mise reshim >/dev/null 2>&1 || true`
}

// IsInstalledCmd exits 0 when the codex binary is on PATH.
func (Codex) IsInstalledCmd() string { return `command -v codex >/dev/null 2>&1` }

// LoginCmd is the interactive login. It needs a tty: run it through
// sshx.Interactive so the user sees the URL and can paste the code back.
func (Codex) LoginCmd() string { return `codex login --device-auth` }

// IsLoggedInCmd exits 0 when stored credentials are valid.
func (Codex) IsLoggedInCmd() string { return `codex login status >/dev/null 2>&1` }

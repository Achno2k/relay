// Package harness knows how to install, log in, and drive each coding agent
// headlessly. Owner: session "bootstrap" (install/login) and "env" (Headless).
package harness

import "context"

type Harness interface {
	Name() string // "claude" | "codex"
	// InstallScript returns bash that installs the harness on Ubuntu.
	InstallScript() string
	// IsInstalledCmd returns bash that exits 0 when installed.
	IsInstalledCmd() string
	// LoginCmd is the interactive login command to run over ssh with a tty.
	LoginCmd() string
	// IsLoggedInCmd returns bash that exits 0 when auth is valid.
	IsLoggedInCmd() string
	// HeadlessJSON runs one non-interactive prompt in dir and returns the
	// final assistant text. schema, when non-empty, is a JSON schema the
	// answer must satisfy.
	HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error)
}

var Registry = map[string]Harness{}

func Register(h Harness) { Registry[h.Name()] = h }

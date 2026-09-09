package harness

import (
	"context"
	"errors"
)

// Stub bodies. bootstrap session replaces install/login ones, env session
// replaces HeadlessJSON. Delete the stub you replace from this file.

var errTODO = errors.New("TODO harness")

func (Claude) InstallScript() string  { return "" }
func (Claude) IsInstalledCmd() string { return "false" }
func (Claude) LoginCmd() string       { return "" }
func (Claude) IsLoggedInCmd() string  { return "false" }
func (Claude) HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error) {
	return "", errTODO
}

func (Codex) InstallScript() string  { return "" }
func (Codex) IsInstalledCmd() string { return "false" }
func (Codex) LoginCmd() string       { return "" }
func (Codex) IsLoggedInCmd() string  { return "false" }
func (Codex) HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error) {
	return "", errTODO
}

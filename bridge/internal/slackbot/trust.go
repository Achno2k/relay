package slackbot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// trustWorktree records path as an accepted project in ~/.claude.json so
// Claude Code's "do you trust this folder" dialog does not block a session the
// bot starts. Every session is a fresh worktree, so without this the dialog
// would appear every time. Only the claude harness has this file.
func trustWorktree(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return trustWorktreeIn(filepath.Join(home, ".claude.json"), path)
}

func trustWorktreeIn(file, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	doc := map[string]any{}
	if b, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return errors.New("claude.json is not valid JSON, leaving it alone")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	projects, _ := doc["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	entry, _ := projects[abs].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["hasTrustDialogAccepted"] = true
	entry["hasCompletedProjectOnboarding"] = true
	projects[abs] = entry
	doc["projects"] = projects

	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

package slackbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustWorktreeInCreatesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".claude.json")
	wt := filepath.Join(dir, "work", "repo", "a1b2")

	// Missing file: created with just the project entry.
	if err := trustWorktreeIn(file, wt); err != nil {
		t.Fatal(err)
	}
	doc := readJSON(t, file)
	entry := doc["projects"].(map[string]any)[wt].(map[string]any)
	if entry["hasTrustDialogAccepted"] != true || entry["hasCompletedProjectOnboarding"] != true {
		t.Fatalf("entry not trusted: %v", entry)
	}

	// Existing file: other keys and other projects survive.
	seed := map[string]any{
		"hasCompletedOnboarding": true,
		"projects": map[string]any{
			"/other": map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []any{"Bash"}},
		},
	}
	b, _ := json.Marshal(seed)
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trustWorktreeIn(file, wt); err != nil {
		t.Fatal(err)
	}
	doc = readJSON(t, file)
	if doc["hasCompletedOnboarding"] != true {
		t.Fatal("top-level key lost")
	}
	projects := doc["projects"].(map[string]any)
	if projects["/other"].(map[string]any)["allowedTools"] == nil {
		t.Fatal("other project lost its keys")
	}
	if projects[wt].(map[string]any)["hasTrustDialogAccepted"] != true {
		t.Fatal("worktree not trusted")
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestTrustWorktreeInRefusesCorruptFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trustWorktreeIn(file, "/x"); err == nil {
		t.Fatal("expected an error on corrupt json")
	}
}

func readJSON(t *testing.T, file string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

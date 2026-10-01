package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMovesAgentsConfigOnce(t *testing.T) {
	base := t.TempDir()
	legacy, target := filepath.Join(base, ".agents"), filepath.Join(base, ".relay")
	os.MkdirAll(legacy, 0o700)
	os.WriteFile(filepath.Join(legacy, "config.toml"), []byte("harnesses = [\"claude\"]\n"), 0o600)

	if note := MigrateAgentsConfig(legacy, target); !strings.HasPrefix(note, "moved") {
		t.Fatalf("note = %q", note)
	}
	if read(t, filepath.Join(target, "config.toml")) != "harnesses = [\"claude\"]\n" {
		t.Fatal("config not moved")
	}
	if exists(filepath.Join(legacy, "config.toml")) {
		t.Fatal("legacy config still there")
	}
	if note := MigrateAgentsConfig(legacy, target); note != "" {
		t.Fatalf("second run: %q", note)
	}
}

func TestKeepsAnExistingRelayConfig(t *testing.T) {
	base := t.TempDir()
	legacy, target := filepath.Join(base, ".agents"), filepath.Join(base, ".relay")
	os.MkdirAll(legacy, 0o700)
	os.MkdirAll(target, 0o700)
	os.WriteFile(filepath.Join(legacy, "config.toml"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(target, "config.toml"), []byte("new"), 0o600)

	if note := MigrateAgentsConfig(legacy, target); note != "" {
		t.Fatalf("note = %q", note)
	}
	if read(t, filepath.Join(target, "config.toml")) != "new" {
		t.Fatal("relay config overwritten")
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	t.Setenv("RELAY_HOME", t.TempDir())
	if _, err := Load(); err != ErrNotInitialised {
		t.Fatalf("Load on empty home = %v", err)
	}
	c := Default()
	c.Harness = []string{"claude", "codex"}
	c.Repos["web"] = Repo{URL: "https://example.com/web.git", DefaultBranch: "main"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Harness, ",") != "claude,codex" || got.Repos["web"].DefaultBranch != "main" || got.Box.WorkDir != "~/work" {
		t.Fatalf("round trip = %+v", got)
	}
	if fi, err := os.Stat(ConfigPath()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config.toml mode = %v, %v", fi.Mode().Perm(), err)
	}
}

func TestOnBoxMarker(t *testing.T) {
	t.Setenv("RELAY_HOME", t.TempDir())
	t.Setenv("RELAY_ON_BOX", "")
	if OnBox() {
		t.Fatal("OnBox without marker")
	}
	os.WriteFile(filepath.Join(Home(), "on-box"), nil, 0o600)
	if !OnBox() {
		t.Fatal("OnBox ignores the marker")
	}
}

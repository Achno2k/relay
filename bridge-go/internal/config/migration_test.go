package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempDirs(t *testing.T) (legacy, target string) {
	t.Helper()
	base := t.TempDir()
	legacy = filepath.Join(base, ".herd")
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(legacy, "uploads/w1_p1"), 0o755))
	must(os.WriteFile(filepath.Join(legacy, "token"), []byte("tok\n"), 0o600))
	must(os.WriteFile(filepath.Join(legacy, "uploads/w1_p1/0123456789abcdef-a.png"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(legacy, "herd.log"), []byte("log"), 0o600))
	must(os.MkdirAll(filepath.Join(legacy, "e2e"), 0o755))
	return legacy, filepath.Join(base, ".relay")
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMovesTheWholeFolderOnce(t *testing.T) {
	legacy, target := tempDirs(t)
	if note := MigrateIfNeeded(legacy, target); !strings.HasPrefix(note, "moved") {
		t.Fatalf("note = %q", note)
	}
	if read(t, filepath.Join(target, "token")) != "tok\n" {
		t.Fatal("token not moved")
	}
	for _, p := range []string{"uploads/w1_p1/0123456789abcdef-a.png", "e2e"} {
		if !exists(filepath.Join(target, p)) {
			t.Fatalf("%s not moved", p)
		}
	}
	if exists(legacy) {
		t.Fatal("legacy still there")
	}
	if note := MigrateIfNeeded(legacy, target); note != "" {
		t.Fatalf("second run: %q", note)
	}
}

func TestMergesIntoAnUnusedRelayDir(t *testing.T) {
	legacy, target := tempDirs(t)
	// launchd created ~/.relay for its log before the first start.
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "relay.log"), []byte("new log"), 0o600)
	if note := MigrateIfNeeded(legacy, target); !strings.Contains(note, "token") {
		t.Fatalf("note = %q", note)
	}
	if read(t, filepath.Join(target, "token")) != "tok\n" || read(t, filepath.Join(target, "relay.log")) != "new log" {
		t.Fatal("wrong contents")
	}
	if exists(legacy) {
		t.Fatal("legacy still there")
	}
}

func TestLeavesAnInUseRelayDirAlone(t *testing.T) {
	legacy, target := tempDirs(t)
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "token"), []byte("current\n"), 0o600)
	if note := MigrateIfNeeded(legacy, target); note != "" {
		t.Fatalf("note = %q", note)
	}
	if read(t, filepath.Join(target, "token")) != "current\n" || !exists(legacy) {
		t.Fatal("touched an in-use dir")
	}
}

func TestDefaultTargetSkippedWithRelayHomeOverride(t *testing.T) {
	legacy, _ := tempDirs(t)
	t.Setenv("RELAY_HOME", t.TempDir())
	if note := MigrateIfNeeded(legacy, ""); note != "" || !exists(legacy) {
		t.Fatalf("migrated under RELAY_HOME: %q", note)
	}
}

func TestTokenIsCreatedOnceWith0600(t *testing.T) {
	t.Setenv("RELAY_HOME", filepath.Join(t.TempDir(), "home"))
	a, err := LoadToken()
	if err != nil || len(a) != 43 || strings.ContainsAny(a, "+/=") {
		t.Fatalf("token %q, %v", a, err)
	}
	st, _ := os.Stat(TokenPath())
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if b, _ := LoadToken(); b != a {
		t.Fatal("token changed on reload")
	}
	if c, _ := RotateToken(); c == a {
		t.Fatal("rotate kept the token")
	}
}

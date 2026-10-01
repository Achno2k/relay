package bootstrap

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustTasks(t *testing.T) {
	got := TrustTasks([]string{"claude", "pi", "codex"}, "~/work", "demo")
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want claude and codex: %+v", len(got), got)
	}
	if got[0].Func != `bs_trust_claude "$HOME"/'work/demo/main'` || got[1].Func != `bs_trust_codex "$HOME"/'work/demo/main'` {
		t.Fatalf("funcs = %q, %q", got[0].Func, got[1].Func)
	}
}

// runTasks runs each task's bootstrap.sh function twice with HOME=home and
// returns the combined output of both rounds.
func runTasks(t *testing.T, home string, tasks []Task) string {
	t.Helper()
	var all strings.Builder
	for round := 0; round < 2; round++ {
		for _, task := range tasks {
			cmd := exec.Command("bash", "-s")
			cmd.Stdin = strings.NewReader(Script(task.Func))
			cmd.Env = append(os.Environ(), "HOME="+home)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", task.Func, err, out)
			}
			all.Write(out)
		}
	}
	return all.String()
}

func needTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, bin := range tools {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not on PATH")
		}
	}
}

// The trust is written once, other keys survive, and a second run is a no-op.
func TestBsTrustKeepsOtherSettings(t *testing.T) {
	needTools(t, "bash", "jq", "python3")
	home := t.TempDir()
	repo := filepath.Join(home, "work", "demo", "main")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"hasCompletedOnboarding":true,"projects":{"/other":{"allowedTools":["x"]}}}`), 0o600)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("model = \"gpt-5\"\n\n[projects.\""+abs+"\"]\ntrust_level = \"untrusted\"\nother = 1\n"), 0o600)

	out := runTasks(t, home, TrustTasks([]string{"claude", "codex"}, "~/work", "demo"))
	for _, want := range []string{"claude trusts demo/main", "claude already trusts demo/main", "codex trusts demo/main", "codex already trusts demo/main"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	var claude struct {
		HasCompletedOnboarding bool
		Projects               map[string]map[string]any
	}
	b, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err := json.Unmarshal(b, &claude); err != nil {
		t.Fatal(err)
	}
	if !claude.HasCompletedOnboarding || claude.Projects["/other"]["allowedTools"] == nil {
		t.Errorf("other keys lost: %s", b)
	}
	if claude.Projects[abs]["hasTrustDialogAccepted"] != true {
		t.Errorf("repo not trusted: %s", b)
	}

	toml, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	want := "model = \"gpt-5\"\n\n[projects.\"" + abs + "\"]\ntrust_level = \"trusted\"\nother = 1\n"
	if string(toml) != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", toml, want)
	}
}

// A codex config without the project gets a new table at the end.
func TestBsTrustCodexAppendsTable(t *testing.T) {
	needTools(t, "bash", "python3")
	home := t.TempDir()
	repo := filepath.Join(home, "work", "demo", "main")
	os.MkdirAll(repo, 0o755)
	abs, _ := filepath.EvalSymlinks(repo)

	runTasks(t, home, TrustTasks([]string{"codex"}, "~/work", "demo"))
	toml, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if want := "[projects.\"" + abs + "\"]\ntrust_level = \"trusted\"\n"; string(toml) != want {
		t.Errorf("config.toml = %q, want %q", toml, want)
	}
}

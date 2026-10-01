package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceTaskQuotes(t *testing.T) {
	got := WorkspaceTask("~/work", "it's").Func
	want := `bs_workspace 'it'\''s' "$HOME"/'work/it'\''s/main'`
	if got != want {
		t.Fatalf("WorkspaceTask = %q, want %q", got, want)
	}
}

// bs_workspace runs against a fake herdr: it creates the workspace once and
// leaves it alone on the second run.
func TestBsWorkspaceIsIdempotent(t *testing.T) {
	for _, bin := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not on PATH")
		}
	}
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(filepath.Join(home, "work", "demo", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// The fake keeps its workspaces in a file and answers list in herdr's shape.
	fake := `#!/bin/sh
state="$HOME/workspaces"
case "$1 $2" in
"workspace list")
	labels=$(cat "$state" 2>/dev/null | sed 's/.*/{"label":"&"}/' | paste -sd, -)
	printf '{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[%s]}}\n' "$labels" ;;
"workspace create")
	shift 2
	while [ $# -gt 0 ]; do
		case "$1" in
		--label) echo "$2" >> "$state"; shift ;;
		--cwd) echo "$2" > "$HOME/cwd"; shift ;;
		esac
		shift
	done ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	task := WorkspaceTask("~/work", "demo")
	for i, want := range []string{"created herdr workspace demo", "already exists"} {
		cmd := exec.Command("bash", "-s")
		cmd.Stdin = strings.NewReader(Script(task.Func))
		cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %d: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(string(out), want) {
			t.Fatalf("run %d: output %q lacks %q", i+1, out, want)
		}
	}
	labels, _ := os.ReadFile(filepath.Join(home, "workspaces"))
	if string(labels) != "demo\n" {
		t.Fatalf("workspaces = %q, want one demo", labels)
	}
	cwd, _ := os.ReadFile(filepath.Join(home, "cwd"))
	if strings.TrimSpace(string(cwd)) != filepath.Join(home, "work", "demo", "main") {
		t.Fatalf("cwd = %q", cwd)
	}
}

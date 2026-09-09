package envplan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlanRoundTrip(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())

	if _, found, err := Load("acme/api"); err != nil || found {
		t.Fatalf("empty cache: found=%v err=%v", found, err)
	}

	want := Plan{
		Repo:      "acme/api",
		Generated: "claude",
		Steps: []Step{
			{Name: "Install Go", Run: "mise use -g go@1.25", Verify: "go version"},
			{Name: "System deps", Run: "sudo -n apt-get -y install libpq-dev", NeedsSudo: true},
		},
		Verify: []Check{{Name: "Go", Cmd: "go version"}},
	}
	if err := Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, found, err := Load("acme/api")
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestPlanPathSlugsRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	got := PlanPath("acme/api.git")
	want := filepath.Join(home, "plans", "acme-api.json")
	if got != want {
		t.Fatalf("PlanPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); err == nil {
		t.Fatal("PlanPath should not create directories")
	}
}

func TestSaveCreatesCacheDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	if err := Save(Plan{Repo: "api", Steps: []Step{{Name: "x", Run: "true"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	fi, err := os.Stat(PlanPath("api"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("plan file mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestParsePlanValidation(t *testing.T) {
	if _, err := parsePlan(`{"steps":[]}`); err == nil {
		t.Fatal("expected error for empty steps")
	}
	if _, err := parsePlan(`{"steps":[{"name":"x"}]}`); err == nil {
		t.Fatal("expected error for step without run")
	}
	if _, err := parsePlan(`not json`); err == nil {
		t.Fatal("expected error for non-JSON plan")
	}

	p, err := parsePlan(`{"steps":[{"run":"true"},{"name":"Deps","run":"go mod download","verify":"test -d vendor","needs_sudo":false}],"verify":[{"name":"Go","cmd":"go version"}]}`)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	if p.Steps[0].Name != "Step 1" {
		t.Fatalf("unnamed step should be numbered, got %q", p.Steps[0].Name)
	}
	if len(p.Verify) != 1 || p.Verify[0].Cmd != "go version" {
		t.Fatalf("verify list not parsed: %+v", p.Verify)
	}
}

func TestParseStepValidation(t *testing.T) {
	st, err := parseStep(`{"name":"Fix apt","run":"sudo -n apt-get -y update","verify":"true","needs_sudo":true}`)
	if err != nil {
		t.Fatalf("parseStep: %v", err)
	}
	if st.Run == "" || !st.NeedsSudo || st.Name != "Fix apt" {
		t.Fatalf("bad step: %+v", st)
	}
	if _, err := parseStep(`{"name":"Fix"}`); err == nil {
		t.Fatal("expected error for step without run")
	}
	if _, err := parseStep(`{`); err == nil {
		t.Fatal("expected error for non-JSON step")
	}

	unnamed, err := parseStep(`{"run":"true"}`)
	if err != nil {
		t.Fatalf("parseStep: %v", err)
	}
	if unnamed.Name == "" {
		t.Fatal("unnamed repaired step should get a name")
	}
}

func TestGeneratePromptContent(t *testing.T) {
	p := generatePrompt("/home/ubuntu/work/api")
	for _, want := range []string{
		"/home/ubuntu/work/api",
		"mise",
		"idempotent",
		"needs_sudo",
		"verify",
		"Ubuntu 24.04",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("generate prompt missing %q", want)
		}
	}
}

func TestRepairPromptContent(t *testing.T) {
	failed := Step{Name: "Install Go", Run: "mise use -g go@1.25", Verify: "go version"}
	lines := make([]string, 0, 101)
	for i := 0; i < 100; i++ {
		lines = append(lines, "noise")
	}
	lines = append(lines, "E: could not resolve host")

	p := repairPrompt(failed, strings.Join(lines, "\n"))
	for _, want := range []string{"Install Go", "mise use -g go@1.25", "go version", "E: could not resolve host"} {
		if !strings.Contains(p, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
	if n := strings.Count(p, "noise"); n > 40 {
		t.Errorf("stderr tail not trimmed: %d noise lines kept", n)
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("", 5); got != "(no output)" {
		t.Fatalf("empty tail = %q", got)
	}
	if got := tailLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("tail = %q", got)
	}
	if got := tailLines("a\nb", 10); got != "a\nb" {
		t.Fatalf("short tail = %q", got)
	}
}

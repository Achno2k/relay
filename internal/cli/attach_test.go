package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/Achno2k/agents-cli/internal/config"
)

func TestValidRef(t *testing.T) {
	for _, ok := range []string{"a3f2", "myrepo-a3f2", "repo.name_1", "Agent-42"} {
		if !validRef(ok) {
			t.Errorf("validRef(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a;rm -rf", "$(x)", "a\nb", "a`b`", "a|b"} {
		if validRef(bad) {
			t.Errorf("validRef(%q) = true, want false", bad)
		}
	}
}

func TestWhoami(t *testing.T) {
	w, err := whoami()
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if !strings.Contains(w, "@") || strings.HasPrefix(w, "@") || strings.HasSuffix(w, "@") {
		t.Errorf("whoami() = %q, want <user>@<host>", w)
	}
}

func TestHerdrRemoteArgs(t *testing.T) {
	t.Setenv("AGENTS_HERDR_SESSION", "")
	cfg := config.Config{}
	cfg.Box.Host = "ec2-1.example.com"
	cfg.Box.User = "ubuntu"
	got := herdrRemoteArgs(cfg)
	want := []string{"--remote", "ubuntu@ec2-1.example.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("herdrRemoteArgs = %v, want %v", got, want)
	}

	os.Setenv("AGENTS_HERDR_SESSION", "agents")
	defer os.Unsetenv("AGENTS_HERDR_SESSION")
	got = herdrRemoteArgs(cfg)
	want = []string{"--remote", "ubuntu@ec2-1.example.com", "--session", "agents"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("herdrRemoteArgs with session = %v, want %v", got, want)
	}

	cfg.Box.User = "" // bare host when no user saved
	got = herdrRemoteArgs(cfg)
	if got[1] != "ec2-1.example.com" {
		t.Errorf("herdrRemoteArgs without user = %v, want target %q", got, "ec2-1.example.com")
	}
}

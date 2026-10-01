package cli

import (
	"strings"
	"testing"

	"relay/internal/config"
)

func TestHerdrRemoteArgs(t *testing.T) {
	cfg := config.Config{}
	cfg.Box.Host = "ec2-1.example.com"
	cfg.Box.User = "ubuntu"
	got := herdrRemoteArgs(cfg)
	want := []string{"--remote", "ubuntu@ec2-1.example.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("herdrRemoteArgs = %v, want %v", got, want)
	}

	cfg.Box.User = "" // bare host when no user saved
	got = herdrRemoteArgs(cfg)
	if got[1] != "ec2-1.example.com" {
		t.Errorf("herdrRemoteArgs without user = %v, want target %q", got, "ec2-1.example.com")
	}
}

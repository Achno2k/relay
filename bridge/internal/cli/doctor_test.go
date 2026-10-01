package cli

import (
	"strings"
	"testing"
)

func TestTailscaleOK(t *testing.T) {
	cases := map[string]string{
		`{"BackendState":"Running","Self":{"TailscaleIPs":["100.64.0.1"]}}`: "",
		`{"BackendState":"NeedsLogin"}`:                                     "NeedsLogin",
		"missing":                                                           "not installed",
		"":                                                                  "not answering",
		"garbage":                                                           "not answering",
	}
	for in, want := range cases {
		err := tailscaleOK(in)
		switch {
		case want == "" && err != nil:
			t.Errorf("tailscaleOK(%q) = %v, want nil", in, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("tailscaleOK(%q) = %v, want an error with %q", in, err, want)
		}
	}
}

func TestParseHealth(t *testing.T) {
	v, h, err := parseHealth(`{"ok":true,"name":"relay","version":"0.2.0","herdr":"connected","uptimeSeconds":4}`)
	if err != nil || v != "0.2.0" || h != "connected" {
		t.Fatalf("parseHealth = %q %q %v", v, h, err)
	}
	for _, bad := range []string{"", "<html>", `{"ok":false}`} {
		if _, _, err := parseHealth(bad); err == nil {
			t.Errorf("parseHealth(%q) accepted it", bad)
		}
	}
}

func TestVersionMatch(t *testing.T) {
	if err := versionMatch("0.2.0", "0.2.0", "0.2.0"); err != nil {
		t.Errorf("all equal: %v", err)
	}
	if err := versionMatch("0.2.0", "0.2.0", ""); err != nil {
		t.Errorf("bridge down is the bridge check's job: %v", err)
	}
	if err := versionMatch("0.2.0", "0.1.0", "0.1.0"); err == nil || !strings.Contains(err.Error(), "relay deploy") {
		t.Errorf("old box binary: %v", err)
	}
	if err := versionMatch("0.2.0", "0.2.0", "0.1.0"); err == nil || !strings.Contains(err.Error(), "still runs 0.1.0") {
		t.Errorf("stale bridge: %v", err)
	}
}

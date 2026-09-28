package config

import (
	"strings"
	"testing"
)

func TestPairingURLEncodesQueryValues(t *testing.T) {
	got := PairingURL("100.64.0.1", 7878, "ab-c_d")
	if got != "relay://pair?url=http%3A%2F%2F100.64.0.1%3A7878&token=ab-c_d" {
		t.Fatal(got)
	}
}

func TestIsIPv4(t *testing.T) {
	for s, want := range map[string]bool{"100.64.0.1": true, "0.0.0.0": true, "256.1.1.1": false, "1.2.3": false, "a.b.c.d": false, "1..2.3": false, "fd7a::1": false, "+1.2.3.4": false} {
		if IsIPv4(s) != want {
			t.Errorf("%s", s)
		}
	}
}

func TestLaunchAgentPlist(t *testing.T) {
	t.Setenv("RELAY_HOME", "/tmp/rh")
	p := string(LaunchAgentPlist("/opt/relay/bin/relay", 7879))
	for _, want := range []string{
		"<string>com.relay.bridge</string>",
		"<string>/opt/relay/bin/relay</string>\n\t\t<string>serve</string>\n\t\t<string>--port</string>\n\t\t<string>7879</string>\n\t\t<string>--require-tailscale</string>",
		"<key>StandardOutPath</key>\n\t<string>/tmp/rh/relay.log</string>",
		"<key>KeepAlive</key>\n\t<true/>",
		"/opt/homebrew/bin:/usr/local/bin",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	t.Setenv("RELAY_HOME", "/tmp/rh")
	u := string(SystemdUnit("/home/dev/my bin/relay", 7878))
	for _, want := range []string{
		`ExecStart="/home/dev/my bin/relay" serve --port 7878 --require-tailscale`,
		"Restart=always",
		"StandardOutput=append:/tmp/rh/relay.log",
		"WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q\n%s", want, u)
		}
	}
}

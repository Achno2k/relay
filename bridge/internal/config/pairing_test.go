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

func TestParseBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://100.101.102.103:7878":     "http://100.101.102.103:7878",
		"http://vm.tail1234.ts.net:7878/": "http://vm.tail1234.ts.net:7878",
		"https://relay.example":           "https://relay.example",
	} {
		if got, err := ParseBaseURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "100.1.2.3:7878", "ftp://h", "http://", "http://h/x", "http://h?a=b", "http://u:p@h"} {
		if _, err := ParseBaseURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := PairingURLFor("http://h:7881", "t/k"); got != "relay://pair?url=http%3A%2F%2Fh%3A7881&token=t%2Fk" {
		t.Error(got)
	}
}

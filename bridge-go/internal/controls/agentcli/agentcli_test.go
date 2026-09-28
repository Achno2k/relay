package agentcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cancelling the context stops a running CLI at once (SIGTERM), so a shutting-down bridge never
// leaves a probe behind.
func TestCancelStopsTheChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out := Output(ctx, []string{"sleep", "30"}, "", time.Minute, false)
	if out != nil || time.Since(start) > 3*time.Second {
		t.Errorf("out %q after %v", out, time.Since(start))
	}
}

func TestTimeoutAndOutput(t *testing.T) {
	if out := Output(context.Background(), []string{"echo", "hi"}, "", 5*time.Second, true); strings.TrimSpace(string(out)) != "hi" {
		t.Errorf("out %q", out)
	}
	if out := Output(context.Background(), []string{"false"}, "", 5*time.Second, true); out != nil {
		t.Errorf("non-zero exit gave %q", out)
	}
	if out := Output(context.Background(), []string{"relay-no-such-cli"}, "", 5*time.Second, false); out != nil {
		t.Errorf("missing CLI gave %q", out)
	}
}

// User install dirs are searched even when the service manager's PATH lacks them (launchd,
// systemd), on both macOS and Linux.
func TestSearchPathFindsUserInstalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	bin := filepath.Join(home, ".local/bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "relay-fake-cli"), []byte("#!/bin/sh\necho ok\n"), 0o755)
	if p, ok := LookPath("relay-fake-cli"); !ok || p != filepath.Join(bin, "relay-fake-cli") {
		t.Errorf("LookPath %q %v", p, ok)
	}
	if out := Output(context.Background(), []string{"relay-fake-cli"}, "", 5*time.Second, true); strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("out %q", out)
	}
	sp := SearchPath()
	if !strings.HasPrefix(sp, "/opt/homebrew/bin:/usr/local/bin:") || !strings.Contains(sp, "/home/linuxbrew/.linuxbrew/bin") {
		t.Errorf("search path %s", sp)
	}
}

// R8-1: the probe returns at the first matching line instead of waiting for the CLI to exit.
func TestOutputUntilStopsAtTheLine(t *testing.T) {
	start := time.Now()
	out := OutputUntil(context.Background(), []string{"sh", "-c", "echo one; echo 'x usage_report y'; sleep 30"}, "", time.Minute,
		func(l []byte) bool { return strings.Contains(string(l), "usage_report") })
	if string(out) != "one\nx usage_report y\n" || time.Since(start) > 3*time.Second {
		t.Errorf("out %q after %v", out, time.Since(start))
	}
	// No match: everything up to EOF.
	if out := OutputUntil(context.Background(), []string{"sh", "-c", "echo a; echo b"}, "", time.Minute,
		func([]byte) bool { return false }); string(out) != "a\nb\n" {
		t.Errorf("out %q", out)
	}
}

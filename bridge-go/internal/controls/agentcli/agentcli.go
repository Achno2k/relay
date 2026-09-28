// Package agentcli runs the agents' own CLIs (`claude`, `codex`, `pi`) for model lists and usage.
// The bridge runs under launchd or systemd with a minimal PATH, so the usual install locations are
// added on both macOS and Linux.
package agentcli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SearchPath is the PATH the CLIs run with: Homebrew and /usr/local first (as the Swift bridge
// did), then the inherited PATH, then per-user install dirs (native claude, npm/bun globals,
// Linuxbrew) that a service manager's PATH never has.
func SearchPath() string {
	home, _ := os.UserHomeDir()
	var dirs []string
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	if p := os.Getenv("PATH"); p != "" {
		dirs = append(dirs, filepath.SplitList(p)...)
	} else {
		dirs = append(dirs, "/usr/bin", "/bin")
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local/bin"),
			filepath.Join(home, ".claude/local"),
			filepath.Join(home, ".npm-global/bin"),
			filepath.Join(home, ".bun/bin"),
			filepath.Join(home, ".volta/bin"),
			filepath.Join(home, "bin"),
		)
	}
	dirs = append(dirs, "/home/linuxbrew/.linuxbrew/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// Env is the process environment with PATH replaced by SearchPath.
func Env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+SearchPath())
}

// LookPath finds name on SearchPath (exec.LookPath only looks at the bridge's own PATH).
func LookPath(name string) (string, bool) {
	if strings.Contains(name, "/") {
		return name, true
	}
	for _, d := range filepath.SplitList(SearchPath()) {
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// Command builds a command for args[0] on SearchPath that gets SIGTERM when ctx ends and SIGKILL
// 2 s later, so a hung CLI is always reaped. Stdin and stderr are /dev/null.
func Command(ctx context.Context, dir string, extraEnv []string, args ...string) (*exec.Cmd, bool) {
	path, ok := LookPath(args[0])
	if !ok {
		return nil, false
	}
	cmd := exec.CommandContext(ctx, path, args[1:]...)
	cmd.Args[0] = args[0]
	cmd.Dir = dir
	cmd.Env = append(Env(), extraEnv...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	return cmd, true
}

// Output runs args with a timeout and returns stdout, or nil if it couldn't start or printed
// nothing. With requireSuccess, a non-zero exit is nil too. Cancelling ctx stops the child the
// same way the timeout does.
func Output(ctx context.Context, args []string, dir string, timeout time.Duration, requireSuccess bool, extraEnv ...string) []byte {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, ok := Command(ctx, dir, extraEnv, args...)
	if !ok {
		return nil
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if requireSuccess && err != nil {
		return nil
	}
	if out.Len() == 0 {
		return nil
	}
	return out.Bytes()
}

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Achno2k/agents-cli/internal/sshx"
)

// ModulePath is this module, used to find its source tree when cross compiling.
const ModulePath = "github.com/Achno2k/agents-cli"

// remoteUpload is where the binary lands before it is installed with sudo.
const remoteUpload = "/tmp/agents.upload"

// BuildForBox cross compiles this module for linux/<goarch> and returns the
// path of the binary. CGO is off; sqlite is modernc, so a static build is fine.
func BuildForBox(ctx context.Context, goarch string) (string, error) {
	if goarch == "" {
		goarch = "amd64"
	}
	dir, err := ModuleDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), "agents-linux-"+goarch)

	cmd := exec.CommandContext(ctx, "go", "build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", out,
		"./cmd/agents",
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")

	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cross compile linux/%s: %w\n%s", goarch, err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// InstallBinary uploads a locally built binary and installs it as
// /usr/local/bin/agents on the box.
func InstallBinary(ctx context.Context, r sshx.Runner, localPath string) error {
	if err := r.Copy(ctx, localPath, remoteUpload); err != nil {
		return fmt.Errorf("upload agents binary: %w", err)
	}
	script := "set -e\n" +
		"sudo -n install -m 0755 " + remoteUpload + " /usr/local/bin/agents\n" +
		"rm -f " + remoteUpload + "\n" +
		"/usr/local/bin/agents version || true\n"
	return run(ctx, r, script, "install agents binary")
}

// ErrNoModuleSource means the agents source tree is not reachable from here, so
// the box binary cannot be built.
var ErrNoModuleSource = errors.New("cannot find the agents-cli source tree; set AGENTS_SRC to its path")

// ModuleDir locates this module's source tree: $AGENTS_SRC first, then the
// working directory and the running binary's directory, walking up for a go.mod
// that declares ModulePath.
func ModuleDir() (string, error) {
	if src := os.Getenv("AGENTS_SRC"); src != "" {
		if ok, _ := isModuleRoot(src); ok {
			return src, nil
		}
		return "", fmt.Errorf("%w (AGENTS_SRC=%s is not it)", ErrNoModuleSource, src)
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		if dir, ok := walkUpToModule(start); ok {
			return dir, nil
		}
	}
	return "", ErrNoModuleSource
}

func walkUpToModule(start string) (string, bool) {
	dir := start
	for {
		if ok, _ := isModuleRoot(dir); ok {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isModuleRoot(dir string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false, err
	}
	return modulePathOf(string(b)) == ModulePath, nil
}

// modulePathOf returns the module path declared in go.mod contents.
func modulePathOf(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

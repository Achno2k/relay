package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"relay/internal/sshx"
)

// ModulePath is this module, used to find its source tree when cross compiling.
const ModulePath = "relay"

// remoteUpload is where the binary lands before it is installed with sudo.
const remoteUpload = "/tmp/relay.upload"

// BuildForBox cross compiles this module for linux/<goarch> and returns the
// path of the binary. CGO is off, so a static build is fine.
func BuildForBox(ctx context.Context, goarch string) (string, error) {
	if goarch == "" {
		goarch = "amd64"
	}
	dir, err := ModuleDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), "relay-linux-"+goarch)

	cmd := exec.CommandContext(ctx, "go", "build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", out,
		"./cmd/relay",
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")

	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cross compile linux/%s: %w\n%s", goarch, err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// InstallBinary uploads a locally built binary and installs it as
// /usr/local/bin/relay on the box.
func InstallBinary(ctx context.Context, r sshx.Runner, localPath string) error {
	same, err := boxHasBinary(ctx, r, localPath)
	if err == nil && same {
		return nil
	}
	if err := r.Copy(ctx, localPath, remoteUpload); err != nil {
		return fmt.Errorf("upload relay binary: %w", err)
	}
	script := "set -e\n" +
		"sudo -n install -m 0755 " + remoteUpload + " /usr/local/bin/relay\n" +
		"rm -f " + remoteUpload + "\n" +
		"/usr/local/bin/relay --version || true\n"
	return run(ctx, r, script, "install relay binary")
}

// boxHasBinary reports whether /usr/local/bin/relay on the box has the same
// sha256 as localPath, so an unchanged build is not uploaded again.
func boxHasBinary(ctx context.Context, r sshx.Runner, localPath string) (bool, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	local := hex.EncodeToString(h.Sum(nil))
	var out bytes.Buffer
	if err := r.Run(ctx, "sha256sum /usr/local/bin/relay 2>/dev/null | cut -d' ' -f1", &out, io.Discard); err != nil {
		return false, err
	}
	return strings.TrimSpace(out.String()) == local, nil
}

// ErrNoModuleSource means the relay source tree is not reachable from here, so
// the box binary cannot be built.
var ErrNoModuleSource = errors.New("cannot find the relay source tree (bridge/); set RELAY_SRC to its path")

// ModuleDir locates this module's source tree: $RELAY_SRC first, then the
// working directory and the running binary's directory, walking up for a go.mod
// that declares ModulePath.
// BuildSourceDir is the checkout this binary was built from, stamped by the
// Makefile with -ldflags "-X .../bootstrap.BuildSourceDir=<dir>". It lets
// `relay init` cross-compile the box binary from any working directory.
var BuildSourceDir string

func ModuleDir() (string, error) {
	if src := os.Getenv("RELAY_SRC"); src != "" {
		if ok, _ := isModuleRoot(src); ok {
			return src, nil
		}
		return "", fmt.Errorf("%w (RELAY_SRC=%s is not it)", ErrNoModuleSource, src)
	}
	if BuildSourceDir != "" {
		if ok, _ := isModuleRoot(BuildSourceDir); ok {
			return BuildSourceDir, nil
		}
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

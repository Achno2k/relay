// Package sshx runs commands on the box by shelling out to the system ssh.
// Owner: session "aws". SSM transport = ssh with a ProxyCommand.
package sshx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Achno2k/agents-cli/internal/config"
)

type Target struct {
	Host       string
	User       string
	Transport  string // ssh | ssm
	KeyPath    string
	InstanceID string
	Profile    string
	Region     string
}

type Runner interface {
	// Run executes bash on the box, streams stdout/stderr, returns exit error.
	Run(ctx context.Context, script string, stdout, stderr io.Writer) error
	// Interactive attaches the local tty to a remote command (login flows,
	// herdr --remote).
	Interactive(ctx context.Context, command string) error
	// Copy uploads a local file to a remote path.
	Copy(ctx context.Context, localPath, remotePath string) error
	// Args returns the ssh argv prefix for callers that must exec ssh themselves.
	Args() []string
	// Ping runs `true` on the box with a 10s timeout as a connectivity check.
	Ping(ctx context.Context) error
}

// PingTimeout bounds the connectivity check.
const PingTimeout = 10 * time.Second

// TargetFromConfig builds a Target from the saved config.
func TargetFromConfig(a config.AWS, b config.Box) Target {
	return normalize(Target{
		Host:       b.Host,
		User:       b.User,
		Transport:  b.Transport,
		KeyPath:    b.KeyPath,
		InstanceID: a.InstanceID,
		Profile:    a.Profile,
		Region:     a.Region,
	})
}

func New(t Target) Runner { return &runner{t: normalize(t)} }

type runner struct{ t Target }

func normalize(t Target) Target {
	if t.User == "" {
		t.User = "ubuntu"
	}
	if t.Transport == "" {
		t.Transport = "ssh"
	}
	return t
}

// hostOf is the name ssh connects to. Over SSM that must be the instance ID,
// because the ProxyCommand passes %h straight to `aws ssm start-session --target`.
func hostOf(t Target) string {
	if t.Transport == "ssm" && t.InstanceID != "" {
		return t.InstanceID
	}
	return t.Host
}

func destOf(t Target) string { return t.User + "@" + hostOf(t) }

// ControlDir holds the ssh multiplexing sockets.
func ControlDir() string { return filepath.Join(config.Dir(), "cm") }

// ControlPath is the multiplexing socket for a target. It is a short hash so
// the path stays under the unix socket length limit.
func ControlPath(t Target) string {
	t = normalize(t)
	sum := sha256.Sum256([]byte(destOf(t) + "/" + t.Transport))
	return filepath.Join(ControlDir(), hex.EncodeToString(sum[:])[:12])
}

// ProxyCommand is the SSM tunnel used as ssh's ProxyCommand.
func ProxyCommand(t Target) string {
	s := "aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p"
	if t.Profile != "" {
		s += " --profile " + t.Profile
	}
	if t.Region != "" {
		s += " --region " + t.Region
	}
	return s
}

// options are the -o flags shared by ssh and scp.
func options(t Target) []string {
	opts := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=30",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + ControlPath(t),
		"-o", "ControlPersist=300",
	}
	if t.KeyPath != "" {
		opts = append(opts, "-i", expandHome(t.KeyPath), "-o", "IdentitiesOnly=yes")
	}
	if t.Transport == "ssm" {
		opts = append(opts, "-o", "ProxyCommand="+ProxyCommand(t))
	}
	return opts
}

// sshArgv builds `ssh [extra] [options] user@host [command...]`.
func sshArgv(t Target, extra []string, command ...string) []string {
	argv := []string{"ssh"}
	argv = append(argv, extra...)
	argv = append(argv, options(t)...)
	argv = append(argv, destOf(t))
	argv = append(argv, command...)
	return argv
}

func (r *runner) Args() []string { return sshArgv(r.t, nil) }

func (r *runner) Run(ctx context.Context, script string, stdout, stderr io.Writer) error {
	return r.runWith(ctx, nil, script, stdout, stderr)
}

// runWith pipes a script to `bash -s` with extra ssh flags in front of the
// shared options.
func (r *runner) runWith(ctx context.Context, extra []string, script string, stdout, stderr io.Writer) error {
	if err := ensureControlDir(); err != nil {
		return err
	}
	argv := sshArgv(r.t, extra, "bash -s")
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = orDiscard(stdout)
	cmd.Stderr = orDiscard(stderr)
	return cmd.Run()
}

func (r *runner) Interactive(ctx context.Context, command string) error {
	if err := ensureControlDir(); err != nil {
		return err
	}
	argv := interactiveArgv(r.t, command)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func interactiveArgv(t Target, command string) []string {
	var cmdParts []string
	if strings.TrimSpace(command) != "" {
		cmdParts = []string{command}
	}
	return sshArgv(t, []string{"-t"}, cmdParts...)
}

func (r *runner) Copy(ctx context.Context, localPath, remotePath string) error {
	if err := ensureControlDir(); err != nil {
		return err
	}
	argv := scpArgv(r.t, localPath, remotePath)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Never inherit the terminal: scp's progress meter would draw under the
	// step runner's frame. Quiet mode plus captured stderr for the error.
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp %s: %w: %s", filepath.Base(localPath), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func scpArgv(t Target, localPath, remotePath string) []string {
	argv := []string{"scp", "-q"}
	argv = append(argv, options(t)...)
	argv = append(argv, localPath, destOf(t)+":"+remotePath)
	return argv
}

func (r *runner) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()
	return r.runWith(ctx, pingFlags, "true", io.Discard, io.Discard)
}

// pingFlags keep the connectivity check non-interactive, so a passphrase-only
// key fails immediately instead of sitting on a prompt until the timeout.
var pingFlags = []string{"-o", "BatchMode=yes"}

func ensureControlDir() error { return os.MkdirAll(ControlDir(), 0o700) }

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// expandHome resolves a leading ~/ so ssh gets a real path.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

package changes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"relay/internal/api"
)

// Timeout bounds every git call.
var Timeout = 10 * time.Second

// gitBin is the git executable; tests point it at a missing one.
var gitBin = "git"

var (
	errUnavailable = api.NewError(http.StatusServiceUnavailable, "unavailable", "git isn't installed on this machine")
	errTimeout     = api.NewError(http.StatusGatewayTimeout, "timeout", "git took too long")
	errFailed      = api.NewError(http.StatusInternalServerError, "internal", "git failed")
)

// exitError is a git call that ran and failed.
type exitError struct {
	code   int
	stderr string
}

func (e *exitError) Error() string { return "git exited " + strconv.Itoa(e.code) + ": " + e.stderr }

// APIError keeps git's stderr (it can hold full paths) off the wire.
func (e *exitError) APIError() *api.Error { return errFailed }

// git runs in one directory and never changes the repo: no optional locks, no prompts, no
// external diff or textconv, the user's colour and prefix settings overridden.
type git struct {
	ctx context.Context
	dir string
}

func (g git) command(ctx context.Context, args []string) *exec.Cmd {
	full := append([]string{"--no-optional-locks", "-c", "core.quotepath=off", "-c", "color.ui=never",
		"-c", "core.fsmonitor=false", "-c", "log.showSignature=false"}, args...)
	cmd := exec.CommandContext(ctx, gitBin, full...)
	cmd.Dir = g.dir
	cmd.Env = env()
	return cmd
}

// env is the bridge's environment minus GIT_* (a stray GIT_DIR would point every call elsewhere).
func env() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1",
		"GIT_PAGER=cat", "LC_ALL=C")
}

// out runs git and returns its stdout.
func (g git) out(args ...string) (string, error) {
	var stdout bytes.Buffer
	err := g.stream(func(r io.Reader) error {
		_, err := io.Copy(&stdout, r)
		return err
	}, args...)
	return stdout.String(), err
}

// stream runs git and hands its stdout to read while it runs.
func (g git) stream(read func(io.Reader) error, args ...string) error {
	ctx, cancel := context.WithTimeout(g.ctx, Timeout)
	defer cancel()
	cmd := g.command(ctx, args)
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, n: 4 << 10}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errFailed
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errUnavailable
		}
		if ctx.Err() == context.DeadlineExceeded {
			return errTimeout
		}
		return errFailed
	}
	rerr := read(bufio.NewReaderSize(stdout, 64<<10))
	if rerr != nil {
		// Drain so git isn't stuck on a full pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}
	werr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return errTimeout
	}
	if err := g.ctx.Err(); err != nil {
		return err
	}
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			return &exitError{code: ee.ExitCode(), stderr: strings.TrimSpace(stderr.String())}
		}
		return errFailed
	}
	return rerr
}

// exited reports whether err is git exiting with code.
func exited(err error, code int) bool {
	var e *exitError
	return errors.As(err, &e) && e.code == code
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.n -= k
		_, _ = l.w.Write(p[:k])
	}
	return len(p), nil
}

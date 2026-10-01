package config

import (
	"context"
	"os"
	"syscall"
	"time"
)

// LogRotator keeps the service log size-capped. launchd/systemd append `relay serve`'s
// stdout/stderr to LogPath() forever; this truncates the file in place (through the fds that
// point at it) once it passes MaxBytes.
type LogRotator struct {
	Path     string
	MaxBytes int64
	Interval time.Duration
	// FDs to truncate: stdout and stderr, since the service manager points both at Path.
	FDs []int
}

// NewLogRotator has the Swift defaults: LogPath(), 10 MB, every 5 minutes, fds 1 and 2.
func NewLogRotator() *LogRotator {
	return &LogRotator{Path: LogPath(), MaxBytes: 10 << 20, Interval: 5 * time.Minute, FDs: []int{1, 2}}
}

// Run checks now and then every Interval until ctx ends.
func (r *LogRotator) Run(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		r.RotateIfNeeded()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RotateIfNeeded truncates Path (and every fd pointing at it) to zero once it's over MaxBytes.
// It reports whether it rotated.
func (r *LogRotator) RotateIfNeeded() bool {
	st, err := os.Stat(r.Path)
	if err != nil || st.Size() <= r.MaxBytes {
		return false
	}
	for _, fd := range r.FDs {
		_ = syscall.Ftruncate(fd, 0)
		_, _ = syscall.Seek(fd, 0, 0)
	}
	return true
}

// RunLogRotator runs a rotator on path over stdout/stderr until ctx ends.
func RunLogRotator(ctx context.Context, path string, maxBytes int64, interval time.Duration) {
	(&LogRotator{Path: path, MaxBytes: maxBytes, Interval: interval, FDs: []int{1, 2}}).Run(ctx)
}

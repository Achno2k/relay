// Package config holds the bridge's on-disk state: `~/.relay` (RELAY_HOME), the token, the
// `~/.herd` migration, Tailscale lookup, pairing URL and service unit files.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// HomeOverride is RELAY_HOME when set and non-empty.
func HomeOverride() string { return os.Getenv("RELAY_HOME") }

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// Home is `~/.relay`, or RELAY_HOME. It holds the token, uploads and logs.
func Home() string {
	if h := HomeOverride(); h != "" {
		return h
	}
	return filepath.Join(userHome(), ".relay")
}

// LegacyHome is the data dir before the rename to Relay.
func LegacyHome() string { return filepath.Join(userHome(), ".herd") }

// LogPath is where launchd/systemd send `relay serve`'s output.
func LogPath() string { return filepath.Join(Home(), "relay.log") }

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// MigrateIfNeeded moves `~/.herd` to `~/.relay` once (token, uploads, logs, e2e), so paired
// phones keep their token. Only when target has no token yet, and never for the default target
// while RELAY_HOME is set. target "" means Home(). Returns a line for the log, or "".
func MigrateIfNeeded(legacy, target string) string {
	if target == "" {
		if HomeOverride() != "" {
			return ""
		}
		target = Home()
	}
	if !exists(legacy) {
		return ""
	}
	if !exists(target) {
		if err := os.Rename(legacy, target); err != nil {
			return fmt.Sprintf("could not move %s to %s: %v", legacy, target, err)
		}
		return fmt.Sprintf("moved %s to %s (token, uploads, logs kept)", legacy, target)
	}
	// target already exists (e.g. launchd created it for the log) but was never used: move the
	// old contents in, never overwriting, so the phone keeps its token.
	if exists(filepath.Join(target, "token")) || !exists(filepath.Join(legacy, "token")) {
		return ""
	}
	entries, err := os.ReadDir(legacy)
	if err != nil {
		return fmt.Sprintf("could not move %s to %s: %v", legacy, target, err)
	}
	var moved []string
	for _, e := range entries {
		dst := filepath.Join(target, e.Name())
		if exists(dst) {
			continue
		}
		if err := os.Rename(filepath.Join(legacy, e.Name()), dst); err != nil {
			return fmt.Sprintf("could not move %s to %s: %v", legacy, target, err)
		}
		moved = append(moved, e.Name())
	}
	if rest, err := os.ReadDir(legacy); err == nil && len(rest) == 0 {
		_ = os.Remove(legacy)
	}
	slices.Sort(moved)
	return fmt.Sprintf("moved %s from %s into %s", strings.Join(moved, ", "), legacy, target)
}

// EnsureHome creates Home() and makes it 0700 even if launchd/systemd created it first for the
// log (QA R8-21). An existing log is made 0600 too.
func EnsureHome() error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(Home(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(LogPath(), 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// TokenPath is `<home>/token`.
func TokenPath() string { return filepath.Join(Home(), "token") }

// LoadToken reads the token, creating one on first run (after moving an old `~/.herd` over).
func LoadToken() (string, error) {
	if note := MigrateIfNeeded(LegacyHome(), ""); note != "" {
		fmt.Println("relay: " + note)
	}
	// Missing, unreadable or empty: make a new one, as Swift does.
	if b, err := os.ReadFile(TokenPath()); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	return RotateToken()
}

// RotateToken writes a new token (0600) and returns it.
func RotateToken() (string, error) {
	if err := EnsureHome(); err != nil {
		return "", err
	}
	t, err := generateToken()
	if err != nil {
		return "", err
	}
	p := TokenPath()
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(t + "\n"); err != nil {
		f.Close()
		return "", err
	}
	return t, f.Close()
}

// generateToken is 32 random bytes, base64url without padding.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

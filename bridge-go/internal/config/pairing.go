package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PairingURL is `relay://pair?url=<base>&token=<token>` with the values fully percent-encoded
// (only A-Z a-z 0-9 - . _ ~ kept) so the app can split safely.
func PairingURL(host string, port int, token string) string {
	return "relay://pair?url=" + encode(fmt.Sprintf("http://%s:%d", host, port)) + "&token=" + encode(token)
}

func encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// tailscaleCandidates are checked after $PATH.
func tailscaleCandidates() []string {
	if runtime.GOOS == "linux" {
		return []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale"}
	}
	return []string{
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	}
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// TailscaleBinary finds tailscale on $PATH, then in the usual install locations.
func TailscaleBinary() string {
	var dirs []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		dirs = append(dirs, filepath.Join(d, "tailscale"))
	}
	for _, p := range append(dirs, tailscaleCandidates()...) {
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

// TailscaleIPv4 is `tailscale ip -4`, or "" when Tailscale isn't installed or running.
func TailscaleIPv4() string {
	bin := TailscaleBinary()
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "ip", "-4").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); IsIPv4(s) {
			return s
		}
	}
	return ""
}

// IsIPv4 is four dot-separated 0-255 decimals.
func IsIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return false
		}
		if n, err := strconv.Atoi(p); err != nil || n > 255 {
			return false
		}
	}
	return true
}

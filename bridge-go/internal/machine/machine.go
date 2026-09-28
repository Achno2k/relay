// Package machine describes the computer this bridge runs on (GET /machine).
package machine

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"relay/internal/api"
)

// Kind: an internal battery decides, since newer Macs report ids like `Mac17,2` for laptops
// too; the old `*Book*` model names are the fallback.
func Kind(model string, hasBattery bool) string {
	if hasBattery || strings.Contains(model, "Book") {
		return "laptop"
	}
	return "desktop"
}

// Current reads the machine once per call; callers cache it.
func Current() api.Machine { return current() }

// output runs a small system tool with a short timeout and returns trimmed stdout, or "".
func output(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Package machine describes the computer this bridge runs on (GET /machine).
package machine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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

// Test-only: a second bridge on the same computer sets these to show up as another machine.
const (
	EnvID   = "RELAY_MACHINE_ID"
	EnvName = "RELAY_MACHINE_NAME"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidID: machine ids never hold `|` or `/`, so the app can use them as key separators.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// CheckOverrides fails when RELAY_MACHINE_ID is set but not a valid id.
func CheckOverrides() error {
	if id := os.Getenv(EnvID); id != "" && !ValidID(id) {
		return fmt.Errorf("%s=%q must match [A-Za-z0-9._-]{1,64}", EnvID, id)
	}
	return nil
}

// Current reads the machine once per call; callers cache it. RELAY_MACHINE_ID and
// RELAY_MACHINE_NAME replace the id and name when set.
func Current() api.Machine {
	m := current()
	if id := os.Getenv(EnvID); id != "" {
		m.ID = id
	}
	if name := os.Getenv(EnvName); name != "" {
		m.Name = name
	}
	return m
}

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

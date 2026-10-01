package machine

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"relay/internal/api"
)

// Paths are variables so tests can point them at a fake root.
var (
	machineIDPath   = "/etc/machine-id"
	powerSupplyGlob = "/sys/class/power_supply/BAT*"
	productNamePath = "/sys/class/dmi/id/product_name"
	osReleasePath   = "/etc/os-release"
)

func current() api.Machine {
	id := readTrim(machineIDPath)
	if id == "" {
		id = hostname()
	}
	model := readTrim(productNamePath)
	if model == "" {
		model = unameMachine()
	}
	return api.Machine{ID: id, Name: hostname(), Kind: Kind(model, hasBattery()), Model: model, OS: prettyName(osReleasePath)}
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func hasBattery() bool {
	m, _ := filepath.Glob(powerSupplyGlob)
	return len(m) > 0
}

func unameMachine() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return "linux"
	}
	return unix.ByteSliceToString(u.Machine[:])
}

// prettyName is PRETTY_NAME from os-release, unquoted; "Linux" if missing.
func prettyName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "Linux"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && k == "PRETTY_NAME" {
			v = strings.Trim(v, `"'`)
			if v != "" {
				return v
			}
		}
	}
	return "Linux"
}

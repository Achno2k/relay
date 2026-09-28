package machine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxFromFakeRoot(t *testing.T) {
	root := t.TempDir()
	w := func(p, s string) string {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(s), 0o644)
		return full
	}
	machineIDPath = w("etc/machine-id", "0123456789abcdef0123456789abcdef\n")
	productNamePath = w("sys/class/dmi/id/product_name", "ThinkPad X1 Carbon\n")
	osReleasePath = w("etc/os-release", "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\n")
	w("sys/class/power_supply/BAT0/present", "1")
	powerSupplyGlob = filepath.Join(root, "sys/class/power_supply/BAT*")

	m := Current()
	if m.ID != "0123456789abcdef0123456789abcdef" || m.Model != "ThinkPad X1 Carbon" || m.OS != "Ubuntu 24.04.1 LTS" || m.Kind != "laptop" {
		t.Fatalf("%+v", m)
	}

	os.RemoveAll(filepath.Join(root, "sys"))
	osReleasePath = filepath.Join(root, "missing")
	m = Current()
	if m.Kind != "desktop" || m.Model == "" || m.OS != "Linux" {
		t.Fatalf("fallbacks: %+v", m)
	}
}

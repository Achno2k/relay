package setup

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HerdrManifestURL lists the latest herdr release, as bootstrap.sh's bs_herdr reads it.
const HerdrManifestURL = "https://herdr.dev/latest.json"

// HerdrRelease is one herdr build from the manifest.
type HerdrRelease struct {
	Version, URL, SHA256 string
}

// HerdrTarget is the manifest's asset key for a Go architecture.
func HerdrTarget(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "linux-x86_64", nil
	case "arm64":
		return "linux-aarch64", nil
	}
	return "", fmt.Errorf("no herdr release for linux/%s", goarch)
}

// ParseHerdrManifest picks the build for goarch out of herdr.dev/latest.json:
// {"version": "…", "assets": {target: url}, "sha256": {target: hex}}.
func ParseHerdrManifest(b []byte, goarch string) (HerdrRelease, error) {
	target, err := HerdrTarget(goarch)
	if err != nil {
		return HerdrRelease{}, err
	}
	var m struct {
		Version string            `json:"version"`
		Assets  map[string]string `json:"assets"`
		SHA256  map[string]string `json:"sha256"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return HerdrRelease{}, fmt.Errorf("herdr manifest: %w", err)
	}
	r := HerdrRelease{Version: m.Version, URL: m.Assets[target], SHA256: strings.ToLower(m.SHA256[target])}
	if !strings.HasPrefix(r.URL, "https://") || len(r.SHA256) != 64 {
		return HerdrRelease{}, fmt.Errorf("herdr manifest has no %s asset", target)
	}
	return r, nil
}

// TailscaleBackendState is BackendState from `tailscale status --json`, or ""
// when the output isn't that JSON (tailscaled not running).
func TailscaleBackendState(b []byte) string {
	var s struct{ BackendState string }
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.BackendState
}

// AuthURL finds the login link in a line `tailscale up` prints:
// "To authenticate, visit:" is followed by a tab-indented https URL.
func AuthURL(line string) string {
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, "https://") {
			return f
		}
	}
	return ""
}

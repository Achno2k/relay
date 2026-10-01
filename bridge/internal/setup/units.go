// Package setup turns a Linux machine into a Relay box for `relay pair`: herdr and
// its server, Tailscale and its login, and the relay.service system unit. Every
// step checks first, so running it again changes nothing.
package setup

import (
	"bytes"
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed units/*.service
var unitFS embed.FS

const (
	HerdrUnitName = "herdr-server.service"
	RelayUnitName = "relay.service"
	// UnitDir is where both system units live.
	UnitDir = "/etc/systemd/system"
	// DefaultHerdr is where pair installs herdr when it is missing.
	DefaultHerdr = "/usr/local/bin/herdr"
)

// UnitPath is the system unit file for name.
func UnitPath(name string) string { return filepath.Join(UnitDir, name) }

// UnitParams fill the unit templates. Both units run as User, never root's
// own services, so no linger is needed.
type UnitParams struct {
	User, Home string
	Relay      string // the relay binary
	Herdr      string // the herdr binary; DefaultHerdr when empty
	Port       int
	Socket     string // HERDR_SOCKET_PATH, only when the user set one
}

// servicePATH is the PATH both units give their process and so every agent CLI
// the bridge or herdr runs: mise shims and ~/.local/bin first, like bootstrap's profile.
func servicePATH(home string) string {
	return strings.Join([]string{
		home + "/.local/share/mise/shims", home + "/.local/bin", home + "/bin",
		"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
	}, ":")
}

// systemdQuote quotes one ExecStart word or Environment= assignment if it needs it.
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;$%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `$$`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

// Unit renders one embedded unit template.
func Unit(name string, p UnitParams) (string, error) {
	if p.User == "" || p.Home == "" {
		return "", fmt.Errorf("%s: user and home are required", name)
	}
	// StandardOutput=append: and WorkingDirectory= can't be quoted.
	if strings.ContainsAny(p.Home, " \t\n") {
		return "", fmt.Errorf("%s: home %q has whitespace, which systemd can't take here", name, p.Home)
	}
	if name == RelayUnitName && (p.Relay == "" || p.Port <= 0) {
		return "", fmt.Errorf("%s: relay binary and port are required", name)
	}
	herdr := p.Herdr
	if herdr == "" {
		herdr = DefaultHerdr
	}
	b, err := unitFS.ReadFile("units/" + name)
	if err != nil {
		return "", fmt.Errorf("no embedded unit %q: %w", name, err)
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(b))
	if err != nil {
		return "", err
	}
	data := map[string]any{
		"User":   p.User,
		"Home":   p.Home,
		"Path":   systemdQuote("PATH=" + servicePATH(p.Home)),
		"Herdr":  systemdQuote(herdr),
		"Relay":  systemdQuote(p.Relay),
		"Port":   p.Port,
		"Log":    filepath.Join(p.Home, ".relay", "relay.log"),
		"Socket": "",
	}
	if p.Socket != "" {
		data["Socket"] = systemdQuote("HERDR_SOCKET_PATH=" + p.Socket)
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

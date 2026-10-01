package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is `<home>/config.toml`: the laptop's box settings for `relay init` and friends, and
// a trimmed copy on the box (harnesses, repos, work dir) for `relay env`.
type Config struct {
	AWS     AWS             `toml:"aws"`
	Box     Box             `toml:"box"`
	Harness []string        `toml:"harnesses"` // "claude", "codex"
	Repos   map[string]Repo `toml:"repos"`     // key: short repo name
}

// AWS is the profile, region and instance `relay init` picked.
type AWS struct {
	Profile    string `toml:"profile"`
	Region     string `toml:"region"`
	InstanceID string `toml:"instance_id"`
}

// Box is how to reach the box and where repos live on it.
type Box struct {
	Host      string `toml:"host"`      // public DNS or IP
	User      string `toml:"user"`      // ubuntu
	Transport string `toml:"transport"` // "ssh" | "ssm"
	KeyPath   string `toml:"key_path"`  // for ssh transport
	WorkDir   string `toml:"work_dir"`  // default ~/work
}

// Repo is one cloned repo.
type Repo struct {
	URL           string `toml:"url"`
	DefaultBranch string `toml:"default_branch"`
}

// ConfigPath is `<home>/config.toml`.
func ConfigPath() string { return filepath.Join(Home(), "config.toml") }

// LegacyAgentsHome is agents-cli's data dir before it moved into relay.
func LegacyAgentsHome() string { return filepath.Join(userHome(), ".agents") }

// Default is the config before `relay init` fills it in.
func Default() Config {
	return Config{
		Box:   Box{User: "ubuntu", Transport: "ssh", WorkDir: "~/work"},
		Repos: map[string]Repo{},
	}
}

// ErrNotInitialised means there is no config.toml yet.
var ErrNotInitialised = errors.New("not initialised, run `relay init`")

// MigrateAgentsConfig moves agents-cli's `config.toml` from legacy into target once, when target
// has none. Never for the default target while RELAY_HOME is set. target "" means Home().
// Returns a line for the user, or "".
func MigrateAgentsConfig(legacy, target string) string {
	if target == "" {
		if HomeOverride() != "" {
			return ""
		}
		target = Home()
	}
	src, dst := filepath.Join(legacy, "config.toml"), filepath.Join(target, "config.toml")
	if !exists(src) || exists(dst) {
		return ""
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Sprintf("could not move %s to %s: %v", src, dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Sprintf("could not move %s to %s: %v", src, dst, err)
	}
	return fmt.Sprintf("moved %s to %s", src, dst)
}

// Load reads config.toml over Default, moving an old `~/.agents/config.toml` over first.
func Load() (Config, error) {
	if note := MigrateAgentsConfig(LegacyAgentsHome(), ""); note != "" {
		fmt.Fprintln(os.Stderr, "relay: "+note)
	}
	c := Default()
	b, err := os.ReadFile(ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return c, ErrNotInitialised
	}
	if err != nil {
		return c, err
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

// Save writes config.toml (0600).
func Save(c Config) error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(ConfigPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// OnBox reports whether this binary is running on the box rather than a laptop. bootstrap sets
// RELAY_ON_BOX=1 in the shell profile and drops a marker file; the marker covers non-login
// shells such as `ssh host cmd`.
func OnBox() bool {
	if os.Getenv("RELAY_ON_BOX") == "1" {
		return true
	}
	return exists(filepath.Join(Home(), "on-box"))
}

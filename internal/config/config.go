// Package config owns ~/.agents/config.toml on both laptop and box.
// Owner: lead. Other packages read via Load(); add fields here only with a
// matching default and a note in PLAN.md.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	AWS      AWS             `toml:"aws"`
	Box      Box             `toml:"box"`
	Harness  []string        `toml:"harnesses"` // "claude", "codex"
	Repos    map[string]Repo `toml:"repos"`     // key: short repo name
	Slack    Slack           `toml:"slack"`
	Sessions SessionPolicy   `toml:"sessions"`
}

type AWS struct {
	Profile    string `toml:"profile"`
	Region     string `toml:"region"`
	InstanceID string `toml:"instance_id"`
}

type Box struct {
	Host      string `toml:"host"`      // public DNS or IP
	User      string `toml:"user"`      // ubuntu
	Transport string `toml:"transport"` // "ssh" | "ssm"
	KeyPath   string `toml:"key_path"`  // for ssh transport
	WorkDir   string `toml:"work_dir"`  // default ~/work
}

type Repo struct {
	URL           string `toml:"url"`
	DefaultBranch string `toml:"default_branch"`
}

type Slack struct {
	DefaultRepoByChannel map[string]string `toml:"default_repo_by_channel"`
	AllowedUserIDs       []string          `toml:"allowed_user_ids"`
}

type SessionPolicy struct {
	IdleTTLHours int `toml:"idle_ttl_hours"` // default 24
	MaxLive      int `toml:"max_live"`       // default 5
}

func Dir() string {
	if d := os.Getenv("AGENTS_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

func Default() Config {
	return Config{
		Box:      Box{User: "ubuntu", Transport: "ssh", WorkDir: "~/work"},
		Repos:    map[string]Repo{},
		Slack:    Slack{DefaultRepoByChannel: map[string]string{}},
		Sessions: SessionPolicy{IdleTTLHours: 24, MaxLive: 5},
	}
}

var ErrNotInitialised = errors.New("not initialised, run `agents init`")

func Load() (Config, error) {
	c := Default()
	b, err := os.ReadFile(Path())
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

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// OnBox reports whether this binary is running on the EC2 box rather than a
// laptop. bootstrap sets AGENTS_ON_BOX=1 in the shell profile and drops a
// marker file; the marker covers non-login shells such as `ssh host cmd`.
func OnBox() bool {
	if os.Getenv("AGENTS_ON_BOX") == "1" {
		return true
	}
	_, err := os.Stat(filepath.Join(Dir(), "on-box"))
	return err == nil
}

package awsx

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profile is one AWS profile found in ~/.aws/config or ~/.aws/credentials.
type Profile struct {
	Name     string
	Region   string
	SSO      bool // profile resolves credentials through IAM Identity Center
	InConfig bool
	InCreds  bool
}

// ConfigPath is the path of the shared AWS config file.
func ConfigPath() string {
	if p := os.Getenv("AWS_CONFIG_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "config")
}

// CredentialsPath is the path of the shared AWS credentials file.
func CredentialsPath() string {
	if p := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "credentials")
}

// Profiles lists every profile from the shared config and credentials files,
// default first then alphabetical. A missing file is not an error.
func Profiles() ([]Profile, error) {
	cfg, err := readFile(ConfigPath())
	if err != nil {
		return nil, err
	}
	creds, err := readFile(CredentialsPath())
	if err != nil {
		return nil, err
	}
	return parseProfiles(cfg, creds), nil
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type iniSection struct {
	Name string
	Keys map[string]string
}

// parseINI reads an AWS-style ini file. Indented continuation keys are folded
// into the enclosing section, which is enough for the keys we care about.
func parseINI(r io.Reader) []iniSection {
	var out []iniSection
	var cur *iniSection
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			out = append(out, iniSection{Name: name, Keys: map[string]string{}})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		if key != "" {
			cur.Keys[key] = val
		}
	}
	return out
}

// parseProfiles merges the two file bodies into one profile list.
func parseProfiles(configSrc, credsSrc string) []Profile {
	byName := map[string]*Profile{}
	get := func(name string) *Profile {
		if p, ok := byName[name]; ok {
			return p
		}
		p := &Profile{Name: name}
		byName[name] = p
		return p
	}

	for _, s := range parseINI(strings.NewReader(configSrc)) {
		name, ok := configSectionProfile(s.Name)
		if !ok {
			continue
		}
		p := get(name)
		p.InConfig = true
		if r := s.Keys["region"]; r != "" {
			p.Region = r
		}
		if isSSOSection(s.Keys) {
			p.SSO = true
		}
	}
	for _, s := range parseINI(strings.NewReader(credsSrc)) {
		if s.Name == "" {
			continue
		}
		p := get(s.Name)
		p.InCreds = true
		if r := s.Keys["region"]; r != "" && p.Region == "" {
			p.Region = r
		}
		if isSSOSection(s.Keys) {
			p.SSO = true
		}
	}

	out := make([]Profile, 0, len(byName))
	for _, p := range byName {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == "default") != (out[j].Name == "default") {
			return out[i].Name == "default"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// configSectionProfile maps a config-file section header to a profile name.
// "default" and "profile foo" are profiles, "sso-session foo" and friends are not.
func configSectionProfile(section string) (string, bool) {
	s := strings.TrimSpace(section)
	if s == "default" {
		return s, true
	}
	if rest, ok := strings.CutPrefix(s, "profile "); ok {
		name := strings.TrimSpace(rest)
		return name, name != ""
	}
	return "", false
}

func isSSOSection(keys map[string]string) bool {
	for k := range keys {
		if strings.HasPrefix(k, "sso_") {
			return true
		}
	}
	return false
}

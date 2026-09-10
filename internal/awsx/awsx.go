// Package awsx picks the AWS profile, region and EC2 instance.
// Owner: session "aws".
package awsx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/ui"
)

// Instance is one running EC2 instance as shown in the picker.
type Instance struct {
	ID        string
	Name      string
	Type      string
	PublicIP  string
	PublicDNS string
	Arch      string // x86_64 | arm64
	State     string
	SSMReady  bool
}

// PickTarget runs the full interactive flow: profile → creds → region →
// instance → transport. Returns the values to save in config.
func PickTarget(ctx context.Context) (config.AWS, config.Box, error) {
	var a config.AWS
	var b config.Box

	prof, err := pickProfile(ctx)
	if err != nil {
		return a, b, err
	}
	region, err := pickRegion(prof)
	if err != nil {
		return a, b, err
	}

	cfg, err := EnsureCreds(ctx, prof, region)
	if err != nil {
		return a, b, err
	}

	var insts []instanceInfo
	err = ui.Spinner(ctx, "Listing running instances", func(ctx context.Context) error {
		var lerr error
		insts, lerr = listInstances(ctx, cfg)
		return lerr
	})
	if err != nil {
		return a, b, err
	}
	if len(insts) == 0 {
		return a, b, fmt.Errorf("no running EC2 instances in %s (profile %s)", region, prof.Name)
	}
	sort.Slice(insts, func(i, j int) bool { return instanceLess(insts[i], insts[j]) })

	idx, err := ui.Select("Instance", instanceLabels(insts))
	if err != nil {
		return a, b, err
	}
	inst := insts[idx]

	transport, err := pickTransport(inst)
	if err != nil {
		return a, b, err
	}

	keyPath, err := askKeyPath(inst.KeyName)
	if err != nil {
		return a, b, err
	}

	host := inst.PublicDNS
	if host == "" {
		host = inst.PublicIP
	}
	if host == "" {
		host = inst.ID
	}
	if transport == "ssh" && inst.PublicDNS == "" && inst.PublicIP == "" {
		ui.Warn("instance has no public address, ssh will only work from inside the VPC")
	}

	a = config.AWS{Profile: prof.Name, Region: region, InstanceID: inst.ID}
	b = config.Box{
		Host:      host,
		User:      boxUser,
		Transport: transport,
		KeyPath:   keyPath,
		WorkDir:   "~/work",
	}
	return a, b, nil
}

// boxUser is the login user on the box. PLAN pins us to Ubuntu 24.04 images,
// so there is nothing to ask.
const boxUser = "ubuntu"

// InstanceArch returns the CPU arch of the configured instance ("amd64"|"arm64"
// in GOARCH terms) so the laptop can cross-compile the box binary.
func InstanceArch(ctx context.Context, a config.AWS) (string, error) {
	if a.InstanceID == "" {
		return "", errors.New("no instance id in config")
	}
	cfg, err := LoadConfig(ctx, a.Profile, a.Region)
	if err != nil {
		return "", err
	}
	inst, err := describeInstance(ctx, cfg, a.InstanceID)
	if err != nil {
		return "", err
	}
	return normalizeArch(string(inst.Architecture)), nil
}

func pickProfile(ctx context.Context) (Profile, error) {
	profs, err := Profiles()
	if err != nil {
		return Profile{}, err
	}
	if len(profs) == 0 {
		if err := configureFirstProfile(ctx); err != nil {
			return Profile{}, err
		}
		if profs, err = Profiles(); err != nil {
			return Profile{}, err
		}
		if len(profs) == 0 {
			return Profile{}, errors.New("still no AWS profiles in ~/.aws/config or ~/.aws/credentials")
		}
	}
	env := os.Getenv("AWS_PROFILE")
	profs = preselect(profs, env)
	if len(profs) == 1 {
		return profs[0], nil
	}
	labels := make([]string, len(profs))
	for i, p := range profs {
		labels[i] = profileLabel(p)
		if i == 0 && env != "" && p.Name == env {
			labels[i] += "  (AWS_PROFILE)"
		}
	}
	i, err := ui.Select("AWS profile", labels)
	if err != nil {
		return Profile{}, err
	}
	return profs[i], nil
}

// preselect moves the profile named by AWS_PROFILE to the front so the picker
// opens on it. An empty or unknown name leaves the order untouched.
func preselect(profs []Profile, name string) []Profile {
	name = strings.TrimSpace(name)
	if name == "" {
		return profs
	}
	for i, p := range profs {
		if p.Name != name {
			continue
		}
		out := make([]Profile, 0, len(profs))
		out = append(out, p)
		out = append(out, profs[:i]...)
		out = append(out, profs[i+1:]...)
		return out
	}
	return profs
}

// signInMethods are the ways to create a first AWS profile.
var signInMethods = []struct {
	Label string
	Args  []string
}{
	{"SSO (IAM Identity Center)", []string{"configure", "sso"}},
	{"Access key and secret", []string{"configure"}},
}

// configureFirstProfile runs `aws configure` (or `aws configure sso`) on the
// user's terminal when no profile exists yet. The caller re-scans afterwards.
func configureFirstProfile(ctx context.Context) error {
	if err := requireAWSCLI(); err != nil {
		return err
	}
	labels := make([]string, len(signInMethods))
	for i, m := range signInMethods {
		labels[i] = m.Label
	}
	i, err := ui.Select("No AWS profiles found. How do you sign in?", labels)
	if err != nil {
		return err
	}
	return runAWS(ctx, signInMethods[i].Args...)
}

func profileLabel(p Profile) string {
	var extra []string
	if p.SSO {
		extra = append(extra, "sso")
	}
	if p.Region != "" {
		extra = append(extra, p.Region)
	}
	if len(extra) == 0 {
		return p.Name
	}
	return fmt.Sprintf("%-24s %s", p.Name, strings.Join(extra, "  "))
}

// commonRegions are offered when the profile has no region of its own.
var commonRegions = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
	"eu-west-1", "eu-west-2", "eu-central-1",
	"ap-south-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
}

// regionOptions puts the profile's own region first, then the common ones.
func regionOptions(profileRegion string) []string {
	out := []string{}
	seen := map[string]bool{}
	if profileRegion != "" {
		out = append(out, profileRegion)
		seen[profileRegion] = true
	}
	for _, r := range commonRegions {
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	return out
}

const otherRegion = "other…"

func pickRegion(p Profile) (string, error) {
	opts := append(regionOptions(p.Region), otherRegion)
	i, err := ui.Select("Region", opts)
	if err != nil {
		return "", err
	}
	if opts[i] != otherRegion {
		return opts[i], nil
	}
	r, err := ui.Input("Region", "us-east-1")
	if err != nil {
		return "", err
	}
	r = strings.TrimSpace(r)
	if r == "" {
		return "", errors.New("no region given")
	}
	return r, nil
}

func instanceLess(a, b instanceInfo) bool {
	if a.Name != b.Name {
		if a.Name == "" || b.Name == "" {
			return a.Name != ""
		}
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// instanceLabels renders aligned picker rows for the instance list.
func instanceLabels(insts []instanceInfo) []string {
	out := make([]string, len(insts))
	for i, in := range insts {
		name := in.Name
		if name == "" {
			name = "(no Name tag)"
		}
		addr := in.PublicDNS
		if addr == "" {
			addr = in.PublicIP
		}
		if addr == "" {
			addr = "no public address"
		}
		ssm := ""
		if in.SSMReady {
			ssm = "  ssm"
		}
		out[i] = fmt.Sprintf("%-24s %-20s %-13s %-8s %s%s",
			trunc(name, 24), in.ID, in.Type, in.Arch, addr, ssm)
	}
	return out
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// pickTransport offers ssm only when the instance is SSM managed and the
// session-manager-plugin is installed.
func pickTransport(in instanceInfo) (string, error) {
	opts := []string{"ssh"}
	if in.SSMReady && hasSessionManagerPlugin() {
		opts = append(opts, "ssm")
	}
	if len(opts) == 1 {
		if in.SSMReady && !hasSessionManagerPlugin() {
			ui.Warn("instance is SSM managed but session-manager-plugin is not on PATH, using ssh")
		}
		return "ssh", nil
	}
	i, err := ui.Select("Transport", opts)
	if err != nil {
		return "", err
	}
	return opts[i], nil
}

// askKeyPath asks for the private key, defaulting to a sensible file in ~/.ssh.
func askKeyPath(keyName string) (string, error) {
	def := DefaultKeyPath(keyName)
	p, err := ui.Input("SSH private key", def)
	if err != nil {
		return "", err
	}
	p = strings.TrimSpace(p)
	if p == "" {
		p = def
	}
	return p, nil
}

// DefaultKeyPath guesses the private key for an EC2 key pair name by looking
// in ~/.ssh: <keyName>.pem, <keyName>, then the usual identity files, then any
// single .pem in the directory.
func DefaultKeyPath(keyName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".ssh")
	var candidates []string
	if keyName != "" {
		candidates = append(candidates, keyName+".pem", keyName)
	}
	candidates = append(candidates, "id_ed25519", "id_rsa")
	for _, c := range candidates {
		p := filepath.Join(dir, c)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if pems, _ := filepath.Glob(filepath.Join(dir, "*.pem")); len(pems) > 0 {
		sort.Strings(pems)
		return pems[0]
	}
	return filepath.Join(dir, "id_ed25519")
}

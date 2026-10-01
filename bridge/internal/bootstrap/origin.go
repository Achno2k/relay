package bootstrap

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"strings"
)

var (
	scpLike = regexp.MustCompile(`^(?:([^@]+)@)?([^:/]+):(.+?)(?:\.git)?/?$`)
	sshURL  = regexp.MustCompile(`^ssh://(?:([^@]+)@)?([^/:]+)(?::\d+)?/(.+?)(?:\.git)?/?$`)
	httpURL = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:\.git)?/?$`)
)

// NormalizeOrigin rewrites a laptop-local origin URL into one the box can use.
// Local ssh aliases (Host github-personal in ~/.ssh/config) are resolved to
// their real hostname with `ssh -G`. GitHub remotes are always returned as
// https so the box can authenticate with gh's credential helper instead of
// needing an ssh key of its own. Anything unrecognised is returned unchanged.
func NormalizeOrigin(ctx context.Context, url string) string {
	user, host, path := splitRemote(url)
	if host == "" {
		return url
	}
	host = resolveSSHHost(ctx, host)
	if strings.EqualFold(host, "github.com") {
		return "https://github.com/" + path + ".git"
	}
	if strings.HasPrefix(url, "http") {
		return "https://" + host + "/" + path + ".git"
	}
	if user == "" {
		user = "git"
	}
	return user + "@" + host + ":" + path + ".git"
}

func splitRemote(url string) (user, host, path string) {
	if m := httpURL.FindStringSubmatch(url); m != nil {
		return "", m[1], m[2]
	}
	if m := sshURL.FindStringSubmatch(url); m != nil {
		return m[1], m[2], m[3]
	}
	if m := scpLike.FindStringSubmatch(url); m != nil {
		return m[1], m[2], m[3]
	}
	return "", "", ""
}

// resolveSSHHost asks the local ssh client what hostname an alias maps to.
// A plain hostname resolves to itself; failures fall back to the input.
func resolveSSHHost(ctx context.Context, alias string) string {
	out, err := exec.CommandContext(ctx, "ssh", "-G", alias).Output()
	if err != nil {
		return alias
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "hostname" {
			return f[1]
		}
	}
	return alias
}

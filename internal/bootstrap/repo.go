package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strings"

	"github.com/Achno2k/agents-cli/internal/sshx"
)

// Origin describes the repo `agents init` was run inside.
type Origin struct {
	Name          string // short name, "agents-cli"
	URL           string // remote url as git reports it
	DefaultBranch string // "main"
}

// DetectOrigin reads the origin remote of the git repo at dir.
func DetectOrigin(ctx context.Context, dir string) (Origin, error) {
	url, err := gitOut(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return Origin{}, fmt.Errorf("no origin remote in %s: %w", dir, err)
	}
	o := Origin{URL: NormalizeOrigin(ctx, url), Name: RepoName(url), DefaultBranch: "main"}
	if ref, err := gitOut(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimPrefix(ref, "origin/"); b != "" {
			o.DefaultBranch = b
		}
	}
	return o, nil
}

// RepoName is the short name of a repo url: the last path element, minus .git.
func RepoName(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// CheckoutDir is where a repo's primary checkout lives on the box:
// <workdir>/<repo>/main.
func CheckoutDir(workDir, repo string) string {
	return path.Join(ExpandHome(workDir), repo, "main")
}

// ExpandHome rewrites a leading ~ as $HOME so the path survives a bash heredoc.
func ExpandHome(p string) string {
	switch {
	case p == "~":
		return "$HOME"
	case strings.HasPrefix(p, "~/"):
		return "$HOME/" + strings.TrimPrefix(p, "~/")
	case p == "":
		return "$HOME/work"
	default:
		return p
	}
}

// Clone clones the repo on the box, or fetches it when it is already there.
func Clone(ctx context.Context, r sshx.Runner, workDir string, o Origin, log io.Writer) error {
	dir := CheckoutDir(workDir, o.Name)
	script := "set -e\n" +
		`export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:/usr/local/bin:$PATH"` + "\n" +
		"dir=" + shellQuote(dir) + "\n" +
		"gh auth setup-git >/dev/null 2>&1 || true\n" +
		"mkdir -p \"$(dirname \"$dir\")\"\n" +
		"if [ -d \"$dir/.git\" ]; then\n" +
		"  echo \"already cloned: $dir\"\n" +
		"  git -C \"$dir\" fetch --all --prune\n" +
		"else\n" +
		"  git clone " + shellQuote(o.URL) + " \"$dir\"\n" +
		"fi\n" +
		"git -C \"$dir\" rev-parse --abbrev-ref HEAD\n"
	return r.Run(ctx, script, log, log)
}

// GitHubReady reports whether gh on the box is authenticated, which is what a
// private clone needs.
func GitHubReady(ctx context.Context, r sshx.Runner) bool {
	return r.Run(ctx, loginShell("gh auth status >/dev/null 2>&1"), io.Discard, io.Discard) == nil
}

// GitHubLogin runs `gh auth login` on the box against the user's tty and wires
// git to use gh's credentials. This grants the box the user's whole GitHub
// account; prefer GitHubTokenLogin with a fine-grained token.
func GitHubLogin(ctx context.Context, r sshx.Runner) error {
	return r.Interactive(ctx, InteractiveCmd("gh auth login && gh auth setup-git"))
}

// TokenURL is where a fine-grained personal access token is created.
const TokenURL = "https://github.com/settings/personal-access-tokens/new"

// TokenScopes is what such a token has to grant for the box to clone, push and
// open pull requests.
var TokenScopes = []string{
	"Repository access: all repositories, or just the ones the box will work on",
	"Permissions → Contents: read and write",
	"Permissions → Pull requests: read and write",
	"Permissions → Metadata: read",
}

// ghTokenDelim ends the heredoc that carries the token to the box.
const ghTokenDelim = "AGENTS_GH_TOKEN_EOF"

// GitHubTokenLogin authenticates gh on the box with a fine-grained personal
// access token and wires git to use it. The token travels inside a quoted
// heredoc, never as an argument, so it stays out of the box's process list and
// out of any shell history.
func GitHubTokenLogin(ctx context.Context, r sshx.Runner, token string) error {
	script, err := githubTokenScript(token)
	if err != nil {
		return err
	}
	return run(ctx, r, script, "gh auth login --with-token")
}

// githubTokenScript builds that script. Split out so a test can prove the token
// only ever appears on its own line inside the heredoc.
func githubTokenScript(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("empty GitHub token")
	}
	if strings.ContainsAny(token, "\n\r") {
		return "", errors.New("GitHub token must be a single line")
	}
	return "set -e\n" +
		`export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:/usr/local/bin:$PATH"` + "\n" +
		"gh auth login --with-token <<'" + ghTokenDelim + "'\n" +
		token + "\n" +
		ghTokenDelim + "\n" +
		"gh auth setup-git\n" +
		"gh auth status\n", nil
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

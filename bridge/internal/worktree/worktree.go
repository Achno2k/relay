// Package worktree manages one git worktree per session under
// <workdir>/<repo>/<id>. Owner: session "core".
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BranchPrefix is the namespace every session branch lives under.
const BranchPrefix = "agents/"

// ErrDirty is returned by Remove when the worktree has uncommitted work and
// force is false. Dirty worktrees are never deleted; that is a plan invariant.
var ErrDirty = errors.New("worktree has uncommitted changes")

// Branch is the branch name for a session id.
func Branch(id string) string { return BranchPrefix + id }

// Path is where a session's worktree lives.
func Path(workDir, repo, id string) string { return filepath.Join(expandHome(workDir), repo, id) }

// Create adds a worktree on a new branch "agents/<id>" from the repo's
// default branch (fetch first). repoDir is the bare or main checkout.
func Create(ctx context.Context, repoDir, workDir, repo, id string) (path string, err error) {
	if id == "" {
		return "", errors.New("worktree: id is required")
	}
	repoDir = expandHome(repoDir)
	path = Path(workDir, repo, id)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("worktree: %s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if _, err := git(ctx, repoDir, "fetch", "--prune", "origin"); err != nil {
		return "", err
	}
	base, err := DefaultBranch(ctx, repoDir)
	if err != nil {
		return "", err
	}
	if _, err := git(ctx, repoDir, "worktree", "add", "-b", Branch(id), path, "origin/"+base); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes the worktree and its branch. Refuses when dirty unless force.
func Remove(ctx context.Context, repoDir, path string, force bool) error {
	repoDir, path = expandHome(repoDir), expandHome(path)
	if !force {
		dirty, err := IsDirty(ctx, path)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("%w: %s", ErrDirty, path)
		}
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	if _, err := git(ctx, repoDir, append(args, path)...); err != nil {
		return err
	}
	// The branch is named after the worktree directory, which is the session id.
	branch := Branch(filepath.Base(path))
	if _, err := git(ctx, repoDir, "branch", "-D", branch); err != nil {
		// A missing branch is not a failure: the worktree is gone either way.
		if !strings.Contains(err.Error(), "not found") {
			return err
		}
	}
	return nil
}

// IsDirty reports uncommitted or untracked changes.
func IsDirty(ctx context.Context, path string) (bool, error) {
	out, err := git(ctx, expandHome(path), "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// DiffStat returns `git diff --stat` against the default branch.
func DiffStat(ctx context.Context, path string) (string, error) {
	return diff(ctx, path, "--stat")
}

// Diff returns the full patch against the default branch.
func Diff(ctx context.Context, path string) (string, error) {
	return diff(ctx, path)
}

// diff compares the working tree against the point the session branched from,
// so both committed and uncommitted work show up. That is what a reviewer in
// Slack expects to see.
func diff(ctx context.Context, path string, extra ...string) (string, error) {
	path = expandHome(path)
	base, err := DefaultBranch(ctx, path)
	if err != nil {
		return "", err
	}
	from := "origin/" + base
	if mb, err := git(ctx, path, "merge-base", from, "HEAD"); err == nil && strings.TrimSpace(mb) != "" {
		from = strings.TrimSpace(mb)
	}
	args := append([]string{"diff"}, extra...)
	return git(ctx, path, append(args, from)...)
}

// DefaultBranch resolves the remote's HEAD, falling back to main then master.
func DefaultBranch(ctx context.Context, dir string) (string, error) {
	dir = expandHome(dir)
	if out, err := git(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimPrefix(strings.TrimSpace(out), "origin/"); b != "" {
			return b, nil
		}
	}
	for _, b := range []string{"main", "master"} {
		if _, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+b); err == nil {
			return b, nil
		}
	}
	return "", errors.New("worktree: cannot determine default branch of origin")
}

// git runs a git command in dir and returns stdout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return out.String(), nil
}

// expandHome resolves a leading ~/ so config values like "~/work" work.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

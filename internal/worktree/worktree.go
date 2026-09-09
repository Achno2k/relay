// Package worktree manages one git worktree per session under
// <workdir>/<repo>/<id>. Owner: session "core".
package worktree

import "context"

// Create adds a worktree on a new branch "agents/<id>" from the repo's
// default branch (fetch first). repoDir is the bare or main checkout.
func Create(ctx context.Context, repoDir, workDir, repo, id string) (path string, err error) {
	panic("TODO worktree")
}

// Remove deletes the worktree and its branch. Refuses when dirty unless force.
func Remove(ctx context.Context, repoDir, path string, force bool) error { panic("TODO worktree") }

// IsDirty reports uncommitted or untracked changes.
func IsDirty(ctx context.Context, path string) (bool, error) { panic("TODO worktree") }

// DiffStat returns `git diff --stat` against the default branch.
func DiffStat(ctx context.Context, path string) (string, error) { panic("TODO worktree") }

// Diff returns the full patch against the default branch.
func Diff(ctx context.Context, path string) (string, error) { panic("TODO worktree") }

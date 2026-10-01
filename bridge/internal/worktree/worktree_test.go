package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// repo builds an "origin" bare repo with one commit on main plus a clone that
// stands in for the box's checkout. It returns the clone's path.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "web")

	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "init", "--bare", "--initial-branch=main", origin)
	run(t, root, "init", "--initial-branch=main", seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, seed, "add", ".")
	run(t, seed, "commit", "-m", "initial")
	run(t, seed, "remote", "add", "origin", origin)
	run(t, seed, "push", "-u", "origin", "main")

	run(t, root, "clone", origin, clone)
	return clone
}

func TestBranchAndPath(t *testing.T) {
	if got := Branch("a3f2"); got != "agents/a3f2" {
		t.Errorf("Branch = %q", got)
	}
	if got := Path("/w", "web", "a3f2"); got != "/w/web/a3f2" {
		t.Errorf("Path = %q", got)
	}
}

func TestDefaultBranch(t *testing.T) {
	got, err := DefaultBranch(context.Background(), repo(t))
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want main", got)
	}
}

func TestCreateMakesAWorktreeOnItsOwnBranch(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	workDir := t.TempDir()

	path, err := Create(ctx, repoDir, workDir, "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := filepath.Join(workDir, "web", "a3f2"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Errorf("worktree has no checkout: %v", err)
	}
	branch := strings.TrimSpace(run(t, path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "agents/a3f2" {
		t.Errorf("branch = %q, want agents/a3f2", branch)
	}
}

func TestCreateRefusesAnExistingPath(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	workDir := t.TempDir()

	if _, err := Create(ctx, repoDir, workDir, "web", "a3f2"); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := Create(ctx, repoDir, workDir, "web", "a3f2"); err == nil {
		t.Fatal("second Create should fail")
	}
}

func TestCreateRejectsEmptyID(t *testing.T) {
	if _, err := Create(context.Background(), repo(t), t.TempDir(), "web", ""); err == nil {
		t.Fatal("want an error for an empty id")
	}
}

func TestTwoSessionsCoexist(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	workDir := t.TempDir()

	for _, id := range []string{"a3f2", "b7d1"} {
		if _, err := Create(ctx, repoDir, workDir, "web", id); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	list := run(t, repoDir, "worktree", "list")
	for _, id := range []string{"a3f2", "b7d1"} {
		if !strings.Contains(list, id) {
			t.Errorf("worktree %s missing from:\n%s", id, list)
		}
	}
}

func TestIsDirty(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	path, err := Create(ctx, repoDir, t.TempDir(), "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dirty, err := IsDirty(ctx, path)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("a fresh worktree should be clean")
	}

	// An untracked file counts as dirty; we must never delete that work.
	if err := os.WriteFile(filepath.Join(path, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = IsDirty(ctx, path)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if !dirty {
		t.Error("an untracked file should make the worktree dirty")
	}
}

func TestRemoveDeletesTheWorktreeAndBranch(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	path, err := Create(ctx, repoDir, t.TempDir(), "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Remove(ctx, repoDir, path, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree still on disk: %v", err)
	}
	if out := run(t, repoDir, "branch", "--list", "agents/a3f2"); strings.TrimSpace(out) != "" {
		t.Errorf("branch survived: %q", out)
	}
}

// PLAN.md: never delete a dirty worktree.
func TestRemoveRefusesDirtyWithoutForce(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	path, err := Create(ctx, repoDir, t.TempDir(), "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = Remove(ctx, repoDir, path, false)
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("Remove: %v, want ErrDirty", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("a refused Remove must leave the worktree alone: %v", statErr)
	}
}

func TestRemoveForceDeletesDirty(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	path, err := Create(ctx, repoDir, t.TempDir(), "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Remove(ctx, repoDir, path, true); err != nil {
		t.Fatalf("Remove --force: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree still on disk: %v", err)
	}
}

func TestDiffCoversCommittedAndUncommittedWork(t *testing.T) {
	ctx := context.Background()
	repoDir := repo(t)
	path, err := Create(ctx, repoDir, t.TempDir(), "web", "a3f2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := Diff(ctx, path); err != nil || strings.TrimSpace(got) != "" {
		t.Fatalf("fresh worktree diff = %q, err %v", got, err)
	}

	// One committed change and one still in the working tree.
	if err := os.WriteFile(filepath.Join(path, "committed.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, path, "add", ".")
	run(t, path, "commit", "-m", "work")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("seed\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch, err := Diff(ctx, path)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, want := range []string{"committed.txt", "README.md", "+edited"} {
		if !strings.Contains(patch, want) {
			t.Errorf("patch missing %q:\n%s", want, patch)
		}
	}

	stat, err := DiffStat(ctx, path)
	if err != nil {
		t.Fatalf("DiffStat: %v", err)
	}
	if !strings.Contains(stat, "2 files changed") {
		t.Errorf("DiffStat = %q, want 2 files changed", stat)
	}
	if strings.Contains(stat, "+edited") {
		t.Errorf("DiffStat should summarise, not patch:\n%s", stat)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := expandHome("~/work"); got != filepath.Join(home, "work") {
		t.Errorf("expandHome(~/work) = %q", got)
	}
	if got := expandHome("/abs/work"); got != "/abs/work" {
		t.Errorf("expandHome left an absolute path alone: %q", got)
	}
	if got := expandHome("~work"); got != "~work" {
		t.Errorf("expandHome should not touch ~user: %q", got)
	}
}

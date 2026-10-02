package changes

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"relay/internal/api"
)

// Every test gets an empty HOME, so the user's ~/.gitconfig can't change what git prints.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "changes-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// repo makes a temp git repo; its path has symlinks resolved (macOS temp dirs are under a link).
func repo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	return dir
}

var commitN int

// run is git for setting up a test repo (identity and dates fixed, no signing).
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commitN++
	date := time.Date(2026, 10, 3, 8, 0, commitN, 0, time.UTC).Format(time.RFC3339)
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	return run(t, dir, "rev-parse", "HEAD")
}

func lines(n int, prefix string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(prefix + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

func status(t *testing.T, err error) int {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("want an *api.Error, got %v", err)
	}
	return e.Status
}

func byPath(files []api.ChangedFile) map[string]api.ChangedFile {
	m := map[string]api.ChangedFile{}
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

var ctx = context.Background()

func TestNotARepo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "x\n")
	c, err := Summary(ctx, dir)
	if err != nil || c.Repo {
		t.Fatalf("got %+v, %v", c, err)
	}
	if b, _ := api.Marshal(c); string(b) != `{"repo":false}` {
		t.Errorf("json %s", b)
	}
	if _, err := Diff(ctx, dir, "a.txt"); status(t, err) != 404 {
		t.Errorf("diff: %v", err)
	}
	if _, err := Commit(ctx, dir, "abcd"); status(t, err) != 404 {
		t.Errorf("commit: %v", err)
	}
	if _, err := Summary(ctx, ""); status(t, err) != 404 {
		t.Errorf("no cwd: %v", err)
	}
	if _, err := Summary(ctx, filepath.Join(dir, "gone")); status(t, err) != 404 {
		t.Errorf("missing cwd: %v", err)
	}
}

func TestUncommitted(t *testing.T) {
	dir := repo(t)
	write(t, dir, "mod.txt", "a\nb\nc\n")
	write(t, dir, "gone.txt", "1\n2\n")
	write(t, dir, "old.txt", lines(20, "line "))
	write(t, dir, "img.bin", "\x00\x01\x02")
	write(t, dir, ".gitignore", "*.log\n")
	commit(t, dir, "base")

	write(t, dir, "mod.txt", "a\nB\nc\nd\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	run(t, dir, "mv", "old.txt", "new.txt")
	write(t, dir, "img.bin", "\x00\x01\x03")
	write(t, dir, "staged.txt", "s\n")
	run(t, dir, "add", "staged.txt")
	write(t, dir, "untracked.txt", "u1\nu2\nu3") // no final newline
	write(t, dir, "untracked.bin", "a\x00b")
	write(t, dir, "debug.log", "ignored\n")

	c, err := Summary(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Repo || c.Branch == nil || *c.Branch != "main" || c.Upstream != nil || c.Ahead != nil {
		t.Fatalf("header %+v", c)
	}
	var paths []string
	for _, f := range c.Files {
		paths = append(paths, f.Path)
	}
	want := "gone.txt img.bin mod.txt new.txt staged.txt untracked.bin untracked.txt"
	if strings.Join(paths, " ") != want {
		t.Fatalf("paths %v", paths)
	}
	m := byPath(c.Files)
	check := func(path, st string, add, del int, bin bool) {
		t.Helper()
		f := m[path]
		if f.Status != st || f.Additions != add || f.Deletions != del || f.Binary != bin {
			t.Errorf("%s: %+v", path, f)
		}
	}
	check("mod.txt", "modified", 2, 1, false)
	check("gone.txt", "deleted", 0, 2, false)
	check("new.txt", "renamed", 0, 0, false)
	check("img.bin", "modified", 0, 0, true)
	check("staged.txt", "added", 1, 0, false)
	check("untracked.txt", "untracked", 3, 0, false)
	check("untracked.bin", "untracked", 0, 0, true)
	if m["new.txt"].OldPath != "old.txt" {
		t.Errorf("rename %+v", m["new.txt"])
	}
	// One commit, no remotes: it counts as unpushed.
	if len(c.Commits) != 1 || c.Commits[0].Subject != "base" || c.Commits[0].FileCount != 5 || c.MoreCommits {
		t.Errorf("commits %+v", c.Commits)
	}
	if got := c.Commits[0].Time; got != "2026-10-03T08:00:"+got[17:19]+"+00:00" {
		t.Errorf("time %q", got)
	}
}

func TestDiff(t *testing.T) {
	dir := repo(t)
	write(t, dir, "mod.txt", "a\nb\nc\n")
	write(t, dir, "gone.txt", "1\n2\n")
	write(t, dir, "old.txt", lines(20, "line "))
	write(t, dir, "img.bin", "\x00\x01\x02")
	write(t, dir, "same.txt", "same\n")
	commit(t, dir, "base")
	write(t, dir, "mod.txt", "a\nB\nc\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	run(t, dir, "mv", "old.txt", "new.txt")
	write(t, dir, "new.txt", lines(20, "line ")+"more\n")
	write(t, dir, "img.bin", "\x00\x01\x03")
	write(t, dir, "fresh.txt", "x\ny\n")

	d, err := Diff(ctx, dir, "mod.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.Diff, "diff --git a/mod.txt b/mod.txt\n") || !strings.Contains(d.Diff, "\n--- a/mod.txt\n+++ b/mod.txt\n") ||
		!strings.Contains(d.Diff, "\n-b\n+B\n") || d.Binary || d.Truncated || d.Path != "mod.txt" {
		t.Errorf("modified: %+v", d)
	}
	d, _ = Diff(ctx, dir, "fresh.txt")
	if !strings.Contains(d.Diff, "--- /dev/null\n+++ b/fresh.txt\n") || !strings.Contains(d.Diff, "+x\n+y\n") {
		t.Errorf("untracked: %q", d.Diff)
	}
	d, _ = Diff(ctx, dir, "gone.txt")
	if !strings.Contains(d.Diff, "+++ /dev/null\n") || !strings.Contains(d.Diff, "-1\n-2\n") {
		t.Errorf("deleted: %q", d.Diff)
	}
	d, _ = Diff(ctx, dir, "new.txt")
	if !strings.Contains(d.Diff, "rename from old.txt\nrename to new.txt\n") || !strings.Contains(d.Diff, "+more\n") {
		t.Errorf("renamed: %q", d.Diff)
	}
	d, _ = Diff(ctx, dir, "./img.bin")
	if !d.Binary || d.Diff != "" || d.Path != "img.bin" {
		t.Errorf("binary: %+v", d)
	}

	for path, code := range map[string]int{
		"same.txt": 404, "nope.txt": 404, "": 400, "/etc/hosts": 400, "~/x": 400, "a\x00b": 400,
		"../outside.txt": 403, "sub/../../x": 403,
	} {
		if _, err := Diff(ctx, dir, path); status(t, err) != code {
			t.Errorf("%q: want %d, got %v", path, code, err)
		}
	}
	// A symlink that leads out of the cwd.
	outside := t.TempDir()
	write(t, outside, "secret.txt", "s\n")
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt"))
	if _, err := Diff(ctx, dir, "link.txt"); status(t, err) != 403 {
		t.Errorf("symlink escape: %v", err)
	}
}

func TestSubdirectoryCwd(t *testing.T) {
	dir := repo(t)
	write(t, dir, "top.txt", "t\n")
	write(t, dir, "app/src/main.go", "package main\n")
	commit(t, dir, "base")
	write(t, dir, "top.txt", "T\n")
	write(t, dir, "app/src/main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "app/notes.md", "n\n")
	write(t, dir, "other.md", "o\n")

	cwd := filepath.Join(dir, "app")
	c, err := Summary(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 2 || c.Files[0].Path != "notes.md" || c.Files[1].Path != "src/main.go" {
		t.Fatalf("files %+v", c.Files)
	}
	d, err := Diff(ctx, cwd, "src/main.go")
	if err != nil || !strings.Contains(d.Diff, "--- a/src/main.go\n+++ b/src/main.go\n") {
		t.Fatalf("diff %q %v", d.Diff, err)
	}
	if _, err := Diff(ctx, cwd, "../top.txt"); status(t, err) != 403 {
		t.Errorf("parent file: %v", err)
	}

	// The commit's diff is limited to the cwd too, with cwd-relative paths.
	sha := commit(t, dir, "both")
	cd, err := Commit(ctx, cwd, sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(cd.Files) != 2 || cd.Files[0].Path != "notes.md" || !strings.Contains(cd.Files[1].Diff, "+++ b/src/main.go\n") {
		t.Errorf("commit files %+v", cd.Files)
	}
}

func TestUpstreamAheadBehind(t *testing.T) {
	remote := repo(t)
	run(t, remote, "config", "receive.denyCurrentBranch", "ignore")
	write(t, remote, "r.txt", "r\n")
	commit(t, remote, "root")

	dir := filepath.Join(t.TempDir(), "clone")
	run(t, filepath.Dir(dir), "clone", "-q", remote, dir)
	write(t, dir, "a.txt", "a\n")
	first := commit(t, dir, "local one")
	write(t, dir, "a.txt", "a\nb\n")
	commit(t, dir, "local two\n\nbody line\n")
	write(t, remote, "r.txt", "r\nr2\n")
	commit(t, remote, "remote")
	run(t, dir, "fetch", "-q")

	c, err := Summary(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Upstream == nil || *c.Upstream != "origin/main" || c.Ahead == nil || *c.Ahead != 2 || *c.Behind != 1 {
		t.Fatalf("upstream %v ahead %v behind %v", c.Upstream, c.Ahead, c.Behind)
	}
	if len(c.Commits) != 2 || c.Commits[0].Subject != "local two" || c.Commits[1].SHA != first ||
		c.Commits[0].Additions != 1 || c.Commits[0].FileCount != 1 || len(c.Commits[1].ShortSHA) < 7 {
		t.Errorf("commits %+v", c.Commits)
	}

	// Detail of the newest commit, by short sha.
	d, err := Commit(ctx, dir, c.Commits[0].ShortSHA)
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA != c.Commits[0].SHA || d.Body != "body line" || len(d.Files) != 1 || d.Files[0].Status != "modified" ||
		!strings.Contains(d.Files[0].Diff, " a\n+b\n") || d.Truncated {
		t.Errorf("detail %+v", d)
	}
	// The root commit (no parent) and a pushed commit both work: anything reachable from HEAD.
	root := run(t, dir, "rev-list", "--max-parents=0", "HEAD")
	d, err = Commit(ctx, dir, root)
	if err != nil || len(d.Files) != 1 || d.Files[0].Status != "added" || !strings.Contains(d.Files[0].Diff, "--- /dev/null\n") {
		t.Errorf("root %+v %v", d, err)
	}
	// The remote's new commit isn't reachable from HEAD.
	theirs := run(t, dir, "rev-parse", "origin/main")
	for _, sha := range []string{theirs, "zzzz", "abc", strings.Repeat("0", 40), "deadbeef"} {
		if _, err := Commit(ctx, dir, sha); status(t, err) != 404 {
			t.Errorf("%s: %v", sha, err)
		}
	}

	// Pushed: nothing unpushed any more, behind stays from the last fetch.
	run(t, dir, "pull", "-q", "--no-rebase", "--no-edit")
	run(t, dir, "push", "-q")
	c, _ = Summary(ctx, dir)
	if len(c.Commits) != 0 || *c.Ahead != 0 || *c.Behind != 0 {
		t.Errorf("after push: %+v ahead %d", c.Commits, *c.Ahead)
	}
	if b, _ := api.Marshal(c); !strings.Contains(string(b), `"commits":[]`) || !strings.Contains(string(b), `"files":[]`) {
		t.Errorf("json %s", b)
	}
}

func TestMergeCommitDiffsAgainstFirstParent(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", "a\n")
	commit(t, dir, "root")
	run(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "side.txt", "s\n")
	commit(t, dir, "side")
	run(t, dir, "checkout", "-q", "main")
	write(t, dir, "main.txt", "m\n")
	commit(t, dir, "main")
	run(t, dir, "merge", "-q", "--no-edit", "side")
	merge := run(t, dir, "rev-parse", "HEAD")

	d, err := Commit(ctx, dir, merge)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "side.txt" {
		t.Errorf("merge files %+v", d.Files)
	}
	c, _ := Summary(ctx, dir)
	if len(c.Commits) != 4 || c.Commits[0].SHA != merge || c.Commits[0].FileCount != 1 {
		t.Errorf("commits %+v", c.Commits)
	}
}

func TestUnbornDetachedConflicted(t *testing.T) {
	dir := repo(t)
	write(t, dir, "staged.txt", "a\nb\n")
	run(t, dir, "add", "staged.txt")
	write(t, dir, "loose.txt", "l\n")
	c, err := Summary(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(c.Files)
	if *c.Branch != "main" || len(c.Commits) != 0 || m["staged.txt"].Status != "added" || m["staged.txt"].Additions != 2 ||
		m["loose.txt"].Status != "untracked" {
		t.Fatalf("unborn %+v", c)
	}
	if d, err := Diff(ctx, dir, "staged.txt"); err != nil || !strings.Contains(d.Diff, "+a\n+b\n") {
		t.Errorf("unborn diff %q %v", d.Diff, err)
	}

	write(t, dir, "f.txt", "a\nb\n")
	commit(t, dir, "one")
	run(t, dir, "checkout", "-q", "-b", "x")
	write(t, dir, "f.txt", "a\nX\n")
	commit(t, dir, "x")
	run(t, dir, "checkout", "-q", "main")
	write(t, dir, "f.txt", "a\nY\n")
	commit(t, dir, "y")
	cmd := exec.Command("git", "-c", "user.name=T", "-c", "user.email=t@example.com", "merge", "-q", "x")
	cmd.Dir = dir
	_ = cmd.Run() // conflicts
	c, _ = Summary(ctx, dir)
	if byPath(c.Files)["f.txt"].Status != "conflicted" {
		t.Errorf("conflict %+v", c.Files)
	}
	if d, err := Diff(ctx, dir, "f.txt"); err != nil || !strings.Contains(d.Diff, "<<<<<<<") {
		t.Errorf("conflict diff %q %v", d.Diff, err)
	}
	run(t, dir, "merge", "--abort")

	run(t, dir, "checkout", "-q", "--detach", "HEAD")
	c, _ = Summary(ctx, dir)
	if c.Branch != nil || c.Upstream != nil {
		t.Errorf("detached %+v", c)
	}
	if b, _ := api.Marshal(c); !strings.Contains(string(b), `"branch":null,"upstream":null`) || strings.Contains(string(b), "ahead") {
		t.Errorf("json %s", b)
	}
}

func TestCaps(t *testing.T) {
	dir := repo(t)
	for i := range CommitLimit + 1 {
		run(t, dir, "commit", "-q", "--allow-empty", "-m", "c"+strconv.Itoa(i))
	}
	big := lines(30000, "a fairly long line of text to make the diff large, number ") // ~1.8 MB
	for i := range 5 {
		write(t, dir, "big"+strconv.Itoa(i)+".txt", big)
	}
	sha := commit(t, dir, "big files")
	c, err := Summary(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Commits) != CommitLimit || !c.MoreCommits || c.Commits[0].SHA != sha {
		t.Errorf("commits %d more %v", len(c.Commits), c.MoreCommits)
	}

	d, err := Commit(ctx, dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, f := range d.Files {
		total += len(f.Diff)
		if len(f.Diff) > DiffLimit || !f.Truncated || (f.Diff != "" && !strings.HasSuffix(f.Diff, "\n")) {
			t.Errorf("file %d: %d bytes truncated %v", i, len(f.Diff), f.Truncated)
		}
		if f.Additions != 30000 {
			t.Errorf("file %d additions %d", i, f.Additions)
		}
	}
	if !d.Truncated || total > CommitDiffLimit || len(d.Files[0].Diff) < DiffLimit-200 || len(d.Files[4].Diff) > 1024 {
		t.Errorf("total %d truncated %v", total, d.Truncated)
	}

	write(t, dir, "big0.txt", lines(30000, "changed "))
	fd, err := Diff(ctx, dir, "big0.txt")
	if err != nil || !fd.Truncated || len(fd.Diff) > DiffLimit || len(fd.Diff) < DiffLimit-200 {
		t.Errorf("file diff %d %v %v", len(fd.Diff), fd.Truncated, err)
	}

	for i := range FileLimit + 1 {
		write(t, dir, "many/"+strconv.Itoa(i), "x\n")
	}
	c, _ = Summary(ctx, dir)
	if len(c.Files) != FileLimit || !c.MoreFiles {
		t.Errorf("files %d more %v", len(c.Files), c.MoreFiles)
	}
}

// The user's git config can't change the output (prefixes, colour, external diff).
func TestHostileConfig(t *testing.T) {
	dir := repo(t)
	write(t, dir, "f.txt", "a\n")
	commit(t, dir, "one")
	for _, kv := range [][2]string{{"diff.noprefix", "true"}, {"diff.mnemonicPrefix", "true"}, {"color.ui", "always"},
		{"color.diff", "always"}, {"diff.external", "false"}, {"core.pager", "false"}, {"diff.relative", "true"}} {
		run(t, dir, "config", kv[0], kv[1])
	}
	write(t, dir, "f.txt", "b\n")
	d, err := Diff(ctx, dir, "f.txt")
	if err != nil || !strings.Contains(d.Diff, "--- a/f.txt\n+++ b/f.txt\n") || strings.Contains(d.Diff, "\x1b") {
		t.Errorf("%q %v", d.Diff, err)
	}
	if c, err := Summary(ctx, dir); err != nil || len(c.Commits) != 1 || c.Commits[0].FileCount != 1 || len(c.Files) != 1 {
		t.Errorf("summary %+v %v", c, err)
	}
	// The bridge never takes index.lock.
	if _, err := os.Stat(filepath.Join(dir, ".git", "index.lock")); err == nil {
		t.Error("index.lock left behind")
	}
}

func TestGitMissingAndTimeout(t *testing.T) {
	dir := repo(t)
	defer func(b string, d time.Duration) { gitBin, Timeout = b, d }(gitBin, Timeout)

	gitBin = "relay-no-such-git"
	if _, err := Summary(ctx, dir); status(t, err) != http.StatusServiceUnavailable {
		t.Errorf("missing git: %v", err)
	}

	slow := filepath.Join(t.TempDir(), "git")
	os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	gitBin, Timeout = slow, 100*time.Millisecond
	start := time.Now()
	if _, err := Summary(ctx, dir); status(t, err) != http.StatusGatewayTimeout {
		t.Errorf("slow git: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}

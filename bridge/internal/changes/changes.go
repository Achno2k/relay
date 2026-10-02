// Package changes serves GET /agents/:id/changes*: the uncommitted files and unpushed commits in
// the agent's cwd, read with git and never changing the repo. See api.md "Changes".
package changes

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/files"
)

const (
	// FileLimit and CommitLimit cap the lists of Changes.
	FileLimit   = 500
	CommitLimit = 50
	// DiffLimit caps one file's diff; CommitDiffLimit all of a CommitDetail's diffs together.
	DiffLimit       = 256 << 10
	CommitDiffLimit = 1 << 20
	// sniffLen is how much of an untracked file decides binary, as git does.
	sniffLen = 8000
)

var (
	errNotRepo = api.NotFound("not a git repository")
	errNoFile  = api.NotFound("no uncommitted change at this path")
	errNoSHA   = api.NotFound("no such commit")
)

// diffFlags make every diff the same whatever the user's config says.
var diffFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv", "-M", "-U3",
	"--src-prefix=a/", "--dst-prefix=b/", "--submodule=short"}

// Summary is GET /agents/:id/changes.
func Summary(ctx context.Context, cwd string) (api.Changes, error) {
	g, err := open(ctx, cwd)
	if err != nil {
		return api.Changes{}, err
	}
	ok, err := g.inWorkTree()
	if err != nil || !ok {
		return api.Changes{}, err
	}
	c := api.Changes{Repo: true}
	head, err := g.head()
	if err != nil {
		return c, err
	}
	if out, err := g.out("symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		c.Branch = ptr(strings.TrimSpace(out))
	} else if !exited(err, 1) {
		return c, err
	}
	if c.Branch != nil && head {
		if err := g.upstream(&c); err != nil {
			return c, err
		}
	}
	list, err := g.uncommitted(head)
	if err != nil {
		return c, err
	}
	if len(list) > FileLimit {
		list, c.MoreFiles = list[:FileLimit], true
	}
	for i := range list {
		if list[i].Status == "untracked" {
			untrackedStats(filepath.Join(cwd, filepath.FromSlash(list[i].Path)), &list[i])
		}
	}
	c.Files = list
	if head {
		if c.Commits, c.MoreCommits, err = g.unpushed(); err != nil {
			return c, err
		}
	}
	return c, nil
}

// Diff is GET /agents/:id/changes/diff: one uncommitted file's diff.
func Diff(ctx context.Context, cwd, rel string) (api.FileDiffText, error) {
	clean, _, err := files.Resolve(cwd, rel)
	if err != nil {
		return api.FileDiffText{}, err
	}
	path := filepath.ToSlash(clean)
	g, err := open(ctx, cwd)
	if err != nil {
		return api.FileDiffText{}, err
	}
	if ok, err := g.inWorkTree(); err != nil || !ok {
		return api.FileDiffText{}, orNotRepo(err)
	}
	head, err := g.head()
	if err != nil {
		return api.FileDiffText{}, err
	}
	list, err := g.uncommitted(head)
	if err != nil {
		return api.FileDiffText{}, err
	}
	i := sort.Search(len(list), func(i int) bool { return list[i].Path >= path })
	if i == len(list) || list[i].Path != path {
		return api.FileDiffText{}, errNoFile
	}
	f := list[i]
	out := api.FileDiffText{Path: path}
	var args []string
	if f.Status == "untracked" {
		untrackedStats(filepath.Join(cwd, clean), &f)
		args = append(append([]string{"diff", "--no-index"}, diffFlags...), "--", "/dev/null", path)
	} else {
		base, err := g.base(head)
		if err != nil {
			return out, err
		}
		args = append(append([]string{"diff", base, "--relative"}, diffFlags...), "--", path)
		if f.OldPath != "" {
			args = append(args, f.OldPath)
		}
	}
	if f.Binary {
		out.Binary = true
		return out, nil
	}
	s := newSplitter(DiffLimit, DiffLimit)
	err = g.stream(s.read, args...)
	if f.Status == "untracked" && exited(err, 1) {
		err = nil // --no-index exits 1 when the files differ
	}
	if err != nil {
		return out, err
	}
	for _, sec := range s.sections {
		out.Diff += string(sec.buf)
		out.Truncated = out.Truncated || sec.cut
	}
	return out, nil
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

// Commit is GET /agents/:id/changes/commits/:sha: any commit reachable from HEAD.
func Commit(ctx context.Context, cwd, sha string) (api.CommitDetail, error) {
	if !shaPattern.MatchString(sha) {
		return api.CommitDetail{}, errNoSHA
	}
	g, err := open(ctx, cwd)
	if err != nil {
		return api.CommitDetail{}, err
	}
	if ok, err := g.inWorkTree(); err != nil || !ok {
		return api.CommitDetail{}, orNotRepo(err)
	}
	out, err := g.out("rev-parse", "--verify", "-q", sha+"^{commit}")
	full := strings.TrimSpace(out)
	if exited(err, 1) || exited(err, 128) || !strings.HasPrefix(full, strings.ToLower(sha)) {
		// The prefix check: a branch named like a short sha resolves to the branch.
		return api.CommitDetail{}, errNoSHA
	}
	if err != nil {
		return api.CommitDetail{}, err
	}
	if err := g.run("merge-base", "--is-ancestor", full, "HEAD"); err != nil {
		if exited(err, 1) || exited(err, 128) {
			return api.CommitDetail{}, errNoSHA
		}
		return api.CommitDetail{}, err
	}
	meta, err := g.out("show", "-s", "--format=%H%x00%h%x00%ct%x00%s%x00%b", full, "--")
	if err != nil {
		return api.CommitDetail{}, err
	}
	parts := strings.SplitN(meta, "\x00", 5)
	if len(parts) < 5 {
		return api.CommitDetail{}, errNoSHA
	}
	d := api.CommitDetail{SHA: parts[0], ShortSHA: parts[1], Time: unixTime(parts[2]),
		Subject: parts[3], Body: strings.TrimSpace(parts[4])}

	parent := full + "^1"
	if err := g.run("rev-parse", "--verify", "-q", parent); err != nil {
		if !exited(err, 1) && !exited(err, 128) {
			return d, err
		}
		if parent, err = g.emptyTree(); err != nil {
			return d, err
		}
	}
	list, err := g.changed(parent, full)
	if err != nil {
		return d, err
	}
	s := newSplitter(DiffLimit, CommitDiffLimit)
	if err := g.stream(s.read, append(append([]string{"diff", parent, full, "--relative"}, diffFlags...), "--")...); err != nil {
		return d, err
	}
	// name-status, numstat and the patch come from the same diff, so they're in the same order.
	d.Files = make([]api.CommitFile, len(list))
	for i, f := range list {
		cf := api.CommitFile{ChangedFile: f}
		if i < len(s.sections) && !f.Binary {
			cf.Diff, cf.Truncated = string(s.sections[i].buf), s.sections[i].cut
		}
		d.Truncated = d.Truncated || cf.Truncated
		d.Files[i] = cf
	}
	return d, nil
}

// open checks the cwd before git runs in it.
func open(ctx context.Context, cwd string) (git, error) {
	if cwd == "" {
		return git{}, api.NotFound("the agent has no known folder")
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return git{}, api.NotFound("the agent's folder is gone")
	}
	return git{ctx: ctx, dir: cwd}, nil
}

func orNotRepo(err error) error {
	if err == nil {
		return errNotRepo
	}
	return err
}

// inWorkTree: false (no error) when git says the cwd isn't in a work tree.
func (g git) inWorkTree() (bool, error) {
	out, err := g.out("rev-parse", "--is-inside-work-tree")
	if err != nil {
		if exited(err, 128) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

// head: whether HEAD names a commit (false on an unborn branch).
func (g git) head() (bool, error) {
	err := g.run("rev-parse", "--verify", "-q", "HEAD^{commit}")
	if exited(err, 1) || exited(err, 128) {
		return false, nil
	}
	return err == nil, err
}

func (g git) run(args ...string) error {
	_, err := g.out(args...)
	return err
}

// base is what uncommitted changes diff against: HEAD, or the empty tree before the first commit.
func (g git) base(head bool) (string, error) {
	if head {
		return "HEAD", nil
	}
	return g.emptyTree()
}

func (g git) emptyTree() (string, error) {
	out, err := g.out("hash-object", "-t", "tree", "/dev/null")
	return strings.TrimSpace(out), err
}

func (g git) upstream(c *api.Changes) error {
	out, err := g.out("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		if exited(err, 128) || exited(err, 1) {
			return nil // no upstream, or it's gone
		}
		return err
	}
	c.Upstream = ptr(strings.TrimSpace(out))
	out, err = g.out("rev-list", "--left-right", "--count", "HEAD...@{upstream}", "--")
	if err != nil {
		return err
	}
	f := strings.Fields(out)
	if len(f) == 2 {
		a, _ := strconv.Atoi(f[0])
		b, _ := strconv.Atoi(f[1])
		c.Ahead, c.Behind = &a, &b
	}
	return nil
}

// uncommitted is `git diff <base>` under the cwd plus untracked files, sorted by path. Untracked
// files have no counts yet (untrackedStats).
func (g git) uncommitted(head bool) ([]api.ChangedFile, error) {
	base, err := g.base(head)
	if err != nil {
		return nil, err
	}
	list, err := g.changed(base, "")
	if err != nil {
		return nil, err
	}
	out, err := g.out("ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	conflicted := map[string]bool{}
	for _, rec := range splitZ(out) {
		if _, p, ok := strings.Cut(rec, "\t"); ok {
			conflicted[p] = true
		}
	}
	for i := range list {
		if conflicted[list[i].Path] {
			list[i].Status, list[i].OldPath = "conflicted", ""
		}
	}
	out, err = g.out("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitZ(out) {
		if !strings.HasSuffix(p, "/") { // a nested repository
			list = append(list, api.ChangedFile{Path: p, Status: "untracked"})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list, nil
}

// changed lists the files between from and to (the work tree when to is ""), cwd-relative, in
// git's order.
func (g git) changed(from, to string) ([]api.ChangedFile, error) {
	args := []string{"diff", from}
	if to != "" {
		args = append(args, to)
	}
	args = append(args, "--relative", "--no-ext-diff", "--no-textconv", "-M", "--submodule=short", "-z")
	status, err := g.out(append(args, "--name-status", "--")...)
	if err != nil {
		return nil, err
	}
	numstat, err := g.out(append(args, "--numstat", "--")...)
	if err != nil {
		return nil, err
	}
	var list []api.ChangedFile
	toks := splitZ(status)
	for i := 0; i < len(toks); i++ {
		f := api.ChangedFile{Status: statusName(toks[i])}
		if c := toks[i][0]; (c == 'R' || c == 'C') && i+2 < len(toks) {
			if c == 'R' {
				f.OldPath = toks[i+1]
			}
			f.Path = toks[i+2]
			i += 2
		} else if i+1 < len(toks) {
			f.Path = toks[i+1]
			i++
		}
		list = append(list, f)
	}
	toks = splitZ(numstat)
	for i, n := 0, 0; i < len(toks) && n < len(list); n++ {
		f := strings.SplitN(toks[i], "\t", 3)
		i++
		if len(f) == 3 && f[2] == "" {
			i += 2 // a rename: old and new path follow
		}
		if len(f) >= 2 {
			list[n].Binary = f[0] == "-"
			list[n].Additions, _ = strconv.Atoi(f[0])
			list[n].Deletions, _ = strconv.Atoi(f[1])
		}
	}
	return list, nil
}

func statusName(s string) string {
	switch s[0] {
	case 'A', 'C':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'U':
		return "conflicted"
	}
	return "modified"
}

// unpushed is `git log HEAD --not --remotes`, newest first, each commit counted as a whole.
func (g git) unpushed() ([]api.CommitSummary, bool, error) {
	out, err := g.out("log", "HEAD", "--not", "--remotes", "-n", strconv.Itoa(CommitLimit+1),
		"--format=%x1e%H%x00%h%x00%ct%x00%s", "--numstat", "--no-relative", "--diff-merges=first-parent",
		"--no-ext-diff", "--no-textconv", "-M", "--")
	if err != nil {
		return nil, false, err
	}
	var list []api.CommitSummary
	for _, rec := range strings.Split(out, "\x1e") {
		header, stats, _ := strings.Cut(rec, "\n")
		h := strings.SplitN(header, "\x00", 4)
		if len(h) < 4 {
			continue
		}
		c := api.CommitSummary{SHA: h[0], ShortSHA: h[1], Time: unixTime(h[2]), Subject: h[3]}
		for _, line := range strings.Split(stats, "\n") {
			f := strings.SplitN(line, "\t", 3)
			if len(f) < 3 {
				continue
			}
			c.FileCount++
			a, _ := strconv.Atoi(f[0])
			d, _ := strconv.Atoi(f[1])
			c.Additions += a
			c.Deletions += d
		}
		list = append(list, c)
	}
	if len(list) > CommitLimit {
		return list[:CommitLimit], true, nil
	}
	return list, false, nil
}

// untrackedStats counts an untracked file's lines like git would diff it against /dev/null.
func untrackedStats(path string, f *api.ChangedFile) {
	fi, err := os.Lstat(path)
	if err != nil {
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		f.Additions = 1 // git diffs a symlink as its target path
		return
	}
	if !fi.Mode().IsRegular() {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	r := bufio.NewReaderSize(file, 64<<10)
	head, _ := r.Peek(sniffLen)
	if bytes.IndexByte(head, 0) >= 0 {
		f.Binary = true
		return
	}
	lines, last := 0, byte('\n')
	buf := make([]byte, 64<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
		}
		if err != nil {
			break
		}
	}
	if last != '\n' {
		lines++
	}
	f.Additions = lines
}

func unixTime(s string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return ""
	}
	return api.FormatTime(time.Unix(n, 0))
}

func splitZ(s string) []string {
	s = strings.TrimSuffix(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

func ptr[T any](v T) *T { return &v }

// splitter cuts a patch into its per-file sections (`diff --git …`), keeping whole lines within
// a per-file and a total budget. A section that didn't fit is marked cut.
type splitter struct {
	per, left int
	sections  []section
}

type section struct {
	buf []byte
	cut bool
}

func newSplitter(per, total int) *splitter { return &splitter{per: per, left: total} }

func (s *splitter) read(r io.Reader) error {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	var line []byte // the current line so far, nil once it's known not to fit
	start, fits := true, true
	for {
		piece, err := br.ReadSlice('\n')
		if len(piece) > 0 {
			if start && (len(s.sections) == 0 || bytes.HasPrefix(piece, []byte("diff --git "))) {
				s.sections = append(s.sections, section{})
			}
			if start {
				line, fits = line[:0], true
			}
			sec := &s.sections[len(s.sections)-1]
			room := min(s.per-len(sec.buf), s.left)
			if fits && len(line)+len(piece) <= room {
				line = append(line, piece...)
			} else if fits {
				fits, sec.cut = false, true
			}
			start = piece[len(piece)-1] == '\n'
			if (start || err == io.EOF) && fits && !sec.cut {
				sec.buf = append(sec.buf, line...)
				s.left -= len(line)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

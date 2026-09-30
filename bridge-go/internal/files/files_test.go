package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/api"
)

func setup(t *testing.T) (cwd, outside string) {
	t.Helper()
	root := t.TempDir()
	cwd = filepath.Join(root, "proj")
	outside = filepath.Join(root, "secret")
	must(t, os.MkdirAll(filepath.Join(cwd, "src"), 0o755))
	must(t, os.MkdirAll(outside, 0o755))
	write(t, filepath.Join(cwd, "src", "app.swift"), "import SwiftUI\n")
	write(t, filepath.Join(outside, "key.txt"), "hunter2\n")
	write(t, filepath.Join(cwd, "bin.dat"), "ab\x00cd")
	write(t, filepath.Join(cwd, "pic.png"), "\x89PNG\r\n\x1a\nrest")
	write(t, filepath.Join(cwd, "fake.png"), "not\x00a png")
	must(t, os.Symlink(filepath.Join(outside, "key.txt"), filepath.Join(cwd, "escape.txt")))
	must(t, os.Symlink(outside, filepath.Join(cwd, "escdir")))
	must(t, os.Symlink(filepath.Join(outside, "missing.txt"), filepath.Join(cwd, "dangling.txt")))
	must(t, os.Symlink(filepath.Join(cwd, "src", "app.swift"), filepath.Join(cwd, "inner.swift")))
	return cwd, outside
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, s string) { t.Helper(); must(t, os.WriteFile(p, []byte(s), 0o644)) }

func status(err error) int {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func TestReadText(t *testing.T) {
	cwd, _ := setup(t)
	for _, p := range []string{"src/app.swift", "./src/../src/app.swift", "inner.swift"} {
		r, err := Read(cwd, p)
		if err != nil || r.Text == nil {
			t.Fatalf("%s: %v", p, err)
		}
		if r.Text.Content != "import SwiftUI\n" || r.Text.Size != 15 || r.Text.Truncated || r.Text.Language != "swift" {
			t.Errorf("%s: %+v", p, r.Text)
		}
	}
}

func TestReadRejects(t *testing.T) {
	cwd, outside := setup(t)
	cases := map[string]int{
		"":                   400,
		"/etc/passwd":        400,
		"~/x":                400,
		"a\x00b":             400,
		"src":                400,
		"../secret/key.txt":  403,
		"../secret/nope.txt": 403,
		"escape.txt":         403,
		"escdir/key.txt":     403,
		"dangling.txt":       403,
		"src/../../secret":   403,
		"nope.txt":           404,
		"bin.dat":            415,
		"fake.png":           415,
		outside + "/key.txt": 400,
	}
	for p, want := range cases {
		if _, err := Read(cwd, p); status(err) != want {
			t.Errorf("%q: got %v, want %d", p, err, want)
		}
	}
	if _, err := Read("", "x"); status(err) != 404 {
		t.Errorf("no cwd: %v", err)
	}
}

func TestReadImage(t *testing.T) {
	cwd, _ := setup(t)
	r, err := Read(cwd, "pic.png")
	if err != nil || r.Text != nil || r.ContentType != "image/png" || string(r.Image) != "\x89PNG\r\n\x1a\nrest" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestReadTruncates(t *testing.T) {
	cwd, _ := setup(t)
	line := strings.Repeat("é", 99) + "\n" // 199 bytes
	big := strings.Repeat(line, TextLimit/len(line)+10)
	write(t, filepath.Join(cwd, "big.txt"), big)
	r, err := Read(cwd, "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	c := r.Text.Content
	if !r.Text.Truncated || r.Text.Size != int64(len(big)) || len(c) > TextLimit || !strings.HasSuffix(c, "\n") || len(c)%len(line) != 0 {
		t.Errorf("truncated=%v size=%d len=%d", r.Text.Truncated, r.Text.Size, len(c))
	}
	// A long first line with no newline still cuts on a UTF-8 boundary.
	write(t, filepath.Join(cwd, "oneline.txt"), strings.Repeat("é", TextLimit))
	r, err = Read(cwd, "oneline.txt")
	if err != nil || !r.Text.Truncated || len(r.Text.Content) != TextLimit || strings.ContainsRune(r.Text.Content, '�') {
		t.Errorf("oneline: %v len=%d", err, len(r.Text.Content))
	}
}

func TestLanguage(t *testing.T) {
	for name, want := range map[string]string{"a/Dockerfile": "dockerfile", "x.TSX": "tsx", "README.md": "md", "x.unknown": ""} {
		if got := Language(name); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

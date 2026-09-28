package transcript

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// Ported from PathScrubberTests.swift.

func TestPathScrubber_scrubs(t *testing.T) {
	s := NewScrubber("/Users/dev/shop-api")
	cases := []struct{ input, expected string }{
		{"/Users/dev/shop-api/src/auth.py", "src/auth.py"},
		{"cd /Users/dev/shop-api && ls", "cd . && ls"},
		{"/Users/dev/shop-api-old/x.py", "x.py"},
		{"/Users/dev/other/thing.txt", "thing.txt"},
		{"see ~/.claude/settings.json", "see settings.json"},
		{"/opt/homebrew/bin/", "bin"},
		{"https://example.com/a/b/c", "https://example.com/a/b/c"},
		{"src/auth.py and and/or", "src/auth.py and and/or"},
		{"run /clear now", "run /clear now"},
		{`{"file_path":"/Users/dev/shop-api/a.swift"}`, `{"file_path":"a.swift"}`},
		{"`/tmp/x/y.log`", "`y.log`"},
	}
	for _, c := range cases {
		if got := s.Scrub(c.input); got != c.expected {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestPathScrubber_noCwd(t *testing.T) {
	if got := NewScrubber("").Scrub("/a/b/c.txt"); got != "c.txt" {
		t.Errorf("got %q", got)
	}
	if got := NewScrubber("/").Scrub("/a/b"); got != "b" {
		t.Errorf("got %q", got)
	}
}

func TestPathScrubber_cwdName(t *testing.T) {
	for in, want := range map[string]string{"/Users/dev/shop-api": "shop-api", "/Users/dev/shop-api/": "shop-api", "": ""} {
		if got := CwdName(in); got != want {
			t.Errorf("CwdName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Go-only: cases where RE2 needs the hand-written lookbehind/lookahead to match ICU.
// Expected values are the Swift bridge's output.
func TestPathScrubber_lookaroundEdges(t *testing.T) {
	s := NewScrubber("/Users/dev/shop-api")
	cases := []struct{ input, expected string }{
		{"a~/x/y", "a~/x/y"},
		{"user@host:/var/log/x", "user@host:/var/log/x"},
		{"/a/b /c/d", "b d"},
		{"(/tmp/a/b)", "(b)"},
		{"/Users/dev/shop-api", "."},
		{"/Users/dev/shop-api/", "./"},
		{"/Users/dev/shop-api/ x", "./ x"},
		{"x/Users/dev/shop-api/a", "xa"},
		{"/Users/dev/shop-apiX/y", "y"},
		{"/Users/dev/shop-api/a /Users/dev/shop-api", "a ."},
		{"é/tmp/a/b", "éb"},
		{"\u00a0/tmp/a/b", "\u00a0b"},
		{"/tmp/a\u2003b/c", "a\u2003b/c"},
		{"~/a", "~/a"},
		{"~/a/b", "b"},
		{"//a//b//", "//a//b//"},
		{"/a/b/c/d:/e/f", "f"},
		{"file:///tmp/a/b", "file:///tmp/a/b"},
		{"/Users/dev/shop-api/Users/dev/shop-api/x", "Users/dev/shop-api/x"},
		{"/Users/dev/shop-api.bak/a", "a"},
		{"😀/tmp/x/y", "😀y"},
		{"/tmp/😀/y", "y"},
		{"/Users/dev/shop-api/\u2003", "./\u2003"},
	}
	for _, c := range cases {
		if got := s.Scrub(c.input); got != c.expected {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

// Go-only: random strings scrubbed by the Swift PathScrubber and cut by ToolSummary.truncate /
// firstLine (testdata/swift-golden.json), so Go matches Foundation/ICU byte for byte.
func TestSwiftGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/swift-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g map[string][][]string
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	s := NewScrubber("/Users/dev/shop-api")
	for _, c := range g["scrub"] {
		if got := s.Scrub(c[0]); got != c[1] {
			t.Errorf("Scrub(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	for _, c := range g["scrubNoCwd"] {
		if got := NewScrubber("").Scrub(c[0]); got != c[1] {
			t.Errorf("Scrub(%q) no cwd = %q, want %q", c[0], got, c[1])
		}
	}
	for _, c := range g["truncate"] {
		if got := Truncate(c[0], 10); got != c[1] {
			t.Errorf("Truncate(%q) = %q, want %q", c[0], got, c[1])
		}
		if got := strconv.Itoa(GraphemeCount(c[0])); got != c[2] {
			t.Errorf("GraphemeCount(%q) = %s, want %s", c[0], got, c[2])
		}
		if got := FirstLine(c[0]); got != c[3] {
			t.Errorf("FirstLine(%q) = %q, want %q", c[0], got, c[3])
		}
	}
	if len(g["scrub"]) < 100 || len(g["truncate"]) < 100 {
		t.Fatal("golden file too small")
	}
}

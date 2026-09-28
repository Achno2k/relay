package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"relay/internal/herdr"
)

// Go-only: the Claude and pi paths of TranscriptLocator (the Swift tests cover codex only).

func claudeAgent(cwd, session string) herdr.Agent {
	return herdr.Agent{PaneID: "w1:p1", Agent: "claude", Cwd: cwd,
		AgentSession: &herdr.AgentSession{Agent: "claude", Kind: "id", Value: session}}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocator_claudeByProjectDirThenAnyDir(t *testing.T) {
	root := t.TempDir()
	expectEqual(t, ProjectDirName("/Users/dev/my.app_x"), "-Users-dev-my-app-x")
	direct := filepath.Join(root, ProjectDirName("/Users/dev/shop-api"), "s1.jsonl")
	touch(t, direct)
	moved := filepath.Join(root, "renamed-project", "s2.jsonl")
	touch(t, moved)
	l := NewLocator(root, NewCodexRollouts(filepath.Join(root, "codex")))

	expectEqual(t, l.Locate(claudeAgent("/Users/dev/shop-api", "s1"), time.Time{}),
		&Ref{Path: direct, Format: FormatClaude, Cwd: "/Users/dev/shop-api"})
	expectEqual(t, l.Locate(claudeAgent("/Users/dev/shop-api", "s2"), time.Time{}),
		&Ref{Path: moved, Format: FormatClaude, Cwd: "/Users/dev/shop-api"})
	if l.Locate(claudeAgent("/Users/dev/shop-api", "none"), time.Time{}) != nil {
		t.Error("found a missing session")
	}
	if l.Locate(claudeAgent("/Users/dev/shop-api", "../s1"), time.Time{}) != nil {
		t.Error("a session id with a slash must not be joined into a path")
	}
}

func TestLocator_piSessionPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi.jsonl")
	touch(t, path)
	l := NewLocator(t.TempDir(), nil)
	a := herdr.Agent{Agent: "pi", ForegroundCwd: "/Users/dev/website",
		AgentSession: &herdr.AgentSession{Agent: "pi", Kind: "path", Value: path}}
	expectEqual(t, l.Locate(a, time.Time{}), &Ref{Path: path, Format: FormatPi, Cwd: "/Users/dev/website"})
	a.AgentSession.Value = path + ".gone"
	if l.Locate(a, time.Time{}) != nil {
		t.Error("found a missing file")
	}
	a.AgentSession = nil
	if l.Locate(a, time.Time{}) != nil {
		t.Error("no session, no transcript")
	}
}

func TestLocator_codexNotBeforeSkipsOlderRollouts(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "09", "24")
	path := filepath.Join(day, "rollout-2026-09-24T14-00-00-abc.jsonl")
	touch(t, path)
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-09-24T14:00:00Z","type":"session_meta","payload":{"cwd":"/Users/dev/p"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewCodexRollouts(root)
	expectEqual(t, r.Newest("/Users/dev/p", time.Now().Add(-time.Hour)), path)
	expectEqual(t, r.Newest("/Users/dev/p", time.Now().Add(time.Hour)), "")
}

// Go-only (R8-19): the session id caches stay bounded.
func TestLocator_cachesAreBounded(t *testing.T) {
	root := t.TempDir()
	l := NewLocator(root, nil)
	for i := 0; i < maxCached*2; i++ {
		id := fmt.Sprintf("s%d", i)
		touch(t, filepath.Join(root, "elsewhere", id+".jsonl"))
		if l.Locate(claudeAgent("/Users/dev/x", id), time.Time{}) == nil {
			t.Fatal("not found")
		}
		if len(l.found) > maxCached {
			t.Fatalf("cache grew to %d", len(l.found))
		}
	}
}

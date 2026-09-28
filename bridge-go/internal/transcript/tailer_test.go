package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/api"
)

// Ported from TailerTests.swift, plus Go-only cases for rotation, truncation, the polling
// fallback and Stop.

type collector struct {
	mu sync.Mutex
	ms []api.Message
}

func (c *collector) add(m api.Message) {
	c.mu.Lock()
	c.ms = append(c.ms, m)
	c.mu.Unlock()
}

func (c *collector) get() []api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]api.Message(nil), c.ms...)
}

func (c *collector) count() int { return len(c.get()) }

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func userLine(id, text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-09-23T13:00:00Z","message":{"content":%q}}`, id, text)
}

func assistantLine(id, text string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-09-23T13:00:01Z","message":{"content":[{"type":"text","text":%q}]}}`, id, text)
}

func startTailer(t *testing.T, path string, c *collector) *Tailer {
	t.Helper()
	tl := NewTailer(Ref{Path: path, Format: FormatClaude}, nil, c.add)
	tl.Start()
	t.Cleanup(tl.Stop)
	return tl
}

func testEmitsOnlyAppendedGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.jsonl")
	if err := os.WriteFile(path, []byte(userLine("u1", "hello")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var c collector
	startTailer(t, path, &c)

	a1 := assistantLine("a1", "one")
	a2 := assistantLine("a2", "two")
	appendFile(t, path, a1+"\n"+a2[:20])
	waitUntil(t, func() bool { return c.count() == 1 })
	expectEqual(t, c.get()[0].ID, "a1")
	expectEqual(t, c.get()[0].Blocks, []api.Block{api.TextBlock("one")})

	appendFile(t, path, a2[20:]+"\n")
	waitUntil(t, func() bool { return c.count() == 2 })
	// The same assistant message grew.
	expectEqual(t, c.get()[1].ID, "a1")
	expectEqual(t, c.get()[1].Blocks, []api.Block{api.TextBlock("one"), api.TextBlock("two")})
}

func TestTailer_emitsOnlyAppendedGrowthAndWaitsForPartialLines(t *testing.T) {
	testEmitsOnlyAppendedGrowth(t)
}

func TestTailer_diesWhenFileIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tl := startTailer(t, path, &collector{})
	if tl.Dead() {
		t.Fatal("dead at start")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, tl.Dead)
}

func TestTailer_pollingFallback(t *testing.T) {
	noWatcher = true
	defer func() { noWatcher = false }()
	testEmitsOnlyAppendedGrowth(t)
}

func TestTailer_diesOnRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.jsonl")
	if err := os.WriteFile(path, []byte(userLine("u1", "hello")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var c collector
	tl := startTailer(t, path, &c)
	// Rotate: move the file away and put a new one in its place.
	if err := os.Rename(path, filepath.Join(dir, "tail.1.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(assistantLine("a1", "new")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, tl.Dead)
	if c.count() != 0 {
		t.Errorf("emitted %d messages from the rotated file", c.count())
	}
	// The owner recreates the tailer, which follows the new file.
	var c2 collector
	startTailer(t, path, &c2)
	appendFile(t, path, assistantLine("a2", "more")+"\n")
	waitUntil(t, func() bool { return c2.count() == 1 })
	expectEqual(t, c2.get()[0].Blocks, []api.Block{api.TextBlock("new"), api.TextBlock("more")})
}

func TestTailer_diesWhenReplacedAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.jsonl")
	if err := os.WriteFile(path, []byte(userLine("u1", "hello")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := startTailer(t, path, &collector{})
	tmp := filepath.Join(dir, "tmp.jsonl")
	if err := os.WriteFile(tmp, []byte(userLine("u2", "other")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, tl.Dead)
}

func TestTailer_truncationStartsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.jsonl")
	if err := os.WriteFile(path, []byte(userLine("u1", "hello")+"\n"+assistantLine("a1", "one")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var c collector
	tl := startTailer(t, path, &c)
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, userLine("u9", "again")+"\n")
	waitUntil(t, func() bool { return c.count() == 1 })
	expectEqual(t, c.get()[0].ID, "u9")
	expectEqual(t, c.get()[0].Blocks, []api.Block{api.TextBlock("again")})
	if tl.Dead() {
		t.Error("truncation killed the tailer")
	}
}

func TestTailer_stopEndsCallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var c collector
	tl := NewTailer(Ref{Path: path, Format: FormatClaude}, nil, c.add)
	tl.Start()
	tl.Stop()
	tl.Stop() // idempotent
	if !tl.Dead() {
		t.Error("not dead after Stop")
	}
	appendFile(t, path, userLine("u1", "late")+"\n")
	time.Sleep(3 * safetyPoll / 2)
	if c.count() != 0 {
		t.Error("callback after Stop")
	}
	// Stop without Start doesn't block, and Start after Stop does nothing.
	never := NewTailer(Ref{Path: path, Format: FormatClaude}, nil, c.add)
	never.Stop()
	never.Start()
	if !never.Dead() {
		t.Error("not dead")
	}
}

func TestTailer_missingFileIsDead(t *testing.T) {
	tl := NewTailer(Ref{Path: filepath.Join(t.TempDir(), "none.jsonl"), Format: FormatClaude}, nil, func(api.Message) {})
	tl.Start()
	defer tl.Stop()
	if !tl.Dead() {
		t.Error("expected dead")
	}
}

func TestTailer_seedsFromTheTailOfAHugeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.jsonl")
	// Past the cap: a first line that the cut lands in, then the real tail.
	head := userLine("u0", strings.Repeat("x", 200))
	body := assistantLine("a1", "tail")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(head + "\n"); err != nil {
		t.Fatal(err)
	}
	// Sparse filler of newlines keeps the test fast; blank lines are skipped by the parser.
	if err := f.Truncate(MaxReadBytes + 100); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n" + body + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var c collector
	startTailer(t, path, &c)
	appendFile(t, path, assistantLine("a2", "grew")+"\n")
	waitUntil(t, func() bool { return c.count() == 1 })
	expectEqual(t, c.get()[0].ID, "a1")
	expectEqual(t, c.get()[0].Blocks, []api.Block{api.TextBlock("tail"), api.TextBlock("grew")})
}

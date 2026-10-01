package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Ported from HugeTranscriptTests.swift. Round-5 hardening: ">50 MB" transcripts must not crash
// and must not pull the whole file (plus every parsed message) into memory for one page.

func TestHugeTranscript_readBoundedReturnsTheWholeFileUnderTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.jsonl")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadBounded(path, 18, 1024)
	if err != nil {
		t.Fatal(err)
	}
	expectEqual(t, string(data), "line one\nline two\n")
}

func TestHugeTranscript_readBoundedTailsAndAlignsToTheNextNewlineOverTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.jsonl")
	// Ten 9-byte lines; a cap of 25 bytes lands mid-line-8, so line 8 is dropped as a fragment
	// and only lines 9 and 10 survive.
	content := ""
	for i := 1; i <= 10; i++ {
		content += fmt.Sprintf("line-%03d\n", i)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadBounded(path, int64(len(content)), 25)
	if err != nil {
		t.Fatal(err)
	}
	expectEqual(t, string(data), "line-009\nline-010\n")
	if len(data) > 25+10 {
		t.Errorf("read %d bytes", len(data))
	}
}

func TestHugeTranscript_hugeRealTranscriptDoesNotCrashAndStaysBounded(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, ProjectDirName("/Users/dev/shop-api"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "huge.jsonl")

	// A >64 MB JSONL file of real Claude assistant lines, so it's not just noise the parser skips.
	text := make([]byte, 4000)
	for i := range text {
		text[i] = 'x'
	}
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "uuid": "00000000-0000-0000-0000-000000000000", "isSidechain": false,
		"timestamp": "2026-09-24T10:00:00+00:00",
		"message":   map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}},
	})
	line = append(line, '\n')
	target := MaxReadBytes + 1<<20 // a bit over the real cap
	var batch []byte
	for i := 0; i < 200; i++ {
		batch = append(batch, line...)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var written int64
	for written < target {
		n, err := f.Write(batch)
		if err != nil {
			t.Fatal(err)
		}
		written += int64(n)
	}
	f.Close()

	// Swift goes through AgentService.transcriptMessages; its read is ReadBounded + Parse.
	start := time.Now()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ReadBounded(path, info.Size(), MaxReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > MaxReadBytes {
		t.Errorf("read %d bytes", len(data))
	}
	messages := Parse(data, FormatClaude, "/Users/dev/shop-api", nil)
	if len(messages) == 0 {
		t.Error("no messages")
	}
	if time.Since(start) > 20*time.Second {
		t.Error("too slow")
	}
}

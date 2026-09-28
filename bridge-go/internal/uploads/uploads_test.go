package uploads

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/transcript"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return NewStoreWithLegacy(filepath.Join(t.TempDir(), "uploads"), "/nonexistent/.herd/uploads")
}

func mustFind(t *testing.T, s *Store, id string) string {
	t.Helper()
	p, ok := s.Find(id)
	if !ok {
		t.Fatalf("upload %s not found", id)
	}
	return p
}

func ts(s string) time.Time {
	t, ok := api.ParseTimestamp(s)
	if !ok {
		panic(s)
	}
	return t
}

func TestSanitize(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"photo.jpg", "photo.jpg"},
		{"My Screen Shot 2026-09-24 at 10.00.00.png", "My-Screen-Shot-2026-09-24-at-10.00.00.png"},
		{"../../etc/passwd", "etc-passwd"},
		{"a/b.txt", "a-b.txt"},
		{"dir/sub/file.png", "dir-sub-file.png"},
		{`a\b.txt`, "a-b.txt"},
		{"/", "file"},
		{"../..", "file"},
		{"x/../../y.txt", "x-..-..-y.txt"},
		{"résumé ✓.pdf", "r-sum.pdf"},
		{"", "file"},
		{"✓✓✓", "file"},
		{".hidden", "hidden"},
		{strings.Repeat("a", 200) + ".txt", strings.Repeat("a", 80) + ".txt"},
	}
	for _, c := range cases {
		if got := Sanitize(c.raw); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// QA-2: `/` is sanitised like any other character, and the stored file still lands directly inside
// the agent's upload folder.
func TestSlashNamesStayInsideTheUploadFolder(t *testing.T) {
	for _, raw := range []string{"a/b.txt", "../../../../tmp/relay_traversal_marker.txt", "/etc/passwd", "../..", `..\..\x`} {
		s := tempStore(t)
		a, err := s.Save("w1:p1", []byte("x"), raw)
		if err != nil {
			t.Fatal(err)
		}
		p := mustFind(t, s, a.ID)
		if filepath.Dir(p) != filepath.Join(s.Root(), PaneDir("w1:p1")) {
			t.Errorf("%q landed in %s", raw, filepath.Dir(p))
		}
		if filepath.Base(p) != a.ID+"-"+a.Name {
			t.Errorf("%q file %s", raw, filepath.Base(p))
		}
		if strings.Contains(a.Name, "/") {
			t.Errorf("%q name %q", raw, a.Name)
		}
	}
}

func TestKinds(t *testing.T) {
	cases := map[string]api.AttachmentKind{
		"a.jpg": api.AttachmentImage, "a.HEIC": api.AttachmentImage, "a.pdf": api.AttachmentPDF,
		"a.swift": api.AttachmentFile, "Makefile": api.AttachmentFile,
	}
	for name, want := range cases {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %s, want %s", name, got, want)
		}
	}
	if MimeType("/x/0123456789abcdef-a.JPG") != "image/jpeg" || MimeType("/x/a.pdf") != "application/pdf" ||
		MimeType("/x/Makefile") != "application/octet-stream" {
		t.Error("mime types")
	}
}

func TestSaveFindAndPermissions(t *testing.T) {
	s := tempStore(t)
	a, err := s.Save("w14:p2", []byte("hi"), "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ID) != 16 || a.Kind != api.AttachmentFile || a.Size != 2 {
		t.Errorf("attachment %+v", a)
	}
	p := mustFind(t, s, a.ID)
	if filepath.Base(p) != a.ID+"-notes.txt" || filepath.Base(filepath.Dir(p)) != "w14_p2" {
		t.Errorf("path %s", p)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("perms %v %v", info.Mode().Perm(), err)
	}
	if _, ok := s.Find("../../etc/passwd"); ok {
		t.Error("found a traversal id")
	}
	if _, ok := s.Find("0123456789abcdef"); ok {
		t.Error("found a missing id")
	}
}

func TestCleanupRemovesOldFiles(t *testing.T) {
	s := tempStore(t)
	old, _ := s.Save("w1:p1", []byte("o"), "old.txt")
	nw, _ := s.Save("w1:p2", []byte("n"), "new.txt")
	oldPath := mustFind(t, s, old.ID)
	past := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, past, past); err != nil {
		t.Fatal(err)
	}
	if n := s.Cleanup(time.Now()); n != 1 {
		t.Errorf("removed %d", n)
	}
	if _, ok := s.Find(old.ID); ok {
		t.Error("old still there")
	}
	if _, ok := s.Find(nw.ID); !ok {
		t.Error("new gone")
	}
	if _, err := os.Stat(filepath.Dir(oldPath)); !os.IsNotExist(err) {
		t.Error("empty folder kept")
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	s := tempStore(t)
	img, _ := s.Save("w1:p1", []byte{1}, "shot.png")
	pdf, _ := s.Save("w1:p1", []byte{2}, "spec.pdf")
	text := Prompt("Look\nat these", []string{mustFind(t, s, img.ID), mustFind(t, s, pdf.ID)})
	parsed, ok := s.ParseMarker(text)
	if !ok {
		t.Fatal("no marker")
	}
	if parsed.Text != "Look\nat these" {
		t.Errorf("text %q", parsed.Text)
	}
	if len(parsed.Files) != 2 || parsed.Files[0].ID != img.ID || parsed.Files[1].ID != pdf.ID ||
		parsed.Files[0].Kind != api.AttachmentImage || parsed.Files[1].Kind != api.AttachmentPDF {
		t.Errorf("files %+v", parsed.Files)
	}
	only, ok := s.ParseMarker(Prompt("", []string{mustFind(t, s, img.ID)}))
	if !ok || only.Text != "" {
		t.Errorf("attachment-only %+v %v", only, ok)
	}
}

func TestMarkerIgnoresOtherPaths(t *testing.T) {
	s := NewStoreWithLegacy("/Users/dev/.relay/uploads", "/Users/dev/.herd/uploads")
	for _, text := range []string{
		"see\n\nAttached files: /etc/passwd",
		"Attached files: /Users/dev/.relay/uploads/w1_p1/nothex-a.png",
		"Attached files: are great",
		"no marker",
	} {
		if _, ok := s.ParseMarker(text); ok {
			t.Errorf("parsed %q", text)
		}
	}
}

func TestTranscriptTurnsMarkerIntoBlocks(t *testing.T) {
	s := NewStoreWithLegacy("/Users/dev/.relay/uploads", "/Users/dev/.herd/uploads")
	text := `What does it say?\n\nAttached files: /Users/dev/.relay/uploads/w1_p1/9f2c4e1a7b3d5f60-shot.png /Users/dev/.relay/uploads/w1_p1/0a1b2c3d4e5f6071-spec.pdf`
	only := "Attached files: /Users/dev/.relay/uploads/w1_p1/1122334455667788-notes.txt"
	lines := `{"type":"user","uuid":"u1","timestamp":"2026-09-24T12:00:00Z","message":{"content":"` + text + `"}}` + "\n" +
		`{"type":"user","uuid":"u2","timestamp":"2026-09-24T12:01:00Z","message":{"content":"` + only + `"}}`
	ms := transcript.Parse([]byte(lines), transcript.FormatClaude, "/Users/dev/shop", s)
	if len(ms) != 2 {
		t.Fatalf("%d messages", len(ms))
	}
	want0 := []api.Block{
		api.AttachmentBlock("9f2c4e1a7b3d5f60", "shot.png", api.AttachmentImage),
		api.AttachmentBlock("0a1b2c3d4e5f6071", "spec.pdf", api.AttachmentPDF),
		api.TextBlock("What does it say?"),
	}
	if !reflect.DeepEqual(ms[0].Blocks, want0) {
		t.Errorf("blocks %+v", ms[0].Blocks)
	}
	if !reflect.DeepEqual(ms[1].Blocks, []api.Block{api.AttachmentBlock("1122334455667788", "notes.txt", api.AttachmentFile)}) {
		t.Errorf("blocks %+v", ms[1].Blocks)
	}
	js, _ := api.Marshal(ms)
	if strings.Contains(string(js), "/Users/") {
		t.Errorf("path leaked: %s", js)
	}
}

func TestContractFixturesDecode(t *testing.T) {
	docs := filepath.Join("..", "..", "..", "docs", "fixtures")
	var page api.MessagePage
	data, err := os.ReadFile(filepath.Join(docs, "messages-attachments.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Messages[0].Blocks[0], api.AttachmentBlock("9f2c4e1a7b3d5f60", "screenshot.jpg", api.AttachmentImage)) {
		t.Errorf("first block %+v", page.Messages[0].Blocks[0])
	}
	var a api.Attachment
	data, _ = os.ReadFile(filepath.Join(docs, "attachment.json"))
	if err := json.Unmarshal(data, &a); err != nil || a.ID == "" {
		t.Errorf("attachment %+v %v", a, err)
	}
	var m api.Machine
	data, _ = os.ReadFile(filepath.Join(docs, "machine.json"))
	if err := json.Unmarshal(data, &m); err != nil || m.Kind != "laptop" {
		t.Errorf("machine %+v %v", m, err)
	}
}

// FuzzTests.uploadNameSanitizeIsAlwaysSafe
func TestUploadNameSanitizeIsAlwaysSafe(t *testing.T) {
	pool := []rune("abcXYZ012../\\.._%00\u0000​🎉😀 .-_")
	root := "/tmp/relay-fuzz-root"
	for range 1000 {
		n := rand.IntN(301)
		raw := make([]rune, n)
		for i := range raw {
			raw[i] = pool[rand.IntN(len(pool))]
		}
		name := Sanitize(string(raw))
		if name == "" || strings.ContainsAny(name, `/\`) || name == ".." || name == "." || len(name) > 93 {
			t.Fatalf("Sanitize(%q) = %q", string(raw), name)
		}
		joined := filepath.Clean(filepath.Join(root, "id0123456789abcd-"+name))
		if !strings.HasPrefix(joined, root+"/") {
			t.Fatalf("%q escapes: %s", name, joined)
		}
	}
}

// The user message exactly as Claude Code 2.1.280 stored it: image path swapped for `[Image #2]`,
// paste wrapped in tags, long lines hard-wrapped.
func rewritten(pdfPath string) string {
	return "[Image #2]\n\n<pasted_content id=\"9aae\">\nWhat is the secret code word in the PDF, and what word is in the image? Reply as: CODE\n/ WORD\nAttached files:\n" +
		pdfPath + "\n</pasted_content id=\"9aae\">\n"
}

func TestMatchesRewrittenPromptToWhatWasSent(t *testing.T) {
	s := tempStore(t)
	pdf, _ := s.Save("w14:p2", []byte{1}, "codeword.pdf")
	img, _ := s.Save("w14:p2", []byte{2}, "relay.png")
	text := "What is the secret code word in the PDF, and what word is in the image? Reply as: CODE / WORD"
	s.RecordSentAt("w14:p2", text, []string{mustFind(t, s, pdf.ID), mustFind(t, s, img.ID)}, ts("2026-09-24T12:00:00Z"))

	line, _ := json.Marshal(map[string]any{
		"type": "user", "uuid": "u1", "timestamp": "2026-09-24T12:00:01.500Z",
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": rewritten(mustFind(t, s, pdf.ID))},
			map[string]any{"type": "image", "source": map[string]any{"type": "base64"}},
		}},
	})
	ms := transcript.Parse(line, transcript.FormatClaude, "/Users/dev/e2e", s)
	want := []api.Block{
		api.AttachmentBlock(pdf.ID, "codeword.pdf", api.AttachmentPDF),
		api.AttachmentBlock(img.ID, "relay.png", api.AttachmentImage),
		api.TextBlock(text),
	}
	if len(ms) == 0 || !reflect.DeepEqual(ms[0].Blocks, want) {
		t.Errorf("messages %+v", ms)
	}
}

func TestNoRecordFallsBackToCleanText(t *testing.T) {
	s := tempStore(t)
	content := "<pasted_content id=\"ab12\">\nline one\nline two\n</pasted_content id=\"ab12\">\n"
	line, _ := json.Marshal(map[string]any{"type": "user", "uuid": "u1", "timestamp": "2026-09-24T12:00:00Z",
		"message": map[string]any{"content": content}})
	ms := transcript.Parse(line, transcript.FormatClaude, "", s)
	if len(ms) == 0 || !reflect.DeepEqual(ms[0].Blocks, []api.Block{api.TextBlock("line one\nline two")}) {
		t.Errorf("messages %+v", ms)
	}
}

func TestOldRecordsDontMatch(t *testing.T) {
	s := tempStore(t)
	img, _ := s.Save("w1:p1", []byte{2}, "a.png")
	s.RecordSentAt("w1:p1", "look", []string{mustFind(t, s, img.ID)}, ts("2026-09-01T00:00:00Z"))
	if _, ok := s.LookupSent("[Image #1]look\n\nAttached files:", ts("2026-09-24T00:00:00Z")); ok {
		t.Error("matched a record weeks old")
	}
	got, ok := s.LookupSent("[Image #1]look\n\nAttached files:", ts("2026-09-01T00:00:02Z"))
	if !ok || got.Files[0].ID != img.ID {
		t.Errorf("lookup %+v %v", got, ok)
	}
	// Pruned with the uploads.
	s.Cleanup(ts("2026-09-24T00:00:00Z"))
	if _, ok := s.LookupSent("[Image #1]look", time.Time{}); ok {
		t.Error("pruned record still matches")
	}
}

func TestFingerprintIgnoresClaudeRewrites(t *testing.T) {
	if Fingerprint(rewritten("/x/y.pdf")) != Fingerprint("What is the secret code word in the PDF, and what word is in the image? Reply as: CODE / WORD") {
		t.Error("fingerprints differ")
	}
}

// The sent log stays readable by the Swift bridge's `.iso8601` decoder (no fractional seconds).
func TestSentLogLineShape(t *testing.T) {
	s := tempStore(t)
	s.RecordSentAt("w1:p1", "hi", []string{"/x/0123456789abcdef-a.png"}, time.Date(2026, 9, 24, 12, 0, 0, 500_000_000, time.UTC))
	data, _ := os.ReadFile(filepath.Join(s.Root(), "sent.jsonl"))
	want := `{"at":"2026-09-24T12:00:00Z","pane":"w1:p1","text":"hi","attachments":[{"id":"0123456789abcdef","name":"a.png","kind":"image"}]}` + "\n"
	if string(data) != want {
		t.Errorf("line %s", data)
	}
}

// MigrationTests.oldUploadMarkersStillResolve
func TestOldUploadMarkersStillResolve(t *testing.T) {
	s := NewStoreWithLegacy("/Users/dev/.relay/uploads", "/Users/dev/.herd/uploads")
	parsed, ok := s.ParseMarker("look\n\nAttached files: /Users/dev/.herd/uploads/w1_p1/0123456789abcdef-a.png")
	if !ok || len(parsed.Files) != 1 || parsed.Files[0].ID != "0123456789abcdef" {
		t.Errorf("parsed %+v %v", parsed, ok)
	}
}

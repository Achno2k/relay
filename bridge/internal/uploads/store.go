// Package uploads keeps the files the phone attached, under `<home>/uploads/<pane>/<id>-<name>`
// (0600). Paths stay on the machine: prompts carry them to the agent, the wire only ever sees id,
// name and kind.
package uploads

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"relay/internal/api"
	"relay/internal/config"
	"relay/internal/transcript"
)

const (
	MaxBytes      = 20 << 20
	MaxPerMessage = 10
	Lifetime      = 7 * 24 * time.Hour
	markerPrefix  = "Attached files: "
)

type Store struct {
	root       string
	legacyRoot string
	// Serialises sent.jsonl appends and prunes.
	mu sync.Mutex
}

// NewStore keeps uploads under root; "" means `<relay home>/uploads`. Marker lines written before
// the rename still point at `~/.herd/uploads`, so that prefix is accepted too.
func NewStore(root string) *Store {
	if root == "" {
		root = filepath.Join(config.Home(), "uploads")
	}
	return &Store{root: clean(root), legacyRoot: clean(filepath.Join(config.LegacyHome(), "uploads"))}
}

// NewStoreWithLegacy is NewStore with an explicit legacy root (tests).
func NewStoreWithLegacy(root, legacy string) *Store {
	return &Store{root: clean(root), legacyRoot: clean(legacy)}
}

func clean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func (s *Store) Root() string { return s.root }

// MARK: Names

// Sanitize keeps `A-Z a-z 0-9 . _ -`, turns the rest into `-`, collapses runs, cuts the stem to 80.
// `/` is just another disallowed character, not a path separator: "a/b.txt" is "a-b.txt".
func Sanitize(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		c := '-'
		if r < 0x80 && (isAlnum(r) || r == '.' || r == '_' || r == '-') {
			c = r
		}
		if c == '-' && strings.HasSuffix(b.String(), "-") {
			continue
		}
		b.WriteRune(c)
	}
	out := strings.Trim(b.String(), "-.")
	e := pathExtension(out)
	stem := out
	if e != "" {
		stem = out[:len(out)-len(e)-1]
	}
	if len(stem) > 80 {
		stem = stem[:80]
	}
	stem = strings.Trim(stem, "-.")
	if stem == "" {
		stem = "file"
	}
	if e == "" {
		return stem
	}
	if len(e) > 12 {
		e = e[:12]
	}
	return stem + "." + e
}

// pathExtension is NSString.pathExtension for a name without separators.
func pathExtension(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return name[i+1:]
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// PaneDir is the folder name for a pane id: non-alphanumerics become `_`.
func PaneDir(paneID string) string {
	var b strings.Builder
	for _, r := range paneID {
		if r < 0x80 && isAlnum(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// IsID reports whether s is 16 lowercase hex characters.
func IsID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// MARK: Files

func (s *Store) Save(paneID string, data []byte, filename string) (api.Attachment, error) {
	dir := filepath.Join(s.root, PaneDir(paneID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return api.Attachment{}, err
	}
	name := Sanitize(filename)
	id := newID()
	f, err := os.OpenFile(filepath.Join(dir, id+"-"+name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return api.Attachment{}, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return api.Attachment{}, err
	}
	if err := f.Close(); err != nil {
		return api.Attachment{}, err
	}
	return api.Attachment{ID: id, Name: name, Kind: KindOf(name), Size: len(data)}, nil
}

// Find looks an upload up by id in any agent's folder. The path is built from root, so a marker
// line built from it matches root exactly.
func (s *Store) Find(id string) (string, bool) {
	if !IsID(id) {
		return "", false
	}
	dirs, _ := os.ReadDir(s.root)
	for _, d := range dirs {
		files, _ := os.ReadDir(filepath.Join(s.root, d.Name()))
		for _, f := range files {
			if strings.HasPrefix(f.Name(), id+"-") {
				return filepath.Join(s.root, d.Name(), f.Name()), true
			}
		}
	}
	return "", false
}

// Cleanup deletes uploads older than Lifetime, and folders left empty. Returns how many files went.
func (s *Store) Cleanup(now time.Time) int {
	removed := 0
	s.pruneSentLog(now)
	dirs, _ := os.ReadDir(s.root)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, d.Name())
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			m := now
			if info, err := f.Info(); err == nil {
				m = info.ModTime()
			}
			if now.Sub(m) > Lifetime && os.RemoveAll(filepath.Join(dir, f.Name())) == nil {
				removed++
			}
		}
		if left, err := os.ReadDir(dir); err == nil && len(left) == 0 {
			_ = os.Remove(dir)
		}
	}
	return removed
}

// RunCleaner deletes expired uploads at startup, then daily, until ctx is done.
func RunCleaner(ctx context.Context, s *Store, log *slog.Logger) {
	for {
		if n := s.Cleanup(time.Now()); n > 0 && log != nil {
			log.Info("removed expired uploads", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// MARK: Sent log

// SentRecord is what the bridge sent, so history can show attachments even after Claude Code
// rewrote the prompt: it swaps image paths for `[Image #N]`, wraps pastes in `<pasted_content>`
// tags and hard-wraps long lines, so the marker line doesn't survive intact.
type SentRecord struct {
	At          time.Time
	Pane        string
	Text        string
	Attachments []SentRef
}

type SentRef struct {
	ID   string             `json:"id"`
	Name string             `json:"name"`
	Kind api.AttachmentKind `json:"kind"`
}

// sentJSON is the on-disk line; `at` is ISO 8601 without fractional seconds, like Swift's
// `.iso8601` date strategy, so the file stays readable by either bridge.
type sentJSON struct {
	At          string    `json:"at"`
	Pane        string    `json:"pane"`
	Text        string    `json:"text"`
	Attachments []SentRef `json:"attachments"`
}

func (r SentRecord) line() ([]byte, error) {
	refs := r.Attachments
	if refs == nil {
		refs = []SentRef{}
	}
	return api.Marshal(sentJSON{At: r.At.UTC().Format("2006-01-02T15:04:05Z"), Pane: r.Pane, Text: r.Text, Attachments: refs})
}

func (s *Store) sentLog() string { return filepath.Join(s.root, "sent.jsonl") }

// RecordSent appends what was sent to a pane with these upload paths.
func (s *Store) RecordSent(pane, text string, files []string) {
	s.RecordSentAt(pane, text, files, time.Now())
}

func (s *Store) RecordSentAt(pane, text string, files []string, at time.Time) {
	refs := make([]SentRef, 0, len(files))
	for _, p := range files {
		file := filepath.Base(p)
		if len(file) < 17 {
			continue
		}
		name := file[17:]
		refs = append(refs, SentRef{ID: file[:16], Name: name, Kind: KindOf(name)})
	}
	line, err := SentRecord{At: at, Pane: pane, Text: text, Attachments: refs}.line()
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(s.root, 0o700)
	f, err := os.OpenFile(s.sentLog(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func (s *Store) sentRecords() []SentRecord {
	data, err := os.ReadFile(s.sentLog())
	if err != nil {
		return nil
	}
	var out []SentRecord
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		var j sentJSON
		if json.Unmarshal(sc.Bytes(), &j) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, j.At)
		if err != nil || j.Attachments == nil {
			continue
		}
		out = append(out, SentRecord{At: at, Pane: j.Pane, Text: j.Text, Attachments: j.Attachments})
	}
	return out
}

// LooksAttached reports whether a transcript user message looks like one of ours after Claude Code
// rewrote it.
func LooksAttached(text string) bool {
	return strings.Contains(text, strings.TrimSpace(markerPrefix)) || strings.Contains(text, "[Image #")
}

var imagePlaceholder = regexp.MustCompile(`\[Image #\d+\]`)

// Fingerprint compares prompt text while ignoring what Claude Code changes: pasted-content tags,
// `[Image #N]` placeholders, the marker line onwards, and all whitespace (it hard-wraps).
func Fingerprint(text string) string {
	t := transcript.StripPastedContent(text)
	t = imagePlaceholder.ReplaceAllString(t, "")
	if i := strings.Index(t, strings.TrimSpace(markerPrefix)); i >= 0 {
		t = t[:i]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, t)
}

// LookupSent is the attachments of the sent record whose text matches text, closest to at
// (within 10 minutes); a zero at means unknown, and takes the last match. Text that doesn't look
// like one of ours returns false without reading the log.
func (s *Store) LookupSent(text string, at time.Time) (transcript.Attached, bool) {
	if !LooksAttached(text) {
		return transcript.Attached{}, false
	}
	r := s.lookupSent(text, at)
	if r == nil {
		return transcript.Attached{}, false
	}
	out := transcript.Attached{Text: r.Text}
	for _, a := range r.Attachments {
		out.Files = append(out.Files, api.Attachment{ID: a.ID, Name: a.Name, Kind: a.Kind})
	}
	return out, true
}

func (s *Store) lookupSent(text string, at time.Time) *SentRecord {
	fp := Fingerprint(text)
	var matches []SentRecord
	for _, r := range s.sentRecords() {
		if Fingerprint(r.Text) == fp {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	if at.IsZero() {
		return &matches[len(matches)-1]
	}
	best, bestD := -1, math.Inf(1)
	for i, r := range matches {
		if d := math.Abs(r.At.Sub(at).Seconds()); d < bestD {
			best, bestD = i, d
		}
	}
	if bestD >= 600 {
		return nil
	}
	return &matches[best]
}

func (s *Store) pruneSentLog(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.sentLog()); err != nil {
		return
	}
	var buf bytes.Buffer
	for _, r := range s.sentRecords() {
		if now.Sub(r.At) > Lifetime {
			continue
		}
		if l, err := r.line(); err == nil {
			buf.Write(l)
			buf.WriteByte('\n')
		}
	}
	_ = os.WriteFile(s.sentLog(), buf.Bytes(), 0o600)
}

// MARK: Marker

// Prompt is what the agent receives: the text, a blank line, then `Attached files: <path> <path>`.
func Prompt(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	line := markerPrefix + strings.Join(paths, " ")
	if text == "" {
		return line
	}
	return text + "\n\n" + line
}

// ParseMarker splits our marker line off a user message. false unless every path is one of our
// uploads.
func (s *Store) ParseMarker(text string) (transcript.Attached, bool) {
	trimmed := strings.TrimFunc(text, unicode.IsSpace)
	lines := strings.Split(trimmed, "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, markerPrefix) {
		return transcript.Attached{}, false
	}
	prefixes := []string{s.root + "/", s.legacyRoot + "/"}
	if r, err := filepath.EvalSymlinks(s.root); err == nil {
		prefixes = append(prefixes, r+"/")
	}
	var out []api.Attachment
	for _, token := range strings.Fields(strings.TrimPrefix(last, markerPrefix)) {
		ok := false
		for _, p := range prefixes {
			if strings.HasPrefix(token, p) {
				ok = true
				break
			}
		}
		if !ok {
			return transcript.Attached{}, false
		}
		parts := strings.FieldsFunc(token, func(r rune) bool { return r == '/' })
		file := ""
		if len(parts) > 0 {
			file = parts[len(parts)-1]
		}
		if len(file) < 17 || !IsID(file[:16]) || file[16] != '-' {
			return transcript.Attached{}, false
		}
		name := file[17:]
		out = append(out, api.Attachment{ID: file[:16], Name: name, Kind: KindOf(name)})
	}
	if len(out) == 0 {
		return transcript.Attached{}, false
	}
	body := strings.TrimFunc(strings.Join(lines[:len(lines)-1], "\n"), unicode.IsSpace)
	return transcript.Attached{Files: out, Text: body}, true
}

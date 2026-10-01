package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CodexRollouts finds codex rollout files: `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<sessionId>.jsonl`.
type CodexRollouts struct {
	Root string

	mu   sync.Mutex
	byID map[string]string
}

// NewCodexRollouts: "" = ~/.codex/sessions.
func NewCodexRollouts(root string) *CodexRollouts {
	if root == "" {
		root = filepath.Join(homeDir(), ".codex", "sessions")
	}
	return &CodexRollouts{Root: root, byID: map[string]string{}}
}

// subdirsNewestFirst lists dir's subdirectories, newest (largest name) first.
func subdirsNewestFirst(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i]) > filepath.Base(out[j]) })
	return out
}

// days are the day folders, newest first (at most limit).
func (c *CodexRollouts) days(limit int) []string {
	var out []string
	for _, y := range subdirsNewestFirst(c.Root) {
		for _, m := range subdirsNewestFirst(y) {
			out = append(out, subdirsNewestFirst(m)...)
		}
		if len(out) >= limit {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func fileNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

// Find returns the rollout for a codex session id, or "".
func (c *CodexRollouts) Find(sessionID string) string {
	c.mu.Lock()
	hit, ok := c.byID[sessionID]
	c.mu.Unlock()
	if ok && exists(hit) {
		return hit
	}
	for _, d := range c.days(60) {
		for _, f := range fileNames(d) {
			if strings.HasPrefix(f, "rollout-") && strings.HasSuffix(f, sessionID+".jsonl") {
				path := filepath.Join(d, f)
				c.mu.Lock()
				remember(c.byID, sessionID, path)
				c.mu.Unlock()
				return path
			}
		}
	}
	return ""
}

// Newest is for when herdr has no session id yet: the newest rollout (last 2 days) started in
// cwd, created no earlier than notBefore (zero = no bound), so a new agent doesn't borrow
// another agent's rollout in the same folder.
func (c *CodexRollouts) Newest(cwd string, notBefore time.Time) string {
	best := ""
	var bestMod time.Time
	for _, d := range c.days(2) {
		for _, f := range fileNames(d) {
			if !strings.HasPrefix(f, "rollout-") || !strings.HasSuffix(f, ".jsonl") {
				continue
			}
			path := filepath.Join(d, f)
			meta, ok := readSessionMeta(path)
			if !ok || meta.cwd != cwd {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if !notBefore.IsZero() {
				created, ok := createdTime(info, meta)
				if !ok || created.Before(notBefore) {
					continue
				}
			}
			if best == "" || info.ModTime().After(bestMod) {
				best, bestMod = path, info.ModTime()
			}
		}
	}
	return best
}

type sessionMeta struct {
	cwd       string
	timestamp string
}

// readSessionMeta reads `session_meta` from the first line, looking at most 64 KB in.
func readSessionMeta(path string) (sessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return sessionMeta{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(bufio.NewReader(f), 64*1024))
	if err != nil {
		return sessionMeta{}, false
	}
	var line []byte
	for _, l := range bytes.Split(data, []byte{'\n'}) {
		if len(l) > 0 {
			line = l
			break
		}
	}
	var o struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Payload   struct {
			Cwd *string `json:"cwd"`
		} `json:"payload"`
	}
	if line == nil || json.Unmarshal(line, &o) != nil || o.Type != "session_meta" || o.Payload.Cwd == nil {
		return sessionMeta{}, false
	}
	return sessionMeta{cwd: *o.Payload.Cwd, timestamp: o.Timestamp}, true
}

package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"relay/internal/herdr"
)

// Ref is a transcript file behind an agent.
type Ref struct {
	Path   string
	Format Format
	Cwd    string
}

// Locator finds the transcript file behind a herdr agent.
// Claude: `~/.claude/projects/<cwd with non-alphanumerics as ->/<sessionId>.jsonl`. pi: the session path.
type Locator struct {
	ClaudeProjects string
	Codex          *CodexRollouts

	mu    sync.Mutex
	found map[string]string // claude session id → path, from the slow path
}

// NewLocator: "" and nil mean the defaults (~/.claude/projects, ~/.codex/sessions).
func NewLocator(claudeProjects string, codex *CodexRollouts) *Locator {
	if claudeProjects == "" {
		claudeProjects = filepath.Join(homeDir(), ".claude", "projects")
	}
	if codex == nil {
		codex = NewCodexRollouts("")
	}
	return &Locator{ClaudeProjects: claudeProjects, Codex: codex, found: map[string]string{}}
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

// maxCached bounds the session id → path caches (R8-19). They only save a directory scan.
const maxCached = 256

// remember caches path under key, first dropping entries whose file is gone and, if the cache
// is still full, everything.
func remember(cache map[string]string, key, path string) {
	if len(cache) >= maxCached {
		for k, p := range cache {
			if !exists(p) {
				delete(cache, k)
			}
		}
		if len(cache) >= maxCached {
			clear(cache)
		}
	}
	cache[key] = path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ProjectDirName is Claude's project folder name for a cwd: every character but ASCII
// letters, digits and `-` becomes `-`.
func ProjectDirName(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Locate returns the agent's transcript, or nil. notBefore: for codex's cwd fallback, ignore
// rollouts created before this (when the agent appeared); zero = no bound.
func (l *Locator) Locate(a herdr.Agent, notBefore time.Time) *Ref {
	cwd := a.CwdOrForeground()
	session := a.AgentSession
	kind := a.Agent
	if kind == "" && session != nil {
		kind = session.Agent
	}
	kind = strings.ToLower(kind)
	if kind == "codex" {
		// herdr's session id, else the newest rollout started in this folder (herdr has no id
		// until codex's first message).
		path := ""
		if session != nil {
			path = l.Codex.Find(session.Value)
		}
		if path == "" && cwd != "" {
			path = l.Codex.Newest(cwd, notBefore)
		}
		if path == "" {
			return nil
		}
		return &Ref{Path: path, Format: FormatCodex, Cwd: cwd}
	}
	if session == nil {
		return nil
	}

	if session.Kind == "path" {
		if !exists(session.Value) {
			return nil
		}
		f := FormatPi
		if kind == "claude" {
			f = FormatClaude
		}
		return &Ref{Path: session.Value, Format: f, Cwd: cwd}
	}
	if session.Kind != "id" || kind != "claude" || strings.Contains(session.Value, "/") {
		return nil
	}
	file := session.Value + ".jsonl"
	for _, dir := range []string{a.Cwd, a.ForegroundCwd} {
		if dir == "" {
			continue
		}
		path := filepath.Join(l.ClaudeProjects, ProjectDirName(dir), file)
		if exists(path) {
			return &Ref{Path: path, Format: FormatClaude, Cwd: cwd}
		}
	}
	l.mu.Lock()
	cached, ok := l.found[session.Value]
	l.mu.Unlock()
	if ok && exists(cached) {
		return &Ref{Path: cached, Format: FormatClaude, Cwd: cwd}
	}
	// Slow path: the project dir name didn't match; look in every project dir.
	entries, _ := os.ReadDir(l.ClaudeProjects)
	for _, e := range entries {
		path := filepath.Join(l.ClaudeProjects, e.Name(), file)
		if exists(path) {
			l.mu.Lock()
			remember(l.found, session.Value, path)
			l.mu.Unlock()
			return &Ref{Path: path, Format: FormatClaude, Cwd: cwd}
		}
	}
	return nil
}

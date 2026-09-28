package controls

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"relay/internal/api"
)

// State is the model, permission mode and effort of an agent, as far as its transcript and screen
// tell. nil = unknown. Swift: ControlState.
type State struct {
	Model          *string
	Effort         *string
	PermissionMode *string
}

// Merged fills gaps in s from older.
func (s State) Merged(older *State) State {
	if older == nil {
		return s
	}
	return State{Model: or(s.Model, older.Model), Effort: or(s.Effort, older.Effort), PermissionMode: or(s.PermissionMode, older.PermissionMode)}
}

func or(a, b *string) *string {
	if a != nil {
		return a
	}
	return b
}

func sp(s string) *string { return &s }

func eqp(a *string, b string) bool { return a != nil && *a == b }

func sameP(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Equal compares two states field by field.
func (s State) Equal(o State) bool {
	return sameP(s.Model, o.Model) && sameP(s.Effort, o.Effort) && sameP(s.PermissionMode, o.PermissionMode)
}

// claudeCatalog is everything the bridge offers for Claude Code (GET /controls).
var claudeCatalog = api.Controls{
	Models: []api.ControlChoice{
		{ID: "opus", Label: "Opus 5.5"},
		{ID: "sonnet", Label: "Sonnet 5"},
		{ID: "haiku", Label: "Haiku 4.5"},
		{ID: "fable", Label: "Fable 5.1"},
	},
	Modes: []api.ControlChoice{
		{ID: "default", Label: "Default"},
		{ID: "acceptEdits", Label: "Accept edits"},
		{ID: "plan", Label: "Plan"},
		{ID: "auto", Label: "Auto"},
		{ID: "bypassPermissions", Label: "Bypass permissions"},
	},
	Efforts: []api.ControlChoice{
		{ID: "low", Label: "Low"},
		{ID: "medium", Label: "Medium"},
		{ID: "high", Label: "High"},
		{ID: "xhigh", Label: "Extra high"},
		{ID: "max", Label: "Max"},
	},
}

// Catalog is GET /controls: Claude's lists, kept for older apps.
func Catalog() api.Controls {
	c := claudeCatalog
	c.Models = append([]api.ControlChoice(nil), c.Models...)
	c.Modes = append([]api.ControlChoice(nil), c.Modes...)
	c.Efforts = append([]api.ControlChoice(nil), c.Efforts...)
	return c
}

func claudeEffort(e string) bool {
	for _, c := range claudeCatalog.Efforts {
		if c.ID == e {
			return true
		}
	}
	return false
}

func choice(list []api.ControlChoice, id string) (api.ControlChoice, bool) {
	for _, c := range list {
		if c.ID == id {
			return c, true
		}
	}
	return api.ControlChoice{}, false
}

// split is Swift's `split(separator:)`: empty pieces are dropped.
func split(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func allNumbers(s string) bool {
	for _, r := range s {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// ClaudeLabel: `claude-opus-5-5` -> `Opus 5.5`, `claude-haiku-4-5-20251001` -> `Haiku 4.5`.
func ClaudeLabel(id string) *string {
	first := split(strings.ToLower(id), "[")
	if len(first) == 0 {
		return nil
	}
	parts := split(first[0], "-")
	if len(parts) < 3 || parts[0] != "claude" {
		return nil
	}
	parts = parts[1:]
	if last := parts[len(parts)-1]; len([]rune(last)) == 8 && allNumbers(last) {
		parts = parts[:len(parts)-1]
	}
	family := parts[0]
	parts = parts[1:]
	if len(parts) == 0 {
		return nil
	}
	for _, p := range parts {
		if !allNumbers(p) {
			return nil
		}
	}
	r := []rune(family)
	return sp(strings.ToUpper(string(r[:1])) + string(r[1:]) + " " + strings.Join(parts, "."))
}

// ClaudeID: `Sonnet 5` -> `claude-sonnet-5`, `Opus 5.5 (1M context)` -> `claude-opus-5-5`.
func ClaudeID(label string) *string {
	clean := label
	if p := split(label, "("); len(p) > 0 {
		clean = strings.TrimFunc(p[0], isBlank)
	}
	words := split(clean, " ")
	if len(words) != 2 {
		return nil
	}
	version := split(words[1], ".")
	for _, v := range version {
		if !allNumbers(v) {
			return nil
		}
	}
	return sp("claude-" + strings.ToLower(words[0]) + "-" + strings.Join(version, "-"))
}

// isBlank is CharacterSet.whitespaces: spaces and tabs, not newlines.
func isBlank(r rune) bool {
	return r == ' ' || r == '\t' || (unicode.Is(unicode.Zs, r))
}

func trimBlank(s string) string { return strings.TrimFunc(s, isBlank) }

var (
	setModelRe  = regexp.MustCompile("Set model to `?([^`\\n]+?)`?(?: and |\\s*$|\\s*\\(|</)")
	setEffortRe = regexp.MustCompile("Set effort level to `?([a-z]+)")
)

// ScanClaude reads transcript lines (oldest first); later lines win.
func ScanClaude(data []byte) State {
	var s State
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var o map[string]any
		if json.Unmarshal(line, &o) != nil {
			continue
		}
		switch o["type"] {
		case "assistant":
			if b, _ := o["isSidechain"].(bool); b {
				continue
			}
			if msg, ok := o["message"].(map[string]any); ok {
				if m, ok := msg["model"].(string); ok && strings.HasPrefix(m, "claude-") {
					s.Model = sp(m)
				}
			}
			if e, ok := o["effort"].(string); ok && claudeEffort(e) {
				s.Effort = sp(e)
			}
		case "permission-mode":
			if m, ok := o["permissionMode"].(string); ok {
				s.PermissionMode = sp(NormalizeMode(m))
			}
		case "user":
			msg, _ := o["message"].(map[string]any)
			text, ok := msg["content"].(string)
			if !ok || !strings.Contains(text, "<local-command-stdout>") {
				continue
			}
			if m := setModelRe.FindStringSubmatch(text); m != nil {
				if id := ClaudeID(m[1]); id != nil {
					s.Model = id
				}
			}
			if m := setEffortRe.FindStringSubmatch(text); m != nil && claudeEffort(m[1]) {
				s.Effort = sp(m[1])
			}
		}
	}
	return s
}

// NormalizeMode: Claude calls the default mode "manual" in its footer; the API calls it default.
func NormalizeMode(m string) string {
	if m == "manual" {
		return "default"
	}
	return m
}

var footerModes = []struct{ needle, mode string }{
	{"plan mode on", "plan"},
	{"accept edits on", "acceptEdits"},
	{"auto mode on", "auto"},
	{"manual mode on", "default"},
	{"default mode on", "default"},
	{"bypass permissions on", "bypassPermissions"},
	{"don't ask mode on", "dontAsk"},
	{"dontask mode on", "dontAsk"},
}

func lastLines(screen string, n int) []string {
	lines := strings.Split(screen, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// FooterMode is the permission mode from Claude's footer (`⏸ plan mode on (shift+tab to cycle)`).
func FooterMode(screen string) (string, bool) {
	lines := lastLines(screen, 6)
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.ToLower(lines[i])
		for _, f := range footerModes {
			if strings.Contains(l, f.needle) {
				return f.mode, true
			}
		}
	}
	return "", false
}

var bannerRe = regexp.MustCompile(`\b(Opus|Sonnet|Haiku|Fable) (\d+(?:\.\d+)?)(?: \([^)]*\))? with (low|medium|high|xhigh|max) effort`)

// Banner reads Claude's session banner (`Sonnet 5 with medium effort · Claude Max`), shown at the
// top of a fresh session. It covers the gap after /clear, before the first assistant message.
func Banner(screen string) *State {
	all := bannerRe.FindAllStringSubmatch(screen, -1)
	if len(all) == 0 {
		return nil
	}
	m := all[len(all)-1]
	return &State{Model: ClaudeID(m[1] + " " + m[2]), Effort: sp(m[3])}
}

var effortLineRe = regexp.MustCompile(`\b(low|medium|high|xhigh|max) · /effort`)

// ScreenEffort is the effort badge Claude shows above the input (`● high · /effort`).
func ScreenEffort(screen string) (string, bool) {
	all := effortLineRe.FindAllStringSubmatch(screen, -1)
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1][1], true
}

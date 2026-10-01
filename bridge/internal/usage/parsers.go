// Package usage polls subscription usage (Claude, ChatGPT via codex, OpenCode Go via pi), caches
// it for GET /usage and broadcasts `usage.updated`. See api.md "Usage".
package usage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"

	"relay/internal/api"
)

// num reads a JSON number the way NSNumber did: ints and doubles alike.
func num(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func obj(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// ParseCodex reads the `account/rateLimits/read` JSON-RPC response line. piReady is whether pi's
// `openai-codex` login checked out (same ChatGPT account): it adds "pi" to usedBy.
func ParseCodex(line []byte, now time.Time, piReady bool) *api.UsageProvider {
	var root map[string]any
	if json.Unmarshal(line, &root) != nil {
		return nil
	}
	result, ok := obj(root["result"])
	if !ok {
		return nil
	}
	rateLimits, ok := obj(result["rateLimits"])
	if !ok {
		return nil
	}
	windows := []api.UsageWindow{}
	if w := window(rateLimits["primary"], "primary"); w != nil {
		windows = append(windows, *w)
	}
	if w := window(rateLimits["secondary"], "secondary"); w != nil {
		windows = append(windows, *w)
	}
	var plan *string
	if p, ok := str(rateLimits["planType"]); ok {
		plan = api.Str(planLabel(p))
	}
	usedBy := []string{"codex"}
	if piReady {
		usedBy = append(usedBy, "pi")
	}
	return &api.UsageProvider{ID: "codex", Label: "ChatGPT", Plan: plan, Windows: windows,
		UpdatedAt: api.FormatTime(now), Source: "codex app-server", Stale: false, UsedBy: usedBy}
}

func window(raw any, id string) *api.UsageWindow {
	w, ok := obj(raw)
	if !ok {
		return nil
	}
	out := api.UsageWindow{ID: id}
	if p, ok := num(w["usedPercent"]); ok {
		out.UsedPercent = &p
	}
	if m, ok := num(w["windowDurationMins"]); ok {
		mins := int(m)
		out.WindowMinutes = &mins
	}
	if r, ok := num(w["resetsAt"]); ok {
		whole := int64(r)
		out.ResetsAt = api.Str(api.FormatTime(time.Unix(whole, int64((r-float64(whole))*1e9))))
	}
	out.Label = windowLabel(out.WindowMinutes, id)
	return &out
}

// windowLabel is a human label from the window's length when one is reported, else a generic
// fallback by id.
func windowLabel(minutes *int, id string) string {
	if minutes == nil || *minutes <= 0 {
		if id == "primary" {
			return "Primary"
		}
		return "Secondary"
	}
	m := *minutes
	if m <= 360 {
		return strconv.Itoa(max(1, m/60)) + "-hour"
	}
	if m <= 10_080 {
		return "Weekly"
	}
	return "Monthly"
}

func planLabel(raw string) string {
	switch raw {
	case "free":
		return "Free"
	case "go":
		return "Go"
	case "plus":
		return "Plus"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	case "business":
		return "Business"
	case "enterprise":
		return "Enterprise"
	}
	return Capitalized(raw)
}

// Capitalized is Foundation's String.capitalized: in each whitespace-separated word the first
// character is uppercased and the rest lowercased.
func Capitalized(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			start = true
			b.WriteRune(r)
			continue
		}
		if start {
			b.WriteRune(unicode.ToUpper(r))
			start = false
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// ParseClaude reads `claude -p "/usage" --output-format stream-json --verbose` stdout: one JSON
// object per line; the assistant message carries `usage_report.rate_limits.limits[]`. piReady is
// whether pi's own `anthropic` login checked out: it adds "pi" to usedBy.
func ParseClaude(usage, auth []byte, now time.Time, piReady bool) *api.UsageProvider {
	if usage == nil {
		return nil
	}
	var limits []any
	found := false
	for _, line := range bytes.Split(usage, []byte("\n")) {
		if len(line) <= 20 {
			continue
		}
		var o map[string]any
		if json.Unmarshal(line, &o) != nil {
			continue
		}
		report, ok := obj(o["usage_report"])
		if !ok {
			continue
		}
		rl, ok := obj(report["rate_limits"])
		if !ok {
			continue
		}
		l, ok := rl["limits"].([]any)
		if !ok {
			continue
		}
		if !allObjects(l) {
			continue
		}
		limits, found = l, true
	}
	if !found {
		return nil
	}
	claudeWindow := func(kind, label string) *api.UsageWindow {
		for _, raw := range limits {
			l := raw.(map[string]any)
			if k, _ := str(l["kind"]); k != kind {
				continue
			}
			w := api.UsageWindow{ID: kind, Label: label}
			if p, ok := num(l["percent"]); ok {
				w.UsedPercent = &p
			}
			if s, ok := str(l["resets_at"]); ok {
				if t, ok := api.ParseTimestamp(s); ok {
					w.ResetsAt = api.Str(api.FormatTime(t))
				}
			}
			return &w
		}
		return nil
	}
	windows := []api.UsageWindow{}
	if w := claudeWindow("session", "Session"); w != nil {
		windows = append(windows, *w)
	}
	if w := claudeWindow("weekly_all", "This week"); w != nil {
		windows = append(windows, *w)
	}
	var plan *string
	if auth != nil {
		var a map[string]any
		if json.Unmarshal(auth, &a) == nil {
			if s, ok := str(a["subscriptionType"]); ok {
				plan = api.Str(planLabel(s))
			}
		}
	}
	usedBy := []string{"claude"}
	if piReady {
		usedBy = append(usedBy, "pi")
	}
	return &api.UsageProvider{ID: "claude", Label: "Claude", Plan: plan, Windows: windows,
		UpdatedAt: api.FormatTime(now), Source: "claude -p /usage", Stale: false, UsedBy: usedBy}
}

// allObjects mirrors Swift's `as? [[String: Any]]`, which fails if any element isn't an object.
func allObjects(l []any) bool {
	for _, e := range l {
		if _, ok := e.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// OpenCodeGoProvider is built entirely from `pi auth check` (no usage API exists for OpenCode
// Go). nil when pi isn't authenticated to it, so the card is omitted rather than shown empty.
func OpenCodeGoProvider(piReady bool, now time.Time) *api.UsageProvider {
	if !piReady {
		return nil
	}
	return &api.UsageProvider{ID: "opencode-go", Label: "OpenCode Go", Plan: api.Str("OpenCode Go"),
		Windows: []api.UsageWindow{}, UpdatedAt: api.FormatTime(now), Source: "pi auth check", Stale: false,
		UnavailableReason: api.Str("Usage not available from OpenCode"), UsedBy: []string{"pi"}}
}

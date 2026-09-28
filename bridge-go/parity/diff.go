package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// decode parses JSON keeping number literals (json.Number) so 11 vs 11.0 or 1e3 vs 1000 counts as
// a difference, and keeping null keys, so "null" and "missing" stay distinct.
func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return v, nil
}

// verdict is what a rule says about two differing values.
type verdict int

const (
	reject verdict = iota // not this rule's business: still a difference
	accept                // legitimately different
	warn                  // different for a known reason worth seeing, but not a failure
)

// absent stands in for a key one side doesn't send, so rules can tell "missing" from null.
type absentKey struct{}

var absent any = absentKey{}

// rule judges two differing values at a path. It is only consulted when the values differ (a leaf,
// a key only one side sends, or arrays of different lengths). parent is the swift-side object
// holding the value (nil for array elements and the root).
type rule func(path []string, parent map[string]any, swift, gov any) verdict

// differ walks two JSON trees and records every difference no rule accepts.
type differ struct {
	rules []rule
	diffs []string
	warns []string
	max   int
}

func (d *differ) add(path []string, format string, args ...any) {
	if d.max > 0 && len(d.diffs) >= d.max {
		return
	}
	d.diffs = append(d.diffs, pathString(path)+": "+fmt.Sprintf(format, args...))
}

// judged records a warning when a rule asks for one, and reports whether the pair is settled.
func (d *differ) judged(path []string, parent map[string]any, a, b any) bool {
	for _, r := range d.rules {
		switch r(path, parent, a, b) {
		case accept:
			return true
		case warn:
			d.warns = append(d.warns, pathString(path)+": swift="+show(a)+" go="+show(b))
			return true
		}
	}
	return false
}

func (d *differ) walk(path []string, a, b any) { d.walkIn(path, nil, a, b) }

func (d *differ) walkIn(path []string, parent map[string]any, a, b any) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			d.leaf(path, parent, a, b)
			return
		}
		for _, k := range sortedKeys(av, bv) {
			x, inA := av[k]
			y, inB := bv[k]
			p := append(append([]string{}, path...), k)
			switch {
			case inA && !inB:
				if !d.judged(p, av, x, absent) {
					d.add(p, "missing in go (swift=%s)", show(x))
				}
			case !inA && inB:
				if !d.judged(p, av, absent, y) {
					d.add(p, "extra in go (go=%s)", show(y))
				}
			default:
				d.walkIn(p, av, x, y)
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			d.leaf(path, parent, a, b)
			return
		}
		if len(av) != len(bv) {
			if d.judged(path, parent, a, b) {
				return
			}
			d.add(path, "length swift=%d go=%d", len(av), len(bv))
		}
		for i := 0; i < len(av) && i < len(bv); i++ {
			d.walkIn(append(append([]string{}, path...), fmt.Sprintf("[%d]", i)), nil, av[i], bv[i])
		}
	default:
		d.leaf(path, parent, a, b)
	}
}

func (d *differ) leaf(path []string, parent map[string]any, a, b any) {
	if equalJSON(a, b) || d.judged(path, parent, a, b) {
		return
	}
	d.add(path, "swift=%s go=%s", show(a), show(b))
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb) && kind(a) == kind(b)
}

func kind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case absentKey:
		return "absent"
	}
	return fmt.Sprintf("%T", v)
}

func sortedKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var ks []string
	for k := range a {
		seen[k] = true
		ks = append(ks, k)
	}
	for k := range b {
		if !seen[k] {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

func pathString(p []string) string {
	if len(p) == 0 {
		return "$"
	}
	var sb strings.Builder
	sb.WriteString("$")
	for _, s := range p {
		if strings.HasPrefix(s, "[") {
			sb.WriteString(s)
		} else {
			sb.WriteString("." + s)
		}
	}
	return sb.String()
}

func show(v any) string {
	if v == absent {
		return "(absent)"
	}
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

func last(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

// --- Rules. Each one is a field that legitimately differs between two bridges started at
// different times. Everything else must match exactly.

// uptimeRule: /health uptimeSeconds, any non-negative integer on both sides.
func uptimeRule(path []string, _ map[string]any, a, b any) verdict {
	return when(last(path) == "uptimeSeconds" && isNonNegInt(a) && isNonNegInt(b))
}

func when(ok bool) verdict {
	if ok {
		return accept
	}
	return reject
}

// seqRule: reply.live seq is a per-bridge counter.
func seqRule(path []string, _ map[string]any, a, b any) verdict {
	return when(len(path) == 1 && path[0] == "seq" && isNonNegInt(a) && isNonNegInt(b))
}

func isNonNegInt(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	i, err := n.Int64()
	return err == nil && i >= 0
}

// clockRule: Agent.updatedAt (and a synthetic screen message's createdAt) is max(transcript mtime,
// the time this bridge saw herdr's state_change_seq move), or the time this bridge first saw the
// agent. Accepted: swift later than go (the long-running Swift bridge saw a seq change the Go one
// didn't), or go at/after its own start (its first-seen or seq-change time). Any other difference is
// a warning: the Swift bridge caches a transcript's mtime per URL, so it can lag behind the file.
// The go value must be in the contract's exact format either way.
func clockRule(goStart time.Time) rule {
	return func(path []string, parent map[string]any, a, b any) verdict {
		switch last(path) {
		case "updatedAt":
			if inUsage(path) {
				return reject
			}
		case "createdAt":
			id, _ := parent["id"].(string)
			if !strings.HasPrefix(id, "screen:") {
				return reject
			}
		default:
			return reject
		}
		sa, ok1 := a.(string)
		sb, ok2 := b.(string)
		if !ok1 || !ok2 || !wireTime.MatchString(sb) {
			return reject
		}
		ta, err1 := parseTime(sa)
		tb, err2 := parseTime(sb)
		if err1 != nil || err2 != nil {
			return reject
		}
		if ta.After(tb) || !tb.Before(goStart.Add(-2*time.Second)) {
			return accept
		}
		return warn
	}
}

// wireTime is Swift's Timestamps.format: second precision, always "+00:00".
var wireTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\+00:00$`)

// historyRule: the Swift bridge remembers each agent's last known controls (lastControls) and
// fills a field the screen doesn't show right now from it, so a fresh bridge can have null where
// Swift has a value. That is a warning; the reverse (go knows, swift doesn't) is a difference.
func historyRule(path []string, parent map[string]any, a, b any) verdict {
	switch last(path) {
	case "model", "modelLabel", "permissionMode", "effort":
	default:
		return reject
	}
	if _, ok := parent["workspaceId"]; !ok {
		return reject
	}
	if _, ok := a.(string); ok && b == nil {
		return warn
	}
	return reject
}

// usageRule: /usage and usage.updated snapshots are fetched independently by each bridge, so the
// fetch time, the numbers, the staleness and the reason can differ. Types must still match, and
// unavailableReason may be null or absent (Swift leaves a nil reason out).
func usageRule(path []string, _ map[string]any, a, b any) verdict {
	if !inUsage(path) {
		return reject
	}
	switch last(path) {
	case "updatedAt", "resetsAt":
		return when(sameKindOrNull(a, b) && timeOrNull(a) && timeOrNull(b))
	case "windows":
		// A window list that appeared or emptied between two independent fetches.
		return when(kind(a) == "array" && kind(b) == "array")
	case "usedPercent", "stale":
		return when(sameKindOrNull(a, b))
	case "unavailableReason":
		return when(stringOrNothing(a) && stringOrNothing(b))
	}
	return reject
}

func stringOrNothing(v any) bool {
	switch v.(type) {
	case nil, string, absentKey:
		return true
	}
	return false
}

func inUsage(path []string) bool {
	for _, p := range path {
		if p == "providers" || p == "provider" {
			return true
		}
	}
	return false
}

func sameKindOrNull(a, b any) bool {
	return kind(a) == kind(b) || a == nil || b == nil
}

func timeOrNull(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && wireTime.MatchString(s)
}

// parseTime accepts the contract's ISO 8601 with an explicit offset (fractional seconds allowed).
func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

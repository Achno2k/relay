package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Known fixes: bugs filed in docs/qa/round8.md that the Go port fixes on purpose, so its output
// differs from the frozen Swift bridge. Each one is either a transform applied to Swift's output
// before a strict compare, or a rule that accepts exactly that change. Every hit is reported as a
// warning named after the bug, so the fix stays visible.

// ---- R8-9: injected user lines. Go drops `<task-notification`, `<bash-stdout>`, `<bash-stderr>`
// user texts and turns `<bash-input>cmd</bash-input>` into `! cmd`. A user message left empty
// disappears, and the assistant messages around it merge (first id and createdAt, blocks joined),
// exactly as the parser merges consecutive assistant lines.

var r89Dropped = []string{"<task-notification", "<bash-stdout>", "<bash-stderr>"}

// fixUserBlocks applies R8-9 to one user message's blocks. It returns the new blocks and whether
// anything changed.
func fixUserBlocks(blocks []any) ([]any, bool) {
	var out []any
	changed := false
	for _, b := range blocks {
		m, _ := b.(map[string]any)
		text, isText := m["text"].(string)
		if m["type"] != "text" || !isText {
			out = append(out, b)
			continue
		}
		t := strings.TrimSpace(text)
		if hasPrefixAny(t, r89Dropped) {
			changed = true
			continue
		}
		if rest, ok := strings.CutPrefix(t, "<bash-input>"); ok {
			cmd, _, _ := strings.Cut(rest, "</bash-input>")
			out = append(out, map[string]any{"type": "text", "text": "! " + cmd})
			changed = true
			continue
		}
		out = append(out, b)
	}
	return out, changed
}

func hasPrefixAny(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// fixMessages applies R8-9 to a message list, returning the new list and how many user messages
// changed.
func fixMessages(msgs []any) ([]any, int) {
	var out []any
	hits := 0
	for _, x := range msgs {
		m, ok := x.(map[string]any)
		if !ok {
			out = append(out, x)
			continue
		}
		if m["role"] == "user" {
			blocks, _ := m["blocks"].([]any)
			nb, changed := fixUserBlocks(blocks)
			if changed {
				hits++
				if len(nb) == 0 {
					continue
				}
				c := copyMap(m)
				c["blocks"] = nb
				m = c
			}
		}
		if n := len(out); n > 0 && m["role"] == "assistant" {
			if prev, ok := out[n-1].(map[string]any); ok && prev["role"] == "assistant" {
				c := copyMap(prev)
				pb, _ := prev["blocks"].([]any)
				mb, _ := m["blocks"].([]any)
				c["blocks"] = append(append([]any{}, pb...), mb...)
				out[n-1] = c
				continue
			}
		}
		out = append(out, m)
	}
	if out == nil {
		out = []any{}
	}
	return out, hits
}

// fixPage applies R8-9 to a /messages page. Pages hold the newest N messages, so after dropping
// messages the two sides can start at different points: both are trimmed to start at the first
// message id they share, and hasMore is no longer comparable.
func fixPage(swift, gov any) (any, any, []string) {
	sa, ok1 := swift.(map[string]any)
	ga, ok2 := gov.(map[string]any)
	if !ok1 || !ok2 {
		return swift, gov, nil
	}
	sm, ok1 := sa["messages"].([]any)
	gm, ok2 := ga["messages"].([]any)
	if !ok1 || !ok2 {
		return swift, gov, nil
	}
	fixed, hits := fixMessages(sm)
	if hits == 0 {
		return swift, gov, nil
	}
	ids := func(ms []any) map[string]bool {
		out := map[string]bool{}
		for _, m := range ms {
			if mm, ok := m.(map[string]any); ok {
				id, _ := mm["id"].(string)
				out[id] = true
			}
		}
		return out
	}
	inGo, inSwift := ids(gm), ids(fixed)
	trim := func(ms []any, other map[string]bool) []any {
		for i, m := range ms {
			mm, _ := m.(map[string]any)
			if id, _ := mm["id"].(string); other[id] {
				return ms[i:]
			}
		}
		return []any{}
	}
	fixed, gm = trim(fixed, inGo), trim(gm, inSwift)
	// The first shared message may have lost blocks merged from before the page boundary.
	if len(fixed) > 0 && len(gm) > 0 {
		fs, _ := fixed[0].(map[string]any)
		gs, _ := gm[0].(map[string]any)
		fb, _ := fs["blocks"].([]any)
		gb, _ := gs["blocks"].([]any)
		if fs["role"] == "assistant" && len(gb) > len(fb) {
			c := copyMap(gs)
			c["blocks"] = gb[len(gb)-len(fb):]
			gm = append([]any{c}, gm[1:]...)
		}
	}
	cs, cg := copyMap(sa), copyMap(ga)
	cs["messages"], cg["messages"] = fixed, gm
	cs["hasMore"], cg["hasMore"] = true, true
	return cs, cg, []string{fmt.Sprintf("R8-9: %d injected user message(s) dropped or rewritten on the swift side before comparing", hits)}
}

// fixUpserted applies R8-9 to a message.upserted frame. It returns nil when Go wouldn't send it.
func fixUpserted(v map[string]any) map[string]any {
	m, ok := v["message"].(map[string]any)
	if !ok || m["role"] != "user" {
		return v
	}
	blocks, _ := m["blocks"].([]any)
	nb, changed := fixUserBlocks(blocks)
	if !changed {
		return v
	}
	if len(nb) == 0 {
		return nil
	}
	cm := copyMap(m)
	cm["blocks"] = nb
	cv := copyMap(v)
	cv["message"] = cm
	return cv
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// ---- R8-8: `file:///abs/path` and `host:/abs/path` are scrubbed in Go, not in Swift. Accepted
// when the two strings differ only in whitespace-separated words where the swift word holds such a
// path, and the go word no longer holds an absolute home path. A summary cut with "…" may end at a
// different word.

// A `host:/path` or `host:~/path` word, not a `scheme://` URL.
var r88Path = regexp.MustCompile(`file://|[^\s:/]:(?:/(?:[^/]|$)|~/)`)

func r88Rule(path []string, _ map[string]any, a, b any) verdict {
	sa, ok1 := a.(string)
	sb, ok2 := b.(string)
	if !ok1 || !ok2 || !r88Path.MatchString(sa) {
		return reject
	}
	wa, wb := strings.Fields(sa), strings.Fields(sb)
	cut := strings.HasSuffix(sa, "…") || strings.HasSuffix(sb, "…")
	if len(wa) != len(wb) && !cut {
		return reject
	}
	n := min(len(wa), len(wb))
	for i := 0; i < n; i++ {
		if wa[i] == wb[i] {
			continue
		}
		last := i == n-1 && cut
		if !last && (!r88Path.MatchString(wa[i]) || strings.Contains(wb[i], "/Users/") || strings.Contains(wb[i], "/home/")) {
			return reject
		}
	}
	return "R8-8"
}

// ---- R8-18: a WS upgrade with a missing or bad token gets `401` + the JSON error in Go, where
// Swift answered a bodiless `400`.

func r818(p probe, a, b reply) (bool, []string, []string) {
	if !strings.HasPrefix(p.path, "/ws") || a.status != 400 || len(a.body) != 0 {
		return false, nil, nil
	}
	v, err := decode(b.body)
	m, _ := v.(map[string]any)
	e, _ := m["error"].(map[string]any)
	if b.status != 401 || err != nil || e["code"] != "unauthorized" {
		return true, []string{fmt.Sprintf("R8-18: want 401 {\"error\":{\"code\":\"unauthorized\"}}, go=%d %q", b.status, clip(b.body))}, nil
	}
	return true, nil, []string{"R8-18: swift 400 bodiless, go 401 JSON"}
}

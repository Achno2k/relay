package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type frame struct {
	at  time.Time
	raw []byte
	v   map[string]any
}

// stream collects one bridge's /ws frames.
type stream struct {
	name   string
	mu     sync.Mutex
	frames []frame
	err    error
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *stream) snapshot() []frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]frame(nil), s.frames...)
}

func dialWS(ctx context.Context, b *bridge) (*stream, error) {
	u := strings.Replace(b.base, "http", "ws", 1) + "/ws?token=" + url.QueryEscape(b.token)
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s /ws: %w", b.name, err)
	}
	c.SetReadLimit(64 << 20)
	rctx, cancel := context.WithCancel(context.Background())
	s := &stream{name: b.name, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer c.CloseNow()
		for {
			typ, data, err := c.Read(rctx)
			if err != nil {
				if rctx.Err() == nil {
					s.mu.Lock()
					s.err = err
					s.mu.Unlock()
				}
				return
			}
			f := frame{at: time.Now(), raw: data}
			if typ == websocket.MessageText {
				if v, err := decode(data); err == nil {
					f.v, _ = v.(map[string]any)
				}
			}
			s.mu.Lock()
			s.frames = append(s.frames, f)
			s.mu.Unlock()
		}
	}()
	return s, nil
}

func (s *stream) close() {
	s.cancel()
	<-s.done
}

type wsWatch struct {
	swift, gov *stream
	started    time.Time
}

func (rn *runner) startWS(ctx context.Context) *wsWatch {
	a, errA := dialWS(ctx, rn.swift)
	b, errB := dialWS(ctx, rn.gov)
	if errA != nil || errB != nil {
		res := result{name: "ws connect"}
		for _, e := range []error{errA, errB} {
			if e != nil {
				res.diffs = append(res.diffs, e.Error())
			}
		}
		rn.rep.add(res)
		for _, s := range []*stream{a, b} {
			if s != nil {
				s.close()
			}
		}
		return nil
	}
	return &wsWatch{swift: a, gov: b, started: time.Now()}
}

// finishWS closes both streams and compares what they carried.
func (rn *runner) finishWS(w *wsWatch) {
	time.Sleep(3 * time.Second) // let the slower bridge catch up
	w.swift.close()
	w.gov.close()
	rn.compareStreams("ws passive", w, nil, false)
}

// compareStreams diffs two frame logs. focus limits the comparison to one agent (e2e runs).
//
// Timing differs between two independent bridges, so frames are compared by what they converge
// to, not one by one:
//   - the first frame is exactly {"type":"hello"} on both;
//   - every frame's shape (keys, types, nulls) is checked against the contract;
//   - message.upserted: the last version of each (agent, message id) must match;
//   - agent.updated: each agent's sequence of distinct states (updatedAt aside) must match;
//   - agent.created / agent.closed: the same set of agent ids;
//   - reply.live: seq only increases, the same non-generic tool summaries show up, and the last
//     frame for the agent clears both fields when the other side's does;
//   - usage.updated: the same provider ids.
//
// strictLive makes a reply.live tool-set difference a failure. Passive watching of agents that are
// mid-turn when the window opens or closes can't be strict about it.
func (rn *runner) compareStreams(name string, w *wsWatch, focus *string, strictLive bool) {
	fa, fb := w.swift.snapshot(), w.gov.snapshot()
	res := result{name: name}
	for _, s := range []*stream{w.swift, w.gov} {
		if s.err != nil {
			res.diffs = append(res.diffs, fmt.Sprintf("%s stream ended early: %v", s.name, s.err))
		}
	}
	for _, pair := range []struct {
		n  string
		fs []frame
	}{{"swift", fa}, {"go", fb}} {
		if len(pair.fs) == 0 || string(pair.fs[0].raw) != `{"type":"hello"}` {
			first := ""
			if len(pair.fs) > 0 {
				first = string(pair.fs[0].raw)
			}
			res.diffs = append(res.diffs, fmt.Sprintf("%s first frame %q, want {\"type\":\"hello\"}", pair.n, first))
		}
		for _, f := range pair.fs {
			if f.v == nil {
				res.diffs = append(res.diffs, fmt.Sprintf("%s sent a non-JSON frame: %q", pair.n, clip(f.raw)))
			}
		}
	}
	// Frames near the window edges may have landed on one side only.
	edge := 2 * time.Second
	start, end := w.started.Add(edge), time.Now().Add(-5*time.Second)
	ga, gb := groupFrames(fixFrames(fa), focus), groupFrames(fb, focus)
	res.diffs = append(res.diffs, rn.diffGroups(ga, gb, start, end, strictLive, &res.warns)...)
	res.warns = append(res.warns, fmt.Sprintf("frames swift=%d go=%d (%s)", len(fa), len(fb), countTypes(fa, fb)))
	rn.rep.add(res)
}

// fixFrames applies the known fixes to swift's frames (R8-9: injected user messages).
func fixFrames(fs []frame) []frame {
	var out []frame
	for _, f := range fs {
		if f.v != nil && f.v["type"] == "message.upserted" {
			v := fixUpserted(f.v)
			if v == nil {
				continue
			}
			f.v = v
		}
		out = append(out, f)
	}
	return out
}

type group struct {
	frames []frame
}

func (g *group) first() time.Time { return g.frames[0].at }

// groupFrames keys frames by type and subject.
func groupFrames(fs []frame, focus *string) map[string]*group {
	out := map[string]*group{}
	for _, f := range fs {
		if f.v == nil {
			continue
		}
		typ, _ := f.v["type"].(string)
		agent := frameAgent(f.v)
		if focus != nil && typ != "hello" && typ != "usage.updated" && agent != *focus {
			continue
		}
		key := typ + " " + agent
		switch typ {
		case "message.upserted":
			if m, ok := f.v["message"].(map[string]any); ok {
				id, _ := m["id"].(string)
				key += " " + id
			}
		case "usage.updated":
			if p, ok := f.v["provider"].(map[string]any); ok {
				id, _ := p["id"].(string)
				key += " " + id
			}
		}
		g := out[key]
		if g == nil {
			g = &group{}
			out[key] = g
		}
		g.frames = append(g.frames, f)
	}
	return out
}

func frameAgent(v map[string]any) string {
	if id, ok := v["agentId"].(string); ok {
		return id
	}
	if a, ok := v["agent"].(map[string]any); ok {
		id, _ := a["id"].(string)
		return id
	}
	return ""
}

func (rn *runner) diffGroups(ga, gb map[string]*group, start, end time.Time, strictLive bool, warns *[]string) []string {
	var diffs []string
	keys := map[string]bool{}
	for k := range ga {
		keys[k] = true
	}
	for k := range gb {
		keys[k] = true
	}
	var sorted []string
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		a, b := ga[k], gb[k]
		typ := strings.SplitN(k, " ", 2)[0]
		if typ == "hello" {
			continue
		}
		if a == nil || b == nil {
			only, g := "swift", a
			if a == nil {
				only, g = "go", b
			}
			if g.first().Before(start) || g.first().After(end) {
				continue
			}
			// usage polls run on each bridge's own timer; a lone clearing reply.live ends a preview
			// that started before the window.
			if typ == "usage.updated" || (typ == "reply.live" && (allCleared(g.frames) || !strictLive)) {
				*warns = append(*warns, fmt.Sprintf("%s: only %s sent it", k, only))
				continue
			}
			diffs = append(diffs, fmt.Sprintf("%s: only %s sent it (%d frames), e.g. %s", k, only, len(g.frames), clip(g.frames[len(g.frames)-1].raw)))
			continue
		}
		switch typ {
		case "message.upserted", "usage.updated", "agent.created", "agent.closed":
			d := &differ{rules: append(append([]rule{}, rn.base...), seqRule), max: 10}
			d.walk(nil, a.frames[len(a.frames)-1].v, b.frames[len(b.frames)-1].v)
			for _, x := range d.diffs {
				diffs = append(diffs, k+" (last): "+x)
			}
			for _, x := range d.warns {
				*warns = append(*warns, k+" (last): "+x)
			}
		case "agent.updated":
			d := rn.diffStates(k, a.frames, b.frames, warns)
			// An agent created and closed inside the window never settles; its frames are
			// snapshots from different moments.
			if !strictLive && (ga["agent.closed "+frameAgent(a.frames[0].v)] != nil || gb["agent.closed "+frameAgent(a.frames[0].v)] != nil) {
				*warns = append(*warns, d...)
				d = nil
			}
			diffs = append(diffs, d...)
		case "reply.live":
			diffs = append(diffs, diffLive(k, a.frames, b.frames, strictLive, warns)...)
		default:
			diffs = append(diffs, fmt.Sprintf("%s: unknown frame type", k))
		}
		if len(a.frames) != len(b.frames) && typ != "reply.live" {
			*warns = append(*warns, fmt.Sprintf("%s: swift sent %d, go sent %d", k, len(a.frames), len(b.frames)))
		}
	}
	return diffs
}

// diffStates compares each side's sequence of distinct agent states, ignoring updatedAt (a
// bridge-local clock) when deciding what's distinct, then diffs the final states with the rules.
//
// One trail being a suffix of the other is only a warning: a bridge can emit a state from just
// before the window, or its first poll after starting.
func (rn *runner) diffStates(k string, fa, fb []frame, warns *[]string) []string {
	sa, sb := distinctStates(fa), distinctStates(fb)
	var diffs []string
	ta, tb := stateTrail(sa), stateTrail(sb)
	switch {
	case ta == tb:
	case strings.HasSuffix(ta, ">"+tb) || strings.HasSuffix(tb, ">"+ta):
		*warns = append(*warns, fmt.Sprintf("%s: trails swift=%s go=%s", k, ta, tb))
	default:
		diffs = append(diffs, fmt.Sprintf("%s: trails swift=%s go=%s", k, ta, tb))
	}
	if len(sa) > 0 && len(sb) > 0 {
		d := &differ{rules: rn.base, max: 10}
		d.walk(nil, sa[len(sa)-1], sb[len(sb)-1])
		for _, x := range d.diffs {
			diffs = append(diffs, k+" (last): "+x)
		}
		for _, x := range d.warns {
			*warns = append(*warns, k+" (last): "+x)
		}
	}
	return diffs
}

func distinctStates(fs []frame) []map[string]any {
	var out []map[string]any
	prev := ""
	for _, f := range fs {
		key := stateKey(f.v)
		if key == prev {
			continue
		}
		prev = key
		out = append(out, f.v)
	}
	return out
}

func stateKey(v map[string]any) string {
	a, _ := v["agent"].(map[string]any)
	c := map[string]any{}
	for k, x := range a {
		if k != "updatedAt" {
			c[k] = x
		}
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func stateTrail(ss []map[string]any) string {
	var parts []string
	for _, s := range ss {
		a, _ := s["agent"].(map[string]any)
		parts = append(parts, fmt.Sprint(a["status"]))
	}
	return strings.Join(parts, ">")
}

// diffLive checks reply.live frames. Screen reads happen on each bridge's own clock, so text is
// compared loosely (warnings), and structure strictly.
func diffLive(k string, fa, fb []frame, strict bool, warns *[]string) []string {
	var diffs []string
	for _, side := range []struct {
		n  string
		fs []frame
	}{{"swift", fa}, {"go", fb}} {
		var prev int64 = -1
		for _, f := range side.fs {
			for key := range f.v {
				switch key {
				case "type", "agentId", "seq", "text", "tool":
				default:
					diffs = append(diffs, fmt.Sprintf("%s: %s frame has unexpected key %q", k, side.n, key))
				}
			}
			n, ok := f.v["seq"].(json.Number)
			seq, err := n.Int64()
			if !ok || err != nil {
				diffs = append(diffs, fmt.Sprintf("%s: %s seq not an integer: %s", k, side.n, clip(f.raw)))
				continue
			}
			if seq <= prev {
				diffs = append(diffs, fmt.Sprintf("%s: %s seq went %d -> %d", k, side.n, prev, seq))
			}
			prev = seq
			if t, ok := f.v["text"].(string); ok && t == "" {
				msg := fmt.Sprintf("%s: %s sent an empty text (want null): %s", k, side.n, clip(f.raw))
				if side.n == "go" {
					diffs = append(diffs, msg)
				} else {
					*warns = append(*warns, msg)
				}
			}
			if t, ok := f.v["tool"].(map[string]any); ok {
				for _, key := range []string{"name", "summary", "state"} {
					if _, ok := t[key].(string); !ok {
						diffs = append(diffs, fmt.Sprintf("%s: %s tool.%s missing or not a string: %s", k, side.n, key, clip(f.raw)))
					}
				}
			}
		}
	}
	// "only when text or tool actually changed" (api.md Live reply).
	if n := repeats(fb); n > 0 {
		diffs = append(diffs, fmt.Sprintf("%s: go sent %d frame(s) identical to the one before", k, n))
	}
	if n := repeats(fa); n > 0 {
		*warns = append(*warns, fmt.Sprintf("%s: swift sent %d frame(s) identical to the one before", k, n))
	}
	ta, tb := liveTools(fa), liveTools(fb)
	ta, tb = dropPartial(ta, tb), dropPartial(tb, ta)
	if strings.Join(ta, "|") != strings.Join(tb, "|") {
		msg := fmt.Sprintf("%s: tools swift=%q go=%q", k, ta, tb)
		if strict {
			diffs = append(diffs, msg)
		} else {
			*warns = append(*warns, msg)
		}
	}
	la, lb := fa[len(fa)-1].v, fb[len(fb)-1].v
	if cleared(la) != cleared(lb) {
		msg := fmt.Sprintf("%s: last frame swift=%s go=%s", k, clip(fa[len(fa)-1].raw), clip(fb[len(fb)-1].raw))
		if strict {
			diffs = append(diffs, msg)
		} else {
			*warns = append(*warns, msg)
		}
	}
	xa, xb := liveTexts(fa), liveTexts(fb)
	if xa != xb {
		*warns = append(*warns, fmt.Sprintf("%s: final live text differs: swift=%q go=%q", k, xa, xb))
	}
	return diffs
}

// generic summaries show up only until the screen shows the arguments, which depends on timing.
var genericSummaries = map[string]bool{
	"Ran a command": true, "Read a file": true, "Edited a file": true, "Wrote a file": true,
	"Searched": true, "Listed files": true, "Fetched a page": true, "Searched the web": true,
	"Edited a notebook": true, "Ran an agent": true, "Used a skill": true,
}

func liveTools(fs []frame) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		t, ok := f.v["tool"].(map[string]any)
		if !ok {
			continue
		}
		s := fmt.Sprintf("%v: %v", t["name"], t["summary"])
		// "Tool" is an unrecognised or grouped header, also timing-dependent.
		if sum, _ := t["summary"].(string); genericSummaries[sum] || t["name"] == "Tool" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// dropPartial removes summaries caught mid-render: "Bash: Ran cd brid" when either side also has
// "Bash: Ran cd bridge-go && ls".
func dropPartial(tools, other []string) []string {
	all := append(append([]string{}, tools...), other...)
	var out []string
	for _, t := range tools {
		partial := false
		for _, u := range all {
			if u != t && strings.HasPrefix(u, t) {
				partial = true
				break
			}
		}
		if !partial {
			out = append(out, t)
		}
	}
	return out
}

func liveTexts(fs []frame) string {
	last := ""
	for _, f := range fs {
		if t, ok := f.v["text"].(string); ok {
			last = t
		}
	}
	return last
}

// repeats counts frames whose text and tool equal the previous frame's.
func repeats(fs []frame) int {
	n := 0
	for i := 1; i < len(fs); i++ {
		if show(fs[i].v["text"]) == show(fs[i-1].v["text"]) && show(fs[i].v["tool"]) == show(fs[i-1].v["tool"]) {
			n++
		}
	}
	return n
}

func allCleared(fs []frame) bool {
	for _, f := range fs {
		if !cleared(f.v) {
			return false
		}
	}
	return true
}

func cleared(v map[string]any) bool {
	return v["text"] == nil && v["tool"] == nil
}

func countTypes(fa, fb []frame) string {
	count := func(fs []frame) map[string]int {
		m := map[string]int{}
		for _, f := range fs {
			if f.v != nil {
				t, _ := f.v["type"].(string)
				m[t]++
			}
		}
		return m
	}
	a, b := count(fa), count(fb)
	var parts []string
	for _, t := range []string{"agent.updated", "agent.created", "agent.closed", "message.upserted", "reply.live", "usage.updated"} {
		if a[t]+b[t] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d/%d", t, a[t], b[t]))
		}
	}
	return strings.Join(parts, ", ")
}

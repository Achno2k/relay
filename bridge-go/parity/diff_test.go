package main

import (
	"strings"
	"testing"
	"time"
)

func diffJSON(t *testing.T, rules []rule, a, b string) *differ {
	t.Helper()
	ja, err := decode([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	jb, err := decode([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	d := &differ{rules: rules}
	d.walk(nil, ja, jb)
	return d
}

func TestDiffStrict(t *testing.T) {
	cases := []struct {
		name, a, b, want string
	}{
		{"null vs missing", `{"a":null}`, `{}`, "$.a: missing in go"},
		{"missing vs null", `{}`, `{"a":null}`, "$.a: extra in go"},
		{"number literal", `{"n":11}`, `{"n":11.0}`, "$.n: swift=11 go=11.0"},
		{"type", `{"n":"1"}`, `{"n":1}`, `$.n: swift="1" go=1`},
		{"array length", `[1,2]`, `[1]`, "$: length swift=2 go=1"},
		{"nested", `{"a":[{"b":true}]}`, `{"a":[{"b":false}]}`, "$.a[0].b: swift=true go=false"},
		{"empty array vs null", `{"a":[]}`, `{"a":null}`, "$.a: swift=[] go=null"},
	}
	for _, c := range cases {
		d := diffJSON(t, nil, c.a, c.b)
		if len(d.diffs) != 1 || !strings.HasPrefix(d.diffs[0], c.want) {
			t.Errorf("%s: got %q, want prefix %q", c.name, d.diffs, c.want)
		}
	}
	if d := diffJSON(t, nil, `{"a":1,"b":[null,"x"]}`, `{"b":[null,"x"],"a":1}`); len(d.diffs) != 0 {
		t.Errorf("key order should not matter: %q", d.diffs)
	}
}

func TestRules(t *testing.T) {
	goStart := time.Date(2026, 9, 28, 11, 0, 0, 0, time.UTC)
	base := []rule{uptimeRule, clockRule(goStart), usageRule, historyRule}
	cases := []struct {
		name       string
		a, b       string
		diffs, wrn int
	}{
		{"uptime", `{"uptimeSeconds":5}`, `{"uptimeSeconds":900}`, 0, 0},
		{"uptime negative", `{"uptimeSeconds":5}`, `{"uptimeSeconds":-1}`, 1, 0},
		{"swift saw a later seq change", `{"updatedAt":"2026-09-28T10:00:00+00:00"}`, `{"updatedAt":"2026-09-28T09:00:00+00:00"}`, 0, 0},
		{"go first-seen", `{"updatedAt":"2026-09-25T10:00:00+00:00"}`, `{"updatedAt":"2026-09-28T11:00:05+00:00"}`, 0, 0},
		{"go later, before its start", `{"updatedAt":"2026-09-25T10:00:00+00:00"}`, `{"updatedAt":"2026-09-28T10:00:00+00:00"}`, 0, 1},
		{"go wrong format", `{"updatedAt":"2026-09-28T10:00:00+00:00"}`, `{"updatedAt":"2026-09-28T11:00:05Z"}`, 1, 0},
		{"message createdAt is strict", `{"id":"u1","createdAt":"2026-09-28T10:00:00+00:00"}`, `{"id":"u1","createdAt":"2026-09-28T11:00:05+00:00"}`, 1, 0},
		{"screen createdAt", `{"id":"screen:w1:p1","createdAt":"2026-09-25T10:00:00+00:00"}`, `{"id":"screen:w1:p1","createdAt":"2026-09-28T11:00:05+00:00"}`, 0, 0},
		{"usage numbers", `{"providers":[{"updatedAt":"2026-09-28T10:00:00+00:00","windows":[{"usedPercent":1}],"stale":true}]}`,
			`{"providers":[{"updatedAt":"2026-09-28T11:00:00+00:00","windows":[{"usedPercent":2}],"stale":false}]}`, 0, 0},
		{"usage reason absent", `{"providers":[{"unavailableReason":"x"}]}`, `{"providers":[{}]}`, 0, 0},
		{"usage label strict", `{"providers":[{"label":"Claude"}]}`, `{"providers":[{"label":"claude"}]}`, 1, 0},
		{"usage window type", `{"providers":[{"windows":[{"usedPercent":1}]}]}`, `{"providers":[{"windows":[{"usedPercent":"1"}]}]}`, 1, 0},
		{"swift remembers a mode", `{"workspaceId":"w1","permissionMode":"auto"}`, `{"workspaceId":"w1","permissionMode":null}`, 0, 1},
		{"go knows a mode swift doesn't", `{"workspaceId":"w1","permissionMode":null}`, `{"workspaceId":"w1","permissionMode":"auto"}`, 1, 0},
		{"different modes", `{"workspaceId":"w1","permissionMode":"plan"}`, `{"workspaceId":"w1","permissionMode":"auto"}`, 1, 0},
		{"agent updatedAt outside usage stays strict on type", `{"updatedAt":null}`, `{"updatedAt":"2026-09-28T11:00:05+00:00"}`, 1, 0},
	}
	for _, c := range cases {
		d := diffJSON(t, base, c.a, c.b)
		if len(d.diffs) != c.diffs || len(d.warns) != c.wrn {
			t.Errorf("%s: diffs %q warns %q, want %d diffs %d warns", c.name, d.diffs, d.warns, c.diffs, c.wrn)
		}
	}
}

func TestAlignProviders(t *testing.T) {
	a, _ := decode([]byte(`{"providers":[{"id":"claude"},{"id":"codex"}]}`))
	b, _ := decode([]byte(`{"providers":[{"id":"codex"},{"id":"opencode-go"},{"id":"claude"}]}`))
	na, nb, warns := alignProviders(a, b)
	d := &differ{}
	d.walk(nil, na, nb)
	if len(d.diffs) != 0 {
		t.Errorf("aligned providers differ: %q", d.diffs)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "opencode-go") {
		t.Errorf("warns %q", warns)
	}
}

func TestStateTrails(t *testing.T) {
	mk := func(statuses ...string) []frame {
		var fs []frame
		for _, s := range statuses {
			fs = append(fs, frame{v: map[string]any{"type": "agent.updated", "agent": map[string]any{"id": "w1:p1", "status": s}}})
		}
		return fs
	}
	rn := &runner{}
	var warns []string
	if d := rn.diffStates("k", mk("working", "done", "idle"), mk("idle", "working", "working", "done", "idle"), &warns); len(d) != 0 || len(warns) != 1 {
		t.Errorf("suffix trail: diffs %q warns %q", d, warns)
	}
	warns = nil
	if d := rn.diffStates("k", mk("working", "done"), mk("working", "idle"), &warns); len(d) == 0 {
		t.Errorf("different trails should fail")
	}
}

func TestLiveFrames(t *testing.T) {
	mk := func(raw ...string) []frame {
		var fs []frame
		for _, r := range raw {
			v, err := decode([]byte(r))
			if err != nil {
				t.Fatal(err)
			}
			fs = append(fs, frame{raw: []byte(r), v: v.(map[string]any)})
		}
		return fs
	}
	a := mk(`{"type":"reply.live","agentId":"w","seq":1,"text":"Hi","tool":{"name":"Bash","summary":"Ran a command","state":"running"}}`,
		`{"type":"reply.live","agentId":"w","seq":2,"text":"Hi","tool":{"name":"Bash","summary":"Ran ls","state":"running"}}`,
		`{"type":"reply.live","agentId":"w","seq":3,"text":null,"tool":null}`)
	b := mk(`{"type":"reply.live","agentId":"w","seq":6,"text":"Hi","tool":{"name":"Bash","summary":"Ran l","state":"running"}}`,
		`{"type":"reply.live","agentId":"w","seq":7,"text":"Hi","tool":{"name":"Bash","summary":"Ran ls","state":"running"}}`,
		`{"type":"reply.live","agentId":"w","seq":9,"text":null}`)
	var warns []string
	if d := diffLive("k", a, b, true, &warns); len(d) != 0 {
		t.Errorf("equivalent live streams: %q", d)
	}
	bad := mk(`{"type":"reply.live","agentId":"w","seq":2,"text":"x","tool":null}`, `{"type":"reply.live","agentId":"w","seq":2,"text":null,"tool":null,"extra":1}`)
	if d := diffLive("k", a, bad, true, &warns); len(d) < 3 {
		t.Errorf("want seq, key and tools diffs, got %q", d)
	}
}

func TestRepeatsFullText(t *testing.T) {
	long := strings.Repeat("a", 200)
	fs := []frame{
		{v: map[string]any{"text": long, "tool": nil}},
		{v: map[string]any{"text": long + "b", "tool": nil}},
		{v: map[string]any{"text": long + "b", "tool": nil}},
	}
	if n := repeats(fs); n != 1 {
		t.Errorf("repeats = %d, want 1", n)
	}
}

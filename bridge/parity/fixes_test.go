package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFixPageR89(t *testing.T) {
	swift, _ := decode([]byte(`{"hasMore":true,"messages":[
		{"id":"a0","role":"assistant","createdAt":"t0","blocks":[{"type":"text","text":"cut"}]},
		{"id":"u1","role":"user","createdAt":"t1","blocks":[{"type":"text","text":"hi"}]},
		{"id":"a1","role":"assistant","createdAt":"t2","blocks":[{"type":"text","text":"one"}]},
		{"id":"n1","role":"user","createdAt":"t3","blocks":[{"type":"text","text":"<task-notification>\n<task-id>x</task-id>"}]},
		{"id":"a2","role":"assistant","createdAt":"t4","blocks":[{"type":"text","text":"two"}]},
		{"id":"b1","role":"user","createdAt":"t5","blocks":[{"type":"text","text":"<bash-input>ls</bash-input>"}]},
		{"id":"b2","role":"user","createdAt":"t6","blocks":[{"type":"text","text":"<bash-stdout>x</bash-stdout><bash-stderr></bash-stderr>"}]}
	]}`))
	gov, _ := decode([]byte(`{"hasMore":false,"messages":[
		{"id":"old","role":"user","createdAt":"t","blocks":[{"type":"text","text":"older"}]},
		{"id":"u1","role":"user","createdAt":"t1","blocks":[{"type":"text","text":"hi"}]},
		{"id":"a1","role":"assistant","createdAt":"t2","blocks":[{"type":"text","text":"one"},{"type":"text","text":"two"}]},
		{"id":"b1","role":"user","createdAt":"t5","blocks":[{"type":"text","text":"! ls"}]}
	]}`))
	a, b, warns := fixPage(swift, gov)
	d := &differ{}
	d.walk(nil, a, b)
	if len(d.diffs) != 0 {
		ja, _ := json.Marshal(a)
		t.Fatalf("diffs %q\nswift'=%s", d.diffs, ja)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "3 injected") {
		t.Errorf("warns %q", warns)
	}
	// A real difference still shows.
	gov2, _ := decode([]byte(strings.Replace(`{"hasMore":false,"messages":[{"id":"u1","role":"user","createdAt":"t1","blocks":[{"type":"text","text":"HI"}]}]}`, "", "", 1)))
	a, b, _ = fixPage(swift, gov2)
	d = &differ{}
	d.walk(nil, a, b)
	if len(d.diffs) == 0 {
		t.Error("changed text accepted")
	}
}

func TestR88Rule(t *testing.T) {
	cases := []struct {
		a, b string
		ok   bool
	}{
		{"open file:///Users/dev/Downloads/secret.pdf now", "open Downloads/secret.pdf now", true},
		{"scp host:/Users/dev/notes/a.txt .", "scp host:notes/a.txt .", true},
		{"(file://node_modules/x.js:3)", "(node_modules/x.js:3)", true},
		{"open file:///Users/dev/a.pdf now", "open a.pdf later", false},
		{"get https://example.com/x", "get example.com/x", false},
		{"scp k.json <host>:~/.app/k.json, then", "scp k.json <host>:k.json, then", true},
		{"scp host:/Users/dev/a .", "scp host:/Users/dev/b .", false},
		{"Ran grep 'file:///Users/' .claude/projects | wc -l; grep …", "Ran grep 'Users' .claude/projects | wc -l; grep -rlo …", true},
	}
	for _, c := range cases {
		got := r88Rule(nil, nil, c.a, c.b) == "R8-8"
		if got != c.ok {
			t.Errorf("%q vs %q: got %v", c.a, c.b, got)
		}
	}
}

func TestR818(t *testing.T) {
	p := probe{path: "/ws?token=nope"}
	ok, diffs, warns := r818(p, reply{status: 400}, reply{status: 401, body: []byte(`{"error":{"code":"unauthorized","message":"x"}}`)})
	if !ok || len(diffs) != 0 || len(warns) != 1 {
		t.Errorf("fixed: %v %q %q", ok, diffs, warns)
	}
	ok, diffs, _ = r818(p, reply{status: 400}, reply{status: 400})
	if !ok || len(diffs) != 1 {
		t.Errorf("unfixed: %v %q", ok, diffs)
	}
	if ok, _, _ := r818(probe{path: "/agents"}, reply{status: 400}, reply{status: 401}); ok {
		t.Error("only /ws")
	}
}

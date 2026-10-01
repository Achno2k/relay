package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"
)

// runE2E drives one e2e agent through both bridges while both /ws streams are watched: a prompt
// through Swift, a prompt through Go, a stop through Go, and an upload to each. Only ever call it
// on the e2e agents (herdr workspace herd-e2e), after reserving them with qa-bridge.
func (rn *runner) runE2E(ctx context.Context, id string) {
	path := "/agents/" + esc(id)
	r := rn.swift.do(ctx, get(path))
	var ag struct {
		Kind    string `json:"kind"`
		CwdName string `json:"cwdName"`
		Status  string `json:"status"`
	}
	if r.status != 200 || json.Unmarshal(r.body, &ag) != nil {
		rn.rep.add(result{name: "e2e " + id, diffs: []string{fmt.Sprintf("swift GET %s: %d %s", path, r.status, clip(r.body))}})
		return
	}
	if ag.CwdName != "e2e" {
		fmt.Fprintf(os.Stderr, "parity: refusing e2e on %s: cwdName %q is not the e2e folder\n", id, ag.CwdName)
		os.Exit(2)
	}
	fmt.Printf("\n-- e2e %s (%s)\n", id, ag.Kind)
	if !rn.waitReady(ctx, id, 60*time.Second) {
		rn.rep.add(result{name: "e2e " + id, diffs: []string{"agent not idle before the run, skipped"}})
		return
	}
	nonce := time.Now().Unix() % 100000

	// 1. Prompt through Swift, 2. through Go: same frames on both streams, same history after.
	var replies [2]reply
	for i, via := range []*bridge{rn.swift, rn.gov} {
		text := fmt.Sprintf("Run the shell command `echo parity-%d-%s` and then reply with exactly: parity done %d-%s", nonce, via.name, nonce, via.name)
		w := rn.startWS(ctx)
		if w == nil {
			return
		}
		replies[i] = via.do(ctx, post(path+"/prompt", mustJSON(map[string]any{"text": text})))
		rn.waitTurn(ctx, id, via, 240*time.Second)
		time.Sleep(4 * time.Second)
		w.swift.close()
		w.gov.close()
		rn.compareStreams(fmt.Sprintf("e2e %s ws: prompt via %s", id, via.name), w, &id, true)
		rn.check(ctx, get(path))
		rn.check(ctx, get(path+"/messages?limit=10"))
	}
	res := rn.compare(probe{name: "e2e " + id + " prompt reply (swift vs go)", method: "POST", path: path + "/prompt"}, replies[0], replies[1])
	rn.rep.add(res)

	// 3. A stop through Go mid-turn, which also clears what Claude puts back in the input box.
	w := rn.startWS(ctx)
	if w == nil {
		return
	}
	stopText := fmt.Sprintf("Run the shell command `sleep 25 && echo parity-stop-%d`, then reply ok.", nonce)
	rn.gov.do(ctx, post(path+"/prompt", mustJSON(map[string]any{"text": stopText})))
	rn.waitStatus(ctx, id, 30*time.Second, "working")
	time.Sleep(6 * time.Second)
	stop := rn.gov.do(ctx, post(path+"/keys", `{"keys":["esc"]}`))
	rn.waitReady(ctx, id, 60*time.Second)
	time.Sleep(4 * time.Second)
	w.swift.close()
	w.gov.close()
	rn.compareStreams(fmt.Sprintf("e2e %s ws: stop via go", id), w, &id, true)
	if stop.status != 202 || string(stop.body) != "{}" {
		rn.rep.add(result{name: "e2e " + id + " stop via go", diffs: []string{fmt.Sprintf("go answered %d %s, want 202 {}", stop.status, clip(stop.body))}})
	}
	rn.check(ctx, get(path))
	rn.check(ctx, get(path+"/messages?limit=10"))
	rn.check(ctx, get(path+"/approval"))

	// 4. Upload the same file to each bridge; ids differ, the rest matches, and each serves it back.
	png := []byte("\x89PNG\r\n\x1a\nparity")
	up := probe{method: "POST", path: path + "/attachments", body: png,
		header: map[string]string{"Content-Type": "image/png", "X-Filename": "parity%20shot%E2%9C%93.png"}}
	ua, ub := rn.pair(ctx, up)
	res = rn.compare(probe{name: "e2e " + id + " upload", method: "POST", path: up.path, rules: []rule{attachmentIDRule}}, ua, ub)
	rn.rep.add(res)
	var aa, ab struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(ua.body, &aa) == nil && json.Unmarshal(ub.body, &ab) == nil && aa.ID != "" && ab.ID != "" {
		fa := rn.swift.do(ctx, get(path+"/attachments/"+aa.ID))
		fb := rn.gov.do(ctx, get(path+"/attachments/"+ab.ID))
		rn.rep.add(rn.compare(probe{name: "e2e " + id + " download", method: "GET", path: path + "/attachments/:id"}, fa, fb))
	}
}

var hex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)

func attachmentIDRule(path []string, _ map[string]any, a, b any) verdict {
	sa, _ := a.(string)
	sb, _ := b.(string)
	return when(len(path) == 1 && path[0] == "id" && hex16.MatchString(sa) && hex16.MatchString(sb))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (rn *runner) status(ctx context.Context, id string) string {
	r := rn.swift.do(ctx, get("/agents/"+esc(id)))
	var a struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(r.body, &a)
	return a.Status
}

func (rn *runner) waitStatus(ctx context.Context, id string, d time.Duration, want ...string) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := rn.status(ctx, id)
		for _, w := range want {
			if s == w {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func (rn *runner) waitReady(ctx context.Context, id string, d time.Duration) bool {
	return rn.waitStatus(ctx, id, d, "idle", "done")
}

// waitTurn waits for the turn to start and finish. At an approval it checks both bridges parse the
// same dialog, then answers with the first option through the bridge under test.
func (rn *runner) waitTurn(ctx context.Context, id string, via *bridge, d time.Duration) {
	path := "/agents/" + esc(id)
	rn.waitStatus(ctx, id, 20*time.Second, "working", "blocked")
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		switch rn.status(ctx, id) {
		case "idle", "done":
			return
		case "blocked":
			a := rn.check(ctx, get(path+"/approval"))
			var ap struct {
				Options []struct {
					Keys []string `json:"keys"`
				} `json:"options"`
			}
			if json.Unmarshal(a.body, &ap) == nil && len(ap.Options) > 0 {
				via.do(ctx, post(path+"/keys", mustJSON(map[string]any{"keys": ap.Options[0].Keys})))
				time.Sleep(2 * time.Second)
			}
		}
		time.Sleep(time.Second)
	}
	rn.rep.add(result{name: "e2e " + id, diffs: []string{"turn didn't finish in time"}})
}

// runControls checks POST /agents/:id/control on an e2e agent: bad values give the same error on
// both bridges, then the effort is changed through Go and put back through Swift. After each
// change both bridges' reads must agree once Go's 15 s hold on what it just set has expired.
func (rn *runner) runControls(ctx context.Context, id string) {
	path := "/agents/" + esc(id)
	fmt.Printf("\n-- controls %s\n", id)
	for _, body := range []string{`{"effort":"not-an-effort"}`, `{"model":"not-a-model"}`, `{"permissionMode":"not-a-mode"}`, `{"command":"nope"}`} {
		rn.check(ctx, post(path+"/control", body))
	}
	var ag struct {
		Effort  *string `json:"effort"`
		CwdName string  `json:"cwdName"`
	}
	var cs struct {
		Efforts []struct {
			ID string `json:"id"`
		} `json:"efforts"`
		Supports struct {
			Effort bool `json:"effort"`
		} `json:"supports"`
	}
	if json.Unmarshal(rn.swift.do(ctx, get(path)).body, &ag) != nil || json.Unmarshal(rn.swift.do(ctx, get(path+"/controls")).body, &cs) != nil {
		rn.rep.add(result{name: "controls " + id, diffs: []string{"couldn't read the agent or its controls from swift"}})
		return
	}
	if ag.CwdName != "e2e" {
		fmt.Fprintf(os.Stderr, "parity: refusing controls on %s: cwdName %q is not the e2e folder\n", id, ag.CwdName)
		os.Exit(2)
	}
	if !cs.Supports.Effort || ag.Effort == nil || len(cs.Efforts) < 2 {
		fmt.Printf("   (no effort to flip: current %v, %d efforts)\n", ag.Effort, len(cs.Efforts))
		return
	}
	orig, target := *ag.Effort, ""
	for _, e := range cs.Efforts {
		if e.ID != orig && e.ID != "max" && e.ID != "ultra" {
			target = e.ID
			break
		}
	}
	if target == "" {
		return
	}
	if !rn.waitReady(ctx, id, 60*time.Second) {
		rn.rep.add(result{name: "controls " + id, diffs: []string{"agent not idle, skipped"}})
		return
	}
	for _, step := range []struct {
		via   *bridge
		value string
	}{{rn.gov, target}, {rn.swift, orig}} {
		name := fmt.Sprintf("controls %s effort %s via %s", id, step.value, step.via.name)
		r := step.via.do(ctx, post(path+"/control", mustJSON(map[string]string{"effort": step.value})))
		var got struct {
			Effort *string `json:"effort"`
		}
		res := result{name: name}
		if r.status != 202 || json.Unmarshal(r.body, &got) != nil || got.Effort == nil || *got.Effort != step.value {
			res.diffs = append(res.diffs, fmt.Sprintf("%s answered %d %s, want 202 with effort %q", step.via.name, r.status, clip(r.body), step.value))
		}
		rn.rep.add(res)
		time.Sleep(16 * time.Second)
		rn.check(ctx, probe{name: name + ", then both read", method: "GET", path: path, idempotent: true})
		rn.check(ctx, probe{name: name + ", then both read", method: "GET", path: path + "/controls", idempotent: true})
	}
}

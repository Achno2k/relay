// Command parity diffs the Go bridge against the live Swift bridge: every GET endpoint for every
// agent and workspace, error shapes, and the WS streams, optionally while prompts go to the e2e
// agents. It prints each difference and exits 1 on any mismatch.
//
// Usually run through parity/run.sh, which builds and starts the Go bridge on a temp RELAY_HOME.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type authMode int

const (
	authOK authMode = iota
	authNone
	authBad
)

type bridge struct {
	name  string
	base  string
	token string
	hc    *http.Client
}

type probe struct {
	name   string
	method string
	path   string
	body   []byte
	header map[string]string
	auth   authMode
	// retry a mismatch a few times: agents keep working between the two reads.
	idempotent bool
	rules      []rule
}

type reply struct {
	status int
	header http.Header
	body   []byte
	err    error
}

func (b *bridge) do(ctx context.Context, p probe) reply {
	var body io.Reader
	if p.body != nil {
		body = bytes.NewReader(p.body)
	}
	req, err := http.NewRequestWithContext(ctx, p.method, b.base+p.path, body)
	if err != nil {
		return reply{err: err}
	}
	switch p.auth {
	case authOK:
		req.Header.Set("Authorization", "Bearer "+b.token)
	case authBad:
		req.Header.Set("Authorization", "Bearer not-the-token")
	}
	for k, v := range p.header {
		req.Header.Set(k, strings.ReplaceAll(v, tokenPlaceholder, b.token))
	}
	res, err := b.hc.Do(req)
	if err != nil {
		return reply{err: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	return reply{status: res.StatusCode, header: res.Header, body: data, err: err}
}

// ---- report

type result struct {
	name  string
	diffs []string
	warns []string
}

type report struct {
	mu      sync.Mutex
	results []result
	verbose bool
}

func (r *report) add(res result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, res)
	switch {
	case len(res.diffs) > 0:
		fmt.Printf("FAIL %s\n", res.name)
		for _, d := range res.diffs {
			fmt.Printf("     %s\n", d)
		}
	case r.verbose:
		fmt.Printf("ok   %s\n", res.name)
	}
	for _, w := range res.warns {
		fmt.Printf("warn %s: %s\n", res.name, w)
	}
}

func (r *report) summary() (fails, warns int) {
	for _, res := range r.results {
		if len(res.diffs) > 0 {
			fails++
		}
		warns += len(res.warns)
	}
	return
}

// ---- comparison

type runner struct {
	swift, gov *bridge
	goStart    time.Time
	retries    int
	rep        *report
	base       []rule
	dump       string
}

func (rn *runner) pair(ctx context.Context, p probe) (reply, reply) {
	var a, b reply
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a = rn.swift.do(ctx, p) }()
	go func() { defer wg.Done(); b = rn.gov.do(ctx, p) }()
	wg.Wait()
	return a, b
}

// check runs one probe against both bridges and records the result. It returns the swift reply so
// callers can discover more probes from it.
func (rn *runner) check(ctx context.Context, p probe) reply {
	tries := 1
	if p.idempotent {
		tries = rn.retries
	}
	var a reply
	var res result
	for i := 0; i < tries; i++ {
		if i > 0 {
			time.Sleep(1500 * time.Millisecond)
		}
		var b reply
		a, b = rn.pair(ctx, p)
		res = rn.compare(p, a, b)
		if len(res.diffs) == 0 {
			break
		}
	}
	rn.rep.add(res)
	return a
}

func (rn *runner) compare(p probe, a, b reply) result {
	res := result{name: fmt.Sprintf("%s %s", p.method, p.path)}
	if p.name != "" {
		res.name = p.name + " (" + res.name + ")"
	}
	if a.err != nil || b.err != nil {
		if a.err != nil {
			res.diffs = append(res.diffs, "swift error: "+a.err.Error())
		}
		if b.err != nil {
			res.diffs = append(res.diffs, "go error: "+b.err.Error())
		}
		return res
	}
	if ok, diffs, warns := r818(p, a, b); ok {
		res.diffs, res.warns = diffs, warns
		return res
	}
	if a.status != b.status {
		res.diffs = append(res.diffs, fmt.Sprintf("status swift=%d go=%d", a.status, b.status))
	}
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if x, y := a.header.Get(h), b.header.Get(h); x != y {
			res.diffs = append(res.diffs, fmt.Sprintf("header %s swift=%q go=%q", h, x, y))
		}
	}
	res.diffs = append(res.diffs, rn.bodyDiffs(p.path, a.body, b.body, append(rn.base, p.rules...), &res.warns)...)
	return res
}

// bodyDiffs compares two bodies: JSON structurally, anything else byte for byte. An error's
// message text is only a warning; its code and status must match.
func (rn *runner) bodyDiffs(path string, a, b []byte, rules []rule, warns *[]string) []string {
	if len(a) == 0 || len(b) == 0 {
		if len(a) != len(b) {
			return []string{fmt.Sprintf("body length swift=%d go=%d (go=%q)", len(a), len(b), clip(b))}
		}
		return nil
	}
	ja, errA := decode(a)
	jb, errB := decode(b)
	if errA != nil || errB != nil {
		if errA == nil {
			return []string{fmt.Sprintf("go body is not JSON: %q", clip(b))}
		}
		if !bytes.Equal(a, b) {
			return []string{fmt.Sprintf("bytes differ: swift %d bytes, go %d bytes", len(a), len(b))}
		}
		return nil
	}
	ja, jb, extra := alignProviders(ja, jb)
	*warns = append(*warns, extra...)
	if strings.Contains(path, "/messages") {
		ja, jb, extra = fixPage(ja, jb)
		*warns = append(*warns, extra...)
	}
	d := &differ{rules: rules, max: 25}
	d.walk(nil, ja, jb)
	*warns = append(*warns, summarize(d.warns)...)
	var out []string
	for _, x := range d.diffs {
		if strings.HasPrefix(x, "$.error.message:") {
			*warns = append(*warns, x)
			continue
		}
		out = append(out, x)
	}
	return out
}

// alignProviders lines /usage providers up by id. Each bridge polls on its own timer, so one may
// know a provider (say opencode-go) the other hasn't fetched yet; that is a warning, not a diff.
func alignProviders(a, b any) (any, any, []string) {
	ma, okA := a.(map[string]any)
	mb, okB := b.(map[string]any)
	if !okA || !okB {
		return a, b, nil
	}
	pa, okA := ma["providers"].([]any)
	pb, okB := mb["providers"].([]any)
	if !okA || !okB {
		return a, b, nil
	}
	id := func(v any) string {
		m, _ := v.(map[string]any)
		s, _ := m["id"].(string)
		return s
	}
	byID := map[string]any{}
	for _, p := range pb {
		byID[id(p)] = p
	}
	var warns []string
	var na, nb []any
	for _, p := range pa {
		if q, ok := byID[id(p)]; ok {
			na, nb = append(na, p), append(nb, q)
			delete(byID, id(p))
		} else {
			warns = append(warns, fmt.Sprintf("provider %q only on swift (not fetched by go yet?)", id(p)))
		}
	}
	for _, p := range pb {
		if _, ok := byID[id(p)]; ok {
			warns = append(warns, fmt.Sprintf("provider %q only on go (not fetched by swift yet?)", id(p)))
		}
	}
	ca, cb := map[string]any{}, map[string]any{}
	for k, v := range ma {
		ca[k] = v
	}
	for k, v := range mb {
		cb[k] = v
	}
	ca["providers"], cb["providers"] = orEmpty(na), orEmpty(nb)
	return ca, cb, warns
}

func orEmpty(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

// summarize folds repeated known-fix warnings ("R8-8 ...") into one line per fix.
func summarize(ws []string) []string {
	var out []string
	count := map[string]int{}
	var order []string
	for _, w := range ws {
		if i := strings.Index(w, ": R8-"); i >= 0 {
			id := w[i+2:]
			id = id[:strings.IndexAny(id+":", " :")]
			if count[id] == 0 {
				order = append(order, id)
			}
			count[id]++
			continue
		}
		out = append(out, w)
	}
	for _, id := range order {
		out = append(out, fmt.Sprintf("%s: %d value(s) accepted as the known fix", id, count[id]))
	}
	return out
}

func clip(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// ---- probes

const fakeAgent = "w999%3Ap999" // never a live pane

// tokenPlaceholder in a probe header becomes each bridge's own token.
const tokenPlaceholder = "{{token}}"

func get(path string) probe { return probe{method: "GET", path: path, idempotent: true} }

func post(path, body string) probe {
	return probe{method: "POST", path: path, body: []byte(body), header: map[string]string{"Content-Type": "application/json"}}
}

func esc(id string) string { return url.PathEscape(id) }

type agentRef struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	HasTranscript bool   `json:"hasTranscript"`
}

type page struct {
	Messages []struct {
		ID     string `json:"id"`
		Blocks []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"blocks"`
	} `json:"messages"`
	HasMore bool `json:"hasMore"`
}

func (rn *runner) runREST(ctx context.Context, maxPages int, only map[string]bool) {
	// Global endpoints.
	rn.check(ctx, get("/health"))
	for _, p := range []probe{
		{method: "GET", path: "/health", auth: authNone, idempotent: true},
		get("/machine"),
		get("/controls"),
		get("/controls?kind=claude"),
		get("/controls?kind=pi"),
		get("/controls?kind=codex"),
		get("/controls?kind=bogus"),
		get("/controls?kind="),
		get("/usage"),
		get("/workspaces"),
		get("/agents"),
	} {
		rn.check(ctx, p)
	}
	rn.errorProbes(ctx)

	// Per agent.
	a := rn.check(ctx, get("/agents"))
	var agents []agentRef
	if err := json.Unmarshal(a.body, &agents); err != nil {
		rn.rep.add(result{name: "discover agents", diffs: []string{"swift /agents: " + err.Error()}})
		return
	}
	var firstTranscript string
	var attachments []string
	for _, ag := range agents {
		if len(only) > 0 && !only[ag.ID] {
			continue
		}
		id := esc(ag.ID)
		rn.check(ctx, get("/agents/"+id))
		rn.check(ctx, get("/agents/"+id+"/approval"))
		rn.check(ctx, get("/agents/"+id+"/controls"))
		first := rn.check(ctx, get("/agents/"+id+"/messages"))
		attachments = append(attachments, pageAttachments(ag.ID, first.body)...)
		if ag.HasTranscript && firstTranscript == "" {
			firstTranscript = ag.ID
		}
		// Walk history in bigger pages, as the app does when scrolling up.
		before := ""
		for n := 0; n < maxPages; n++ {
			q := "/agents/" + id + "/messages?limit=200"
			if before != "" {
				q += "&before=" + url.QueryEscape(before)
			}
			r := rn.check(ctx, get(q))
			attachments = append(attachments, pageAttachments(ag.ID, r.body)...)
			var pg page
			if json.Unmarshal(r.body, &pg) != nil || !pg.HasMore || len(pg.Messages) == 0 {
				break
			}
			before = pg.Messages[0].ID
		}
	}
	if firstTranscript != "" {
		id := esc(firstTranscript)
		for _, q := range []string{"?limit=0", "?limit=1", "?limit=abc", "?limit=-5", "?limit=9999", "?before=not-a-message-id", "?limit=3&before="} {
			rn.check(ctx, get("/agents/"+id+"/messages"+q))
		}
	}
	sort.Strings(attachments)
	seen := map[string]bool{}
	for i, att := range attachments {
		if seen[att] || i >= 20 {
			continue
		}
		seen[att] = true
		rn.check(ctx, get(att))
	}
}

func pageAttachments(agentID string, body []byte) []string {
	var pg page
	if json.Unmarshal(body, &pg) != nil {
		return nil
	}
	var out []string
	for _, m := range pg.Messages {
		for _, b := range m.Blocks {
			if b.Type == "attachment" && b.ID != "" {
				out = append(out, "/agents/"+esc(agentID)+"/attachments/"+url.PathEscape(b.ID))
			}
		}
	}
	return out
}

// errorProbes hit only a pane id that doesn't exist, so nothing reaches a real agent.
func (rn *runner) errorProbes(ctx context.Context) {
	big := `{"keys":["` + strings.Repeat("a", 2*1024*1024+16) + `"]}`
	for _, p := range []probe{
		{name: "no token", method: "GET", path: "/agents", auth: authNone, idempotent: true},
		{name: "bad token", method: "GET", path: "/agents", auth: authBad, idempotent: true},
		{name: "no token ws", method: "GET", path: "/ws", auth: authNone, idempotent: true},
		{name: "bad token ws", method: "GET", path: "/ws?token=nope", auth: authNone, idempotent: true},
		{name: "unknown route no token", method: "GET", path: "/nope", auth: authNone, idempotent: true},
		get("/nope"),
		get("/agents/" + fakeAgent),
		get("/agents/w999:p999"),
		get("/agents/" + fakeAgent + "/messages"),
		get("/agents/" + fakeAgent + "/approval"),
		get("/agents/" + fakeAgent + "/controls"),
		get("/agents/" + fakeAgent + "/attachments/0000000000000000"),
		get("/agents/%01bad"),
		get("/agents/" + strings.Repeat("x", 200)),
		{name: "wrong method", method: "DELETE", path: "/agents", idempotent: true},
		{name: "HEAD", method: "HEAD", path: "/health", idempotent: true},
		{name: "OPTIONS", method: "OPTIONS", path: "/agents", idempotent: true},
		get("/agents/"),
		get("//agents"),
		get("/health?x=1"),
		get("/agents/" + fakeAgent + "/"),
		{name: "query token off /ws", method: "GET", path: "/agents?token=" + "x", auth: authNone, idempotent: true},
		{name: "lowercase bearer, extra spaces", method: "GET", path: "/machine", header: map[string]string{"Authorization": "bearer   " + tokenPlaceholder + "  "}, idempotent: true},
		{name: "wrong scheme", method: "GET", path: "/machine", auth: authNone, header: map[string]string{"Authorization": "Token " + tokenPlaceholder}, idempotent: true},
		{name: "wrong method health", method: "POST", path: "/health", idempotent: true},
		post("/agents/"+fakeAgent+"/keys", `{"keys":`),
		post("/agents/"+fakeAgent+"/keys", `{}`),
		post("/agents/"+fakeAgent+"/keys", `{"keys":[]}`),
		post("/agents/"+fakeAgent+"/keys", `{"keys":["not-a-key"]}`),
		post("/agents/"+fakeAgent+"/keys", `{"keys":["esc"]}`),
		post("/agents/"+fakeAgent+"/keys", big),
		post("/agents/"+fakeAgent+"/text", `{"text":""}`),
		post("/agents/"+fakeAgent+"/text", `{"text":"hi","submit":false}`),
		post("/agents/"+fakeAgent+"/prompt", `{}`),
		post("/agents/"+fakeAgent+"/prompt", `{"text":""}`),
		post("/agents/"+fakeAgent+"/prompt", `{"text":"hi"}`),
		post("/agents/"+fakeAgent+"/prompt", `{"text":"hi","attachments":["ffffffffffffffff"]}`),
		post("/agents/"+fakeAgent+"/control", `{}`),
		post("/agents/"+fakeAgent+"/control", `{"model":"opus","effort":"high"}`),
		post("/agents/"+fakeAgent+"/control", `{"command":"nope"}`),
		post("/agents/"+fakeAgent+"/control", `{"effort":"high"}`),
		post("/agents", `{}`),
		post("/agents", `{"workspaceId":"w999","kind":""}`),
		post("/agents", `{"workspaceId":"w999","kind":"claude","name":"Bad Name"}`),
		post("/agents", `{"workspaceId":"w999","kind":"claude"}`),
		post("/agents", `{"workspaceId":"w999","kind":"claude","model":"not-a-model"}`),
		{name: "empty upload", method: "POST", path: "/agents/" + fakeAgent + "/attachments", body: []byte{}, header: map[string]string{"Content-Type": "image/png", "X-Filename": "a.png"}},
		{name: "upload to missing agent", method: "POST", path: "/agents/" + fakeAgent + "/attachments", body: []byte("hello"), header: map[string]string{"Content-Type": "text/plain", "X-Filename": "a.txt"}},
	} {
		rn.check(ctx, p)
	}
}

// ---- main

func readToken(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
		os.Exit(2)
	}
	return strings.TrimSpace(string(b))
}

func main() {
	home, _ := os.UserHomeDir()
	swiftURL := flag.String("swift", "http://127.0.0.1:7878", "Swift bridge base URL")
	goURL := flag.String("go", "http://127.0.0.1:7880", "Go bridge base URL")
	swiftToken := flag.String("swift-token", filepath.Join(home, ".relay", "token"), "Swift bridge token file")
	goToken := flag.String("go-token", "", "Go bridge token file (default $RELAY_HOME/token)")
	retries := flag.Int("retries", 4, "attempts per GET before a mismatch counts (agents change between reads)")
	maxPages := flag.Int("max-pages", 3, "history pages (200 messages each) to walk per agent")
	agentsFlag := flag.String("agents", "", "comma-separated agent ids to limit per-agent probes to")
	wsSeconds := flag.Int("ws-seconds", 20, "seconds to watch both /ws streams passively (0 skips)")
	e2e := flag.String("e2e", "", "comma-separated e2e agent ids to prompt through both bridges, e.g. w14:p2,w14:p4,w14:p5 (reserve them with qa-bridge first)")
	e2eControls := flag.Bool("e2e-controls", false, "with -e2e: also change each agent's effort through Go and back through Swift")
	verbose := flag.Bool("v", false, "print passing probes too")
	dump := flag.String("dump", "", "directory to write each WS window's raw frames to (local only: they hold real transcript text)")
	flag.Parse()

	if *goToken == "" {
		rh := os.Getenv("RELAY_HOME")
		if rh == "" {
			fmt.Fprintln(os.Stderr, "parity: set -go-token or RELAY_HOME (the Go bridge's temp home)")
			os.Exit(2)
		}
		*goToken = filepath.Join(rh, "token")
	}
	hc := &http.Client{Timeout: 150 * time.Second}
	rn := &runner{
		swift:   &bridge{name: "swift", base: strings.TrimRight(*swiftURL, "/"), token: readToken(*swiftToken), hc: hc},
		gov:     &bridge{name: "go", base: strings.TrimRight(*goURL, "/"), token: readToken(*goToken), hc: hc},
		retries: max(1, *retries),
		dump:    *dump,
		rep:     &report{verbose: *verbose},
	}
	ctx := context.Background()
	began := time.Now()
	rn.goStart = rn.startTime(ctx)
	rn.base = []rule{uptimeRule, clockRule(rn.goStart), usageRule, historyRule, r88Rule}
	fmt.Printf("parity: swift %s vs go %s (go up since %s)\n", rn.swift.base, rn.gov.base, rn.goStart.UTC().Format(time.RFC3339))

	only := map[string]bool{}
	for _, id := range strings.Split(*agentsFlag, ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}

	// Watch the streams while the REST pass runs, so both have traffic to compare.
	var ws *wsWatch
	if *wsSeconds > 0 {
		ws = rn.startWS(ctx)
	}
	rn.runREST(ctx, *maxPages, only)
	if ws != nil {
		if rest := time.Duration(*wsSeconds)*time.Second - time.Since(ws.started); rest > 0 {
			time.Sleep(rest)
		}
		rn.finishWS(ws)
	}
	if *e2e != "" {
		for _, id := range strings.Split(*e2e, ",") {
			if id = strings.TrimSpace(id); id != "" {
				rn.runE2E(ctx, id)
				if *e2eControls {
					rn.runControls(ctx, id)
				}
			}
		}
	}

	fails, warns := rn.rep.summary()
	fmt.Printf("\nparity: %d checks, %d failed, %d warnings (%s)\n", len(rn.rep.results), fails, warns, time.Since(began).Round(time.Second))
	if fails > 0 {
		os.Exit(1)
	}
}

// startTime derives when the Go bridge started from its /health uptime.
func (rn *runner) startTime(ctx context.Context) time.Time {
	r := rn.gov.do(ctx, probe{method: "GET", path: "/health"})
	var h struct {
		UptimeSeconds int64 `json:"uptimeSeconds"`
	}
	if r.err != nil || json.Unmarshal(r.body, &h) != nil {
		fmt.Fprintf(os.Stderr, "parity: go bridge /health failed at %s: %v %s\n", rn.gov.base, r.err, clip(r.body))
		os.Exit(2)
	}
	return time.Now().Add(-time.Duration(h.UptimeSeconds) * time.Second)
}

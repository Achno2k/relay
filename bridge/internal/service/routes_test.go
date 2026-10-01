package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

const routesToken = "test-token"

var routesAuth = map[string]string{"Authorization": "Bearer " + routesToken}

type routesApp struct {
	ts   *httptest.Server
	fake *herdrtest.Server
	hub  *server.Hub
	// w1:p1's transcript, for tests that append lines.
	transcript string
}

func str(s string) *string { return &s }

// withApp sets up a fake herdr with a claude agent (w1:p1, with transcript), a blocked claude
// agent whose transcript doesn't exist yet (w1:p2) and a gemini agent, a kind without a parser
// (w2:p3). Swift: RoutesTests.withApp.
func withApp(t *testing.T, status func(string) string) routesApp {
	t.Helper()
	if status == nil {
		status = func(id string) string {
			if id == "w1:p2" {
				return "blocked"
			}
			return "idle"
		}
	}
	projects := t.TempDir()
	dir := filepath.Join(projects, transcript.ProjectDirName("/Users/dev/shop-api"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/claude-session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		gemini := herdrtest.AgentJSON("w2:p3", nil, status("w2:p3"), "/Users/dev/website", nil, "")
		gemini["agent"] = "gemini"
		gemini["terminal_title_stripped"] = ""
		agents := []map[string]any{
			herdrtest.AgentJSON("w1:p1", str("api-refactor"), status("w1:p1"), "/Users/dev/shop-api",
				map[string]any{"source": "herdr:claude", "agent": "claude", "kind": "id", "value": "sess-1"}, "Refactor /Users/dev/shop-api/auth"),
			herdrtest.AgentJSON("w1:p2", str("tests"), status("w1:p2"), "/Users/dev/shop-api",
				map[string]any{"source": "herdr:claude", "agent": "claude", "kind": "id", "value": "missing"}, ""),
			gemini,
		}
		target, _ := params["target"].(string)
		switch method {
		case "agent.list":
			return map[string]any{"type": "agent_list", "agents": agents}
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.get":
			for _, a := range agents {
				if a["pane_id"] == target || (a["name"] != nil && a["name"] == target) {
					return map[string]any{"type": "agent_info", "agent": a}
				}
			}
			return herdrtest.Error{Code: "agent_not_found", Message: "no agent " + target}
		case "agent.read":
			text := "$ codex\n> working in /Users/dev/website/src\n"
			if params["source"] == "detection" {
				text = " Bash command\n\n   rm -rf /Users/dev/shop-api/build\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently (esc)\n"
			}
			return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": target, "workspace_id": "w1", "tab_id": "w1:t1",
				"source": params["source"], "format": "text", "text": text, "revision": 7, "truncated": false}}
		case "agent.prompt":
			if target == "w1:p2" {
				return herdrtest.Error{Code: "agent_blocked", Message: "agent is blocked"}
			}
			return map[string]any{"type": "agent_prompted", "agent": agents[0]}
		case "agent.send_keys", "pane.send_text":
			return map[string]any{"type": "ok"}
		case "events.subscribe":
			return map[string]any{"type": "subscription_started"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})

	svc := New(Deps{
		Herdr:   herdr.NewClient(fake.SocketPath),
		Locator: transcript.NewLocator(projects, transcript.NewCodexRollouts(filepath.Join(projects, "codex"))),
		Uploads: uploads.NewStore(filepath.Join(projects, "uploads")),
	})
	hub := server.NewHub()
	ts := httptest.NewServer(server.New(server.Options{
		Backend: svc, Hub: hub, Token: routesToken,
		Machine: func() api.Machine { return testMachine() },
	}))
	t.Cleanup(ts.Close)
	return routesApp{ts: ts, fake: fake, hub: hub, transcript: filepath.Join(dir, "sess-1.jsonl")}
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (a routesApp) do(t *testing.T, method, uri string, body []byte, headers map[string]string) resp {
	t.Helper()
	req, err := http.NewRequest(method, a.ts.URL+uri, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := a.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, res.Header, b}
}

func (a routesApp) get(t *testing.T, uri string) resp { return a.do(t, "GET", uri, nil, routesAuth) }

func (a routesApp) post(t *testing.T, uri, body string) resp {
	return a.do(t, "POST", uri, []byte(body), routesAuth)
}

func decodeInto[T any](t *testing.T, r resp) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode %T: %v: %s", v, err, r.body)
	}
	return v
}

func errCode(t *testing.T, r resp) string { return decodeInto[api.ErrorBody](t, r).Error.Code }

func TestRoutes_HealthNeedsNoAuth(t *testing.T) {
	app := withApp(t, nil)
	r := app.do(t, "GET", "/health", nil, nil)
	if r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	o := decodeInto[map[string]any](t, r)
	if o["ok"] != true || o["version"] != api.Version {
		t.Errorf("%s", r.body)
	}
	if o["herdr"] != "unavailable" { // no Monitor wired into this test app
		t.Errorf("herdr = %v", o["herdr"])
	}
	if up, ok := o["uptimeSeconds"].(float64); !ok || up < 0 {
		t.Errorf("uptime %v", o["uptimeSeconds"])
	}
}

func TestRoutes_RejectsMissingOrWrongToken(t *testing.T) {
	app := withApp(t, nil)
	r := app.do(t, "GET", "/agents", nil, nil)
	if r.status != http.StatusUnauthorized || errCode(t, r) != "unauthorized" {
		t.Errorf("%d %s", r.status, r.body)
	}
	r = app.do(t, "GET", "/agents", nil, map[string]string{"Authorization": "Bearer nope"})
	if r.status != http.StatusUnauthorized {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_ListsAgentsWithoutLeakingPaths(t *testing.T) {
	app := withApp(t, nil)
	r := app.get(t, "/agents")
	if r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	body := string(r.body)
	if strings.Contains(body, "/Users/") {
		t.Errorf("leaked a path: %s", body)
	}
	agents := decodeInto[[]api.Agent](t, r)
	if len(agents) != 3 || agents[0].ID != "w1:p1" || agents[1].ID != "w1:p2" || agents[2].ID != "w2:p3" {
		t.Fatalf("%s", body)
	}
	a0, a1, a2 := agents[0], agents[1], agents[2]
	if a0.Name == nil || *a0.Name != "api-refactor" || a0.Title != "Refactor auth" || a0.WorkspaceName != "shop-api" ||
		a0.CwdName != "shop-api" || !a0.HasTranscript {
		t.Errorf("a0 = %+v", a0)
	}
	if a1.HasTranscript || a1.Status != api.StatusBlocked {
		t.Errorf("a1 = %+v", a1)
	}
	if a2.Kind != "gemini" || a2.Name != nil || a2.Title != "gemini" {
		t.Errorf("a2 = %+v", a2)
	}
	states := []api.TranscriptState{a0.TranscriptState, a1.TranscriptState, a2.TranscriptState}
	if states[0] != api.TranscriptReady || states[1] != api.TranscriptPending || states[2] != api.TranscriptUnsupported {
		t.Errorf("states %v", states)
	}
	if !strings.Contains(body, `"transcriptState":"pending"`) || !strings.Contains(body, `"name":null`) {
		t.Errorf("shape: %s", body)
	}
}

func TestRoutes_WorkspacesCountAgents(t *testing.T) {
	app := withApp(t, nil)
	got := decodeInto[[]api.Workspace](t, app.get(t, "/workspaces"))
	want := []api.Workspace{{ID: "w1", Name: "shop-api", AgentCount: 2}, {ID: "w2", Name: "website", AgentCount: 1}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("%+v", got)
	}
}

func TestRoutes_RejectsAdversarialAgentIds(t *testing.T) {
	app := withApp(t, nil)
	if r := app.get(t, "/agents/"+strings.Repeat("w1%3Ap1", 30)); r.status != http.StatusBadRequest {
		t.Errorf("long: %d", r.status)
	}
	if r := app.get(t, "/agents/w1%3Ap1%00x"); r.status != http.StatusBadRequest {
		t.Errorf("nul: %d", r.status)
	}
}

func TestRoutes_GetAgentDecodesEncodedId(t *testing.T) {
	app := withApp(t, nil)
	r := app.get(t, "/agents/w1%3Ap1")
	if r.status != http.StatusOK || decodeInto[api.Agent](t, r).ID != "w1:p1" {
		t.Errorf("%d %s", r.status, r.body)
	}
	r = app.get(t, "/agents/w9%3Ap9")
	if r.status != http.StatusNotFound || errCode(t, r) != "not_found" {
		t.Errorf("%d %s", r.status, r.body)
	}
}

func ids(ms []api.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func TestRoutes_PagesMessagesOldestFirst(t *testing.T) {
	app := withApp(t, nil)
	r := app.get(t, "/agents/w1%3Ap1/messages?limit=2")
	p := decodeInto[api.MessagePage](t, r)
	if got := strings.Join(ids(p.Messages), ","); got != "u2,a6" || !p.HasMore {
		t.Errorf("%s hasMore=%v", got, p.HasMore)
	}
	if strings.Contains(string(r.body), "/Users/") {
		t.Errorf("leak")
	}
	p = decodeInto[api.MessagePage](t, app.get(t, "/agents/w1%3Ap1/messages?before=u2&limit=50"))
	if got := strings.Join(ids(p.Messages), ","); got != "u1,a1" || p.HasMore {
		t.Errorf("%s hasMore=%v", got, p.HasMore)
	}
	if r := app.get(t, "/agents/w1%3Ap1/messages?before=nope"); r.status != http.StatusNotFound {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_ScreenFallbackWithoutTranscript(t *testing.T) {
	app := withApp(t, nil)
	p := decodeInto[api.MessagePage](t, app.get(t, "/agents/w2%3Ap3/messages"))
	if len(p.Messages) != 1 || p.Messages[0].ID != "screen:w2:p3" {
		t.Fatalf("%+v", p)
	}
	b := p.Messages[0].Blocks
	if len(b) != 1 || !reflect.DeepEqual(b[0], api.TextBlock("```\n$ codex\n> working in src\n```")) {
		t.Errorf("%+v", b)
	}
}

func TestRoutes_PendingTranscriptIsEmptyNotAScreenRead(t *testing.T) {
	app := withApp(t, nil)
	p := decodeInto[api.MessagePage](t, app.get(t, "/agents/w1%3Ap2/messages"))
	if len(p.Messages) != 0 || p.HasMore {
		t.Errorf("%+v", p)
	}
	// No screen read for a kind that will have a transcript.
	if strings.Contains(app.fake.Params("agent.read"), "recent") {
		t.Errorf("read %s", app.fake.Params("agent.read"))
	}
}

func TestRoutes_ApprovalWhenBlockedElse204(t *testing.T) {
	app := withApp(t, nil)
	r := app.get(t, "/agents/w1%3Ap2/approval")
	if r.status != http.StatusOK {
		t.Fatalf("%d %s", r.status, r.body)
	}
	want := `{"agentId":"w1:p2","question":"Do you want to proceed?","options":[{"label":"Yes","keys":["1"]},{"label":"No, and tell Claude what to do differently","keys":["esc"]}]}`
	if string(r.body) != want {
		t.Errorf("%s", r.body)
	}
	if r := app.get(t, "/agents/w1%3Ap1/approval"); r.status != http.StatusNoContent {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_PromptAndKeysForwardToHerdr(t *testing.T) {
	app := withApp(t, nil)
	r := app.post(t, "/agents/w1%3Ap1/prompt", `{"text":"hi"}`)
	if r.status != http.StatusAccepted || string(r.body) != "{}" {
		t.Errorf("%d %s", r.status, r.body)
	}
	if p := app.fake.Params("agent.prompt"); p != `{"target":"w1:p1","text":"hi"}` {
		t.Errorf("prompt %s", p)
	}
	if r := app.post(t, "/agents/w1%3Ap1/keys", `{"keys":["esc"]}`); r.status != http.StatusAccepted {
		t.Errorf("%d", r.status)
	}
	if p := app.fake.Params("agent.send_keys"); p != `{"keys":["esc"],"target":"w1:p1"}` {
		t.Errorf("keys %s", p)
	}
}

func TestRoutes_TextRoute(t *testing.T) {
	app := withApp(t, nil)
	if r := app.post(t, "/agents/w1%3Ap2/text", `{"text":"Green tea"}`); r.status != http.StatusAccepted {
		t.Errorf("%d %s", r.status, r.body)
	}
	if p := app.fake.Params("pane.send_text"); p != `{"pane_id":"w1:p2","text":"Green tea"}` {
		t.Errorf("text %s", p)
	}
	if p := app.fake.Params("agent.send_keys"); p != `{"keys":["enter"],"target":"w1:p2"}` {
		t.Errorf("keys %s", p)
	}
	if r := app.post(t, "/agents/w1%3Ap2/text", `{"text":""}`); r.status != http.StatusBadRequest {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_UploadServeAndPromptWithAttachments(t *testing.T) {
	app := withApp(t, nil)
	png := []byte{0x89, 0x50, 0x4E, 0x47, 1, 2, 3}
	h := map[string]string{"Authorization": "Bearer " + routesToken, "Content-Type": "image/png", "X-Filename": "My%20Screen%20Shot%20%E2%9C%93.png"}
	r := app.do(t, "POST", "/agents/w1%3Ap1/attachments", png, h)
	if r.status != http.StatusCreated {
		t.Fatalf("%d %s", r.status, r.body)
	}
	a := decodeInto[api.Attachment](t, r)
	if a.Name != "My-Screen-Shot.png" || a.Kind != api.AttachmentImage || a.Size != len(png) {
		t.Errorf("%+v", a)
	}

	r = app.get(t, "/agents/w1%3Ap1/attachments/"+a.ID)
	if r.status != http.StatusOK || r.header.Get("Content-Type") != "image/png" || !bytes.Equal(r.body, png) {
		t.Errorf("%d %s", r.status, r.header.Get("Content-Type"))
	}
	if r := app.get(t, "/agents/w1%3Ap1/attachments/0000000000000000"); r.status != http.StatusNotFound {
		t.Errorf("%d", r.status)
	}

	if r := app.post(t, "/agents/w1%3Ap1/prompt", `{"text":"What does it say?","attachments":["`+a.ID+`"]}`); r.status != http.StatusAccepted {
		t.Fatalf("%d %s", r.status, r.body)
	}
	var sent struct{ Text string }
	if err := json.Unmarshal([]byte(app.fake.Params("agent.prompt")), &sent); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sent.Text, "What does it say?\n\nAttached files: /") || !strings.HasSuffix(sent.Text, "/w1_p1/"+a.ID+"-My-Screen-Shot.png") {
		t.Errorf("%q", sent.Text)
	}

	// Attachments only, no text.
	if r := app.post(t, "/agents/w1%3Ap1/prompt", `{"attachments":["`+a.ID+`"]}`); r.status != http.StatusAccepted {
		t.Errorf("%d", r.status)
	}
	if r := app.post(t, "/agents/w1%3Ap1/prompt", `{"text":"x","attachments":["ffffffffffffffff"]}`); r.status != http.StatusBadRequest {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_UploadLimits(t *testing.T) {
	app := withApp(t, nil)
	if r := app.post(t, "/agents/w1%3Ap1/attachments", ""); r.status != http.StatusBadRequest {
		t.Errorf("empty: %d", r.status)
	}
	r := app.do(t, "POST", "/agents/w1%3Ap1/attachments", make([]byte, uploads.MaxBytes+1), routesAuth)
	if r.status != http.StatusRequestEntityTooLarge || errCode(t, r) != "too_large" {
		t.Errorf("big: %d %s", r.status, r.body)
	}
	if r := app.post(t, "/agents/w9%3Ap9/attachments", "x"); r.status != http.StatusNotFound {
		t.Errorf("unknown agent: %d", r.status)
	}
}

func TestRoutes_Machine(t *testing.T) {
	app := withApp(t, nil)
	m := decodeInto[api.Machine](t, app.get(t, "/machine"))
	if m.Name == "" || (m.Kind != "laptop" && m.Kind != "desktop") || m.OS == "" || m.ID == "" {
		t.Errorf("%+v", m)
	}
	if r := app.do(t, "GET", "/machine", nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_PromptErrors(t *testing.T) {
	app := withApp(t, nil)
	r := app.post(t, "/agents/w1%3Ap2/prompt", `{"text":"hi"}`)
	if r.status != http.StatusConflict || errCode(t, r) != "agent_blocked" {
		t.Errorf("%d %s", r.status, r.body)
	}
	r = app.post(t, "/agents/w1%3Ap1/prompt", "nope")
	if r.status != http.StatusBadRequest || errCode(t, r) != "bad_request" {
		t.Errorf("%d %s", r.status, r.body)
	}
	if r := app.post(t, "/agents", `{"workspaceId":"w1","kind":"claude","name":"Bad Name"}`); r.status != http.StatusBadRequest {
		t.Errorf("%d", r.status)
	}
}

func TestRoutes_WebSocketNeedsTokenAndStreamsEvents(t *testing.T) {
	app := withApp(t, nil)
	if r := app.do(t, "GET", "/ws", nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("%d", r.status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(app.ts.URL, "http")+"/ws?token="+routesToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var frames []string
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	frames = append(frames, string(data))
	for app.hub.Count() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	app.hub.Broadcast(api.AgentClosed("w1:p1"))
	if _, data, err = c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	frames = append(frames, string(data))
	if frames[0] != `{"type":"hello"}` || frames[1] != `{"type":"agent.closed","agentId":"w1:p1"}` {
		t.Errorf("%q", frames)
	}
}

func TestRoutes_MonitorEmitsCreatedUpdatedClosed(t *testing.T) {
	var mu sync.Mutex
	statuses := map[string]string{"w1:p1": "idle", "w1:p2": "blocked", "w2:p3": "idle"}
	app := withApp(t, func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		if s, ok := statuses[id]; ok {
			return s
		}
		return "idle"
	})
	svc := New(Deps{
		Herdr:   herdr.NewClient(app.fake.SocketPath),
		Locator: transcript.NewLocator("/nonexistent", transcript.NewCodexRollouts("/nonexistent")),
		Uploads: uploads.NewStore(t.TempDir()),
	})
	hub := server.NewHub()
	sid, events := hub.Subscribe()
	defer hub.Unsubscribe(sid)
	m := NewMonitor(svc, hub, herdr.NewEventStream(app.fake.SocketPath), MonitorOptions{Interval: time.Hour})
	ctx := context.Background()
	m.Trigger(ctx) // baseline: no events
	mu.Lock()
	statuses["w1:p1"] = "working"
	mu.Unlock()
	m.Trigger(ctx)
	select {
	case e := <-events:
		if e.Type != api.EventAgentUpdated || e.Agent.ID != "w1:p1" || e.Agent.Status != api.StatusWorking {
			t.Errorf("%+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected agent.updated")
	}
}

// Security audit: every route this fixture world can reach, swept for a leaked absolute path.
// w1:p1's title ("Refactor /Users/dev/shop-api/auth") is deliberately adversarial input.
func TestRoutes_NoRouteEverLeaksAnAbsolutePath(t *testing.T) {
	app := withApp(t, nil)
	check := func(uri string, body []byte) {
		t.Helper()
		for _, p := range []string{"/Users/", "/private/"} {
			if bytes.Contains(body, []byte(p)) {
				t.Errorf("%s leaked %s: %s", uri, p, body)
			}
		}
	}
	for _, uri := range []string{
		"/health", "/workspaces", "/agents", "/machine", "/controls", "/controls?kind=claude",
		"/agents/w1%3Ap1", "/agents/w1%3Ap2", "/agents/w2%3Ap3",
		"/agents/w1%3Ap1/messages", "/agents/w1%3Ap2/messages", "/agents/w2%3Ap3/messages",
		"/agents/w1%3Ap1/controls", "/agents/w1%3Ap2/approval", "/agents/w1%3Ap1/approval",
		"/agents/w9%3Ap9", // not_found error body
		"/agents/w1%3Ap1/attachments/0000000000000000", // not_found error body
		"/agents/w1%3Ap1/tool-images/toolu_nope/0",     // not_found error body
	} {
		check(uri, app.get(t, uri).body)
	}
	// Error bodies from bad/oversized input.
	check("POST /agents", app.post(t, "/agents", `{"workspaceId":"w9","kind":"claude"}`).body)
	tooBig := `{"text":"` + strings.Repeat("x", 3<<20) + `"}`
	r := app.post(t, "/agents/w1%3Ap1/prompt", tooBig)
	if r.status != http.StatusRequestEntityTooLarge {
		t.Errorf("%d", r.status)
	}
	check("POST prompt", r.body)
}

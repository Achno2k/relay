package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay/internal/api"
)

// Behavior probed on the Swift (Hummingbird) bridge that the routes must keep.

func TestEdges_AuthBeforeRouting(t *testing.T) {
	ts := newTestServer(t, Options{})
	for _, uri := range []string{"/nope", "/agents/", "/agents/w1%3Ap1/nope", "/health/"} {
		r := do(t, ts, "GET", uri, "", nil)
		if r.status != http.StatusUnauthorized || r.body != `{"error":{"code":"unauthorized","message":"missing or invalid token"}}` {
			t.Errorf("%s: %d %s", uri, r.status, r.body)
		}
	}
}

func TestEdges_UnknownRouteOrMethodIs404(t *testing.T) {
	ts := newTestServer(t, Options{})
	want := `{"error":{"code":"not_found","message":"Not Found"}}`
	for _, c := range [][2]string{{"GET", "/nope"}, {"DELETE", "/agents"}, {"PUT", "/health"}, {"GET", "/agents/w1%3Ap1/prompt"}, {"GET", "/agents/a/b/c"}} {
		r := do(t, ts, c[0], c[1], "", auth)
		if r.status != http.StatusNotFound || r.body != want {
			t.Errorf("%s %s: %d %s", c[0], c[1], r.status, r.body)
		}
		if ct := r.header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("content type %q", ct)
		}
	}
	if r := do(t, ts, "HEAD", "/agents", "", auth); r.status != http.StatusNotFound {
		t.Errorf("HEAD: %d", r.status)
	}
}

func TestEdges_EmptySegmentsAreIgnored(t *testing.T) {
	ts := newTestServer(t, Options{})
	for _, uri := range []string{"/usage/", "//usage", "/usage//"} {
		if r := do(t, ts, "GET", uri, "", auth); r.status != http.StatusOK {
			t.Errorf("%s: %d", uri, r.status)
		}
	}
}

func TestEdges_HealthShape(t *testing.T) {
	ts := newTestServer(t, Options{HerdrReachable: func() bool { return true }})
	r := do(t, ts, "GET", "/health?x=1", "", nil)
	var h map[string]any
	if err := json.Unmarshal([]byte(r.body), &h); err != nil {
		t.Fatal(err)
	}
	if h["ok"] != true || h["name"] != "relay" || h["version"] != api.Version || h["herdr"] != "connected" || h["uptimeSeconds"] != 0.0 {
		t.Errorf("%s", r.body)
	}
	if r.header.Get("Server") != "relay" {
		t.Errorf("server header %q", r.header.Get("Server"))
	}
}

func TestEdges_BearerIsCaseInsensitiveAndTrimmed(t *testing.T) {
	ts := newTestServer(t, Options{})
	r := do(t, ts, "GET", "/usage", "", map[string]string{"Authorization": "bEaReR  " + testToken + " "})
	if r.status != http.StatusOK {
		t.Fatalf("%d", r.status)
	}
}

func TestEdges_BodyLimitsAndDecoding(t *testing.T) {
	ts := newTestServer(t, Options{})
	big := `{"text":"` + strings.Repeat("x", 3<<20) + `"}`
	cases := []struct{ uri, body, want string }{
		{"/agents/w1%3Ap1/prompt", big, `{"error":{"code":"too_large","message":"request body is limited to 2097152 bytes"}}`},
		{"/agents/w1%3Ap1/prompt", "nope", `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/prompt", "", `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/prompt", "[]", `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/prompt", `{}`, `{"error":{"code":"bad_request","message":"text or attachments is required"}}`},
		{"/agents/w1%3Ap1/prompt", `{"text":5}`, `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/keys", `{"Keys":["esc"]}`, `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/keys", `{"keys":null}`, `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents/w1%3Ap1/keys", `{"keys":[]}`, `{"error":{"code":"bad_request","message":"keys is required"}}`},
		{"/agents/w1%3Ap1/keys", `{"keys":["bogus"]}`, `{"error":{"code":"bad_request","message":"invalid key \"bogus\""}}`},
		{"/agents/w1%3Ap1/text", `{"text":""}`, `{"error":{"code":"bad_request","message":"text is required"}}`},
		{"/agents/w1%3Ap1/control", `{"model":"x","effort":"y"}`, `{"error":{"code":"bad_request","message":"send exactly one of model, permissionMode, effort, command"}}`},
		{"/agents/w1%3Ap1/control", `{}`, `{"error":{"code":"bad_request","message":"send exactly one of model, permissionMode, effort, command"}}`},
		{"/agents/w1%3Ap1/control", `{"command":"nuke"}`, `{"error":{"code":"bad_request","message":"command must be compact or clear"}}`},
		{"/agents", `{"workspaceId":"w1","kind":""}`, `{"error":{"code":"bad_request","message":"kind is required"}}`},
		{"/agents", `{"kind":"claude"}`, `{"error":{"code":"bad_request","message":"invalid JSON body"}}`},
		{"/agents", `{"workspaceId":"w1","kind":"claude","name":"Bad Name"}`, `{"error":{"code":"bad_request","message":"name must match [a-z][a-z0-9_-]{0,31}"}}`},
		{"/agents/w1%3Ap1/attachments", "", `{"error":{"code":"bad_request","message":"empty file"}}`},
		{"/agents/w1%3Ap1%00/prompt", `{"text":"hi"}`, `{"error":{"code":"bad_request","message":"invalid agent id"}}`},
	}
	for _, c := range cases {
		r := do(t, ts, "POST", c.uri, c.body, auth)
		if r.body != c.want {
			t.Errorf("%s %.40q: %d %s", c.uri, c.body, r.status, r.body)
		}
	}
	tooBig := strings.Repeat("x", 20<<20+1)
	r := do(t, ts, "POST", "/agents/w1%3Ap1/attachments", tooBig, auth)
	if r.status != http.StatusRequestEntityTooLarge || r.body != `{"error":{"code":"too_large","message":"files are limited to 20 MB"}}` {
		t.Errorf("upload: %d %s", r.status, r.body)
	}
}

func TestEdges_AgentErrorsMapThroughHerdr(t *testing.T) {
	ts := newTestServer(t, Options{})
	r := do(t, ts, "GET", "/agents/w1%3Ap1", "", auth)
	if r.status != http.StatusBadGateway || errorCode(t, r.body) != "unused" {
		t.Errorf("%d %s", r.status, r.body)
	}
	r = do(t, ts, "GET", "/agents/w1%3Ap1/attachments/abc", "", auth)
	if r.status != http.StatusNotFound || r.body != `{"error":{"code":"not_found","message":"no attachment (uploads expire after 7 days)"}}` {
		t.Errorf("%d %s", r.status, r.body)
	}
}

func TestEdges_PlainGetOnWSIsEmpty200(t *testing.T) {
	ts := newTestServer(t, Options{})
	r := do(t, ts, "GET", "/ws?token="+testToken, "", nil)
	if r.status != http.StatusOK || r.body != "" {
		t.Errorf("%d %q", r.status, r.body)
	}
	if r := do(t, ts, "GET", "/ws?token=nope", "", nil); r.status != http.StatusUnauthorized {
		t.Errorf("bad token: %d", r.status)
	}
	// The bearer header doesn't count on /ws.
	if r := do(t, ts, "GET", "/ws", "", auth); r.status != http.StatusUnauthorized {
		t.Errorf("header only: %d", r.status)
	}
}

func TestEdges_WSUnsubscribesWhenTheClientLeaves(t *testing.T) {
	hub := NewHub()
	ts := newTestServer(t, Options{Hub: hub})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws?token="+testToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, data, err := c.Read(ctx); err != nil || string(data) != `{"type":"hello"}` {
		t.Fatalf("%s %v", data, err)
	}
	c.Close(websocket.StatusNormalClosure, "")
	for hub.Count() != 0 {
		if ctx.Err() != nil {
			t.Fatal("still subscribed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHub_DropsOldestWhenAClientFallsBehind(t *testing.T) {
	hub := NewHub()
	id, ch := hub.Subscribe()
	for i := range hubBuffer + 5 {
		hub.Broadcast(api.AgentClosed(string(rune('a' + i%26))))
	}
	if len(ch) != hubBuffer {
		t.Fatalf("buffered %d", len(ch))
	}
	first := <-ch
	if first.AgentID != string(rune('a'+5%26)) {
		t.Errorf("first kept = %q", first.AgentID)
	}
	hub.Unsubscribe(id)
	hub.Unsubscribe(id)
	hub.Broadcast(api.Hello()) // no subscribers, no panic on the closed channel
	if hub.Count() != 0 {
		t.Error("count")
	}
}

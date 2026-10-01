package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

func resilienceHandler(method string, _ map[string]any) any {
	switch method {
	case "agent.list":
		return map[string]any{"type": "agent_list", "agents": []any{}}
	case "workspace.list":
		return map[string]any{"type": "workspace_list", "workspaces": []any{}}
	case "events.subscribe":
		return map[string]any{"type": "subscription_started"}
	}
	return herdrtest.Error{Code: "unknown_method", Message: method}
}

// poll retries cond every 50 ms for up to 2 s (Swift: SocketResilienceTests.poll).
func poll(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

func nowhereService(t *testing.T, socketPath string) *Service {
	return New(Deps{
		Herdr:   herdr.NewClient(socketPath),
		Locator: transcript.NewLocator("/nonexistent", transcript.NewCodexRollouts("/nonexistent")),
		Uploads: uploads.NewStore(t.TempDir()),
	})
}

// Swift: SocketResilienceTests (the herdr-only cases live in internal/herdr).
func TestSocketResilience_RestRouteAnswers503FastWhenHerdrDies(t *testing.T) {
	fake := herdrtest.New(t, resilienceHandler)
	ts := httptest.NewServer(server.New(server.Options{Backend: nowhereService(t, fake.SocketPath), Hub: server.NewHub(), Token: "t"}))
	defer ts.Close()
	get := func() (int, string) {
		req, _ := http.NewRequest("GET", ts.URL+"/agents", nil)
		req.Header.Set("Authorization", "Bearer t")
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body api.ErrorBody
		_ = decodeJSON(res.Body, &body)
		return res.StatusCode, body.Error.Code
	}
	if s, _ := get(); s != http.StatusOK {
		t.Fatalf("before: %d", s)
	}
	fake.Stop()
	start := time.Now()
	if s, code := get(); s != http.StatusServiceUnavailable || code != "herdr_unavailable" {
		t.Errorf("after stop: %d %s", s, code)
	}
	if d := time.Since(start); d >= 3*time.Second {
		t.Errorf("took %v", d)
	}
	// herdr comes back on the same socket path: the next call succeeds again.
	herdrtest.Reusing(t, fake, resilienceHandler)
	if !poll(func() bool { s, _ := get(); return s == http.StatusOK }) {
		t.Error("never back up")
	}
}

func TestSocketResilience_HealthReflectsHerdrReachabilityAcrossARestart(t *testing.T) {
	fake := herdrtest.New(t, resilienceHandler)
	m := NewMonitor(nowhereService(t, fake.SocketPath), server.NewHub(), herdr.NewEventStream(fake.SocketPath), MonitorOptions{Interval: time.Hour})
	ctx := context.Background()
	if !poll(func() bool { m.Trigger(ctx); return m.HerdrReachable() }) {
		t.Fatal("never reachable")
	}
	fake.Stop()
	if !poll(func() bool { m.Trigger(ctx); return !m.HerdrReachable() }) {
		t.Fatal("never unreachable")
	}
	herdrtest.Reusing(t, fake, resilienceHandler)
	if !poll(func() bool { m.Trigger(ctx); return m.HerdrReachable() }) {
		t.Fatal("never reachable again")
	}
}

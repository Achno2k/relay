package server

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/usage"
)

func notReadyPi(string) []byte { return []byte(`{"status":"invalid"}`) }

// Swift: UsageRoutesTests.
func TestUsageRoutes_GetUsageServesTheCacheWithoutFetching(t *testing.T) {
	codex, err := os.ReadFile("testdata/usage-codex-ratelimits.json")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	m := usage.NewMonitorWith(hub, usage.Options{
		Codex: func() []byte { return codex }, ClaudeUsage: func(string) []byte { return nil },
		ClaudeAuth: func() []byte { return nil }, PiAuth: notReadyPi, ProbeDir: t.TempDir(),
	})
	m.RequestRefresh()
	time.Sleep(150 * time.Millisecond)
	ts := newTestServer(t, Options{Hub: hub, Usage: m})
	r := do(t, ts, "GET", "/usage", "", auth)
	if r.status != http.StatusOK {
		t.Fatalf("status %d", r.status)
	}
	var snap api.UsageSnapshot
	if err := json.Unmarshal([]byte(r.body), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Providers) != 2 {
		t.Fatalf("providers = %d: %s", len(snap.Providers), r.body)
	}
	for _, p := range snap.Providers {
		if p.ID == "codex" && len(p.Windows) == 0 {
			t.Errorf("codex has no windows")
		}
	}
}

func TestUsageRoutes_GetUsageWithNoMonitorReturnsEmptyProviders(t *testing.T) {
	ts := newTestServer(t, Options{})
	r := do(t, ts, "GET", "/usage", "", auth)
	if r.status != http.StatusOK || r.body != `{"providers":[]}` {
		t.Fatalf("%d %s", r.status, r.body)
	}
}

func TestUsageRoutes_RefreshIsThrottledAfterFirstCall(t *testing.T) {
	hub := NewHub()
	m := usage.NewMonitorWith(hub, usage.Options{
		Codex: func() []byte { return nil }, ClaudeUsage: func(string) []byte { return nil },
		ClaudeAuth: func() []byte { return nil }, PiAuth: notReadyPi, ProbeDir: t.TempDir(),
	})
	ts := newTestServer(t, Options{Hub: hub, Usage: m})
	if r := do(t, ts, "POST", "/usage/refresh", "", auth); r.status != http.StatusAccepted {
		t.Fatalf("first: %d", r.status)
	}
	r := do(t, ts, "POST", "/usage/refresh", "", auth)
	if r.status != http.StatusTooManyRequests || errorCode(t, r.body) != "rate_limited" {
		t.Fatalf("second: %d %s", r.status, r.body)
	}
}

func TestUsageRoutes_UsageNeedsAuth(t *testing.T) {
	ts := newTestServer(t, Options{})
	if r := do(t, ts, "GET", "/usage", "", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("status %d", r.status)
	}
}

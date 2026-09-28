package service

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

func nextEvent(t *testing.T, events <-chan api.ServerEvent, want api.EventType) api.ServerEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Type == want {
				return e
			}
		case <-deadline:
			t.Fatalf("no %s", want)
		}
	}
}

// Go-only: the rest of the monitor's deltas (Swift covered updated only) and its tailers.
func TestMonitor_CreatedClosedAndTailedMessages(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, transcript.ProjectDirName("/Users/dev/shop-api"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(file, []byte(`{"type":"user","isSidechain":false,"uuid":"u1","timestamp":"2026-09-23T13:00:00.000Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	withSecond := false
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.list":
			agents := []any{herdrtest.AgentJSON("w1:p1", str("a"), "working", "/Users/dev/shop-api",
				map[string]any{"source": "herdr:claude", "agent": "claude", "kind": "id", "value": "sess-1"}, "")}
			mu.Lock()
			if withSecond {
				agents = append(agents, herdrtest.AgentJSON("w1:p2", str("b"), "idle", "/Users/dev/shop-api", nil, ""))
			}
			mu.Unlock()
			return map[string]any{"type": "agent_list", "agents": agents}
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.read":
			return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": params["target"], "workspace_id": "w1", "tab_id": "w1:t1",
				"source": "detection", "format": "text", "text": "", "revision": 1, "truncated": false}}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	svc := New(Deps{
		Herdr:   herdr.NewClient(fake.SocketPath),
		Locator: transcript.NewLocator(projects, transcript.NewCodexRollouts(filepath.Join(projects, "codex"))),
		Uploads: uploads.NewStore(t.TempDir()),
	})
	hub := server.NewHub()
	sid, events := hub.Subscribe()
	defer hub.Unsubscribe(sid)
	var landedMu sync.Mutex
	var landed []string
	m := NewMonitor(svc, hub, herdr.NewEventStream(fake.SocketPath), MonitorOptions{
		Interval: time.Hour,
		OnMessage: func(id string, msg api.Message) {
			landedMu.Lock()
			landed = append(landed, id+"/"+msg.ID)
			landedMu.Unlock()
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	if !poll(func() bool { return m.TailerCount() == 1 }) {
		t.Fatal("no tailer")
	}
	mu.Lock()
	withSecond = true
	mu.Unlock()
	m.Trigger(ctx)
	if e := nextEvent(t, events, api.EventAgentCreated); e.Agent.ID != "w1:p2" {
		t.Errorf("created %+v", e.Agent)
	}
	mu.Lock()
	withSecond = false
	mu.Unlock()
	m.Trigger(ctx)
	if e := nextEvent(t, events, api.EventAgentClosed); e.AgentID != "w1:p2" {
		t.Errorf("closed %s", e.AgentID)
	}

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant","isSidechain":false,"uuid":"a1","timestamp":"2026-09-23T13:00:01.000Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n")
	f.Close()
	e := nextEvent(t, events, api.EventMessageUpserted)
	if e.AgentID != "w1:p1" || e.Message.ID != "a1" {
		t.Errorf("upserted %s %+v", e.AgentID, e.Message)
	}
	landedMu.Lock()
	defer landedMu.Unlock()
	if len(landed) != 1 || landed[0] != "w1:p1/a1" {
		t.Errorf("onMessage %v", landed)
	}
}

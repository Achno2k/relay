package live

import (
	"context"
	"sync"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

// Ported from LiveReplyMonitorTests.swift. Swift drove the monitor through AgentMonitor; here the
// test feeds Update the way service's agent monitor does, so the live package stands alone.

type testHub struct {
	events chan api.ServerEvent
}

func newTestHub() *testHub                     { return &testHub{events: make(chan api.ServerEvent, 256)} }
func (h *testHub) Broadcast(ev api.ServerEvent) { h.events <- ev }
func (h *testHub) Count() int                   { return 1 }

// nextReplyLiveText is the next `reply.live` frame for agentID whose text presence is nonNil,
// ignoring anything else, or ok=false if none arrives within the timeout.
func nextReplyLiveText(h *testHub, agentID string, nonNil bool) (text *string, ok bool) {
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev.Type == api.EventReplyLive && ev.AgentID == agentID && (ev.Text != nil) == nonNil {
				return ev.Text, true
			}
		case <-deadline:
			return nil, false
		}
	}
}

// fakeScreen is a fake herdr whose visible screen can change mid-test.
func fakeScreen(t *testing.T, screen *string, mu *sync.Mutex) *herdrtest.Server {
	return herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.read":
			mu.Lock()
			text := *screen
			mu.Unlock()
			source, _ := params["source"].(string)
			if source == "detection" {
				text = ""
			}
			return map[string]any{"type": "pane_read", "read": map[string]any{
				"pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1", "source": source,
				"format": params["format"], "text": text, "revision": 1, "truncated": false,
			}}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
}

func agentWith(status api.AgentStatus) []Agent {
	return []Agent{{ID: "w1:p1", Kind: "claude", Status: status, Cwd: "/Users/dev/shop-api"}}
}

func TestWorkingAgentProducesALiveFrame(t *testing.T) {
	var mu sync.Mutex
	screen := "❯ hi\n\n⏺ Not yet"
	fake := fakeScreen(t, &screen, &mu)
	hub := newTestHub()
	m := NewMonitorWith(herdr.NewClient(fake.SocketPath), hub, NewTracker(250*time.Millisecond, nil), 20*time.Millisecond)

	// Idle: no working agents yet, no frame even once the monitor is polling.
	m.Update(agentWith(api.StatusIdle))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(80 * time.Millisecond)
	select {
	case ev := <-hub.events:
		t.Fatalf("frame while idle: %+v", ev)
	default:
	}

	// The agent starts working with an in-progress `⏺` block on screen.
	mu.Lock()
	screen = "❯ hi\n\n⏺ Hello there"
	mu.Unlock()
	m.Update(agentWith(api.StatusWorking))

	text, ok := nextReplyLiveText(hub, "w1:p1", true)
	if !ok || *text != "Hello there" {
		t.Fatalf("got %v %v, want Hello there", text, ok)
	}
	if !slicesContain(fake.Methods(), "agent.read") {
		t.Fatal("never read the screen")
	}
}

func TestAgentGoingIdleClearsTheLiveFrame(t *testing.T) {
	var mu sync.Mutex
	screen := "❯ hi\n\n⏺ Growing reply"
	fake := fakeScreen(t, &screen, &mu)
	hub := newTestHub()
	m := NewMonitorWith(herdr.NewClient(fake.SocketPath), hub, NewTracker(250*time.Millisecond, nil), 20*time.Millisecond)

	m.Update(agentWith(api.StatusWorking))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	text, ok := nextReplyLiveText(hub, "w1:p1", true)
	if !ok || *text != "Growing reply" {
		t.Fatalf("got %v %v, want Growing reply", text, ok)
	}

	m.Update(agentWith(api.StatusIdle))
	if _, ok := nextReplyLiveText(hub, "w1:p1", false); !ok {
		t.Fatal("no clearing frame")
	}
}

func TestAReadInFlightWhenTheAgentStopsDoesNotReopenThePreview(t *testing.T) {
	// A read that answers after the agent stopped must not show text nothing would clear again.
	release := make(chan struct{})
	reading := make(chan struct{}, 1)
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		reading <- struct{}{}
		<-release
		return map[string]any{"type": "pane_read", "read": map[string]any{
			"pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1", "source": "visible",
			"format": "ansi", "text": "❯ hi\n\n⏺ Late text", "revision": 1, "truncated": false,
		}}
	})
	hub := newTestHub()
	m := NewMonitorWith(herdr.NewClient(fake.SocketPath), hub, NewTracker(0, nil), time.Hour)
	m.Update(agentWith(api.StatusWorking))
	done := make(chan struct{})
	go func() {
		m.tick(context.Background())
		close(done)
	}()
	<-reading
	m.Update(agentWith(api.StatusIdle))
	close(release)
	<-done
	select {
	case ev := <-hub.events:
		t.Fatalf("frame after the agent stopped: %+v", ev)
	default:
	}
}

func TestReadsTheVisibleScreenWithANSI(t *testing.T) {
	var mu sync.Mutex
	screen := "❯ hi\n\n⏺ Hi"
	fake := fakeScreen(t, &screen, &mu)
	hub := newTestHub()
	m := NewMonitorWith(herdr.NewClient(fake.SocketPath), hub, NewTracker(0, nil), time.Hour)
	m.Update(agentWith(api.StatusWorking))
	m.tick(context.Background())
	expectEqual(t, fake.Params("agent.read"), `{"format":"ansi","source":"visible","strip_ansi":false,"target":"w1:p1"}`)
}

func slicesContain(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

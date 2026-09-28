package herdr_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

// Simulates herdr disappearing and coming back, with herdrtest rather than the real herdr.
// The route/monitor halves of Swift's SocketResilienceTests (restRouteAnswers503FastWhenHerdrDies,
// healthReflectsHerdrReachabilityAcrossARestart) live with go-server.

func herdrHandler(method string, _ map[string]any) any {
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

func nextEvent(s *herdr.EventStream, timeout time.Duration) (herdr.Event, bool) {
	select {
	case e, ok := <-s.Events():
		return e, ok
	case <-time.After(timeout):
		return herdr.Event{}, false
	}
}

func TestRestIsFastNotHungWhenSocketIsGone(t *testing.T) {
	c := herdr.NewClient(fmt.Sprintf("/tmp/relay-missing-%08x.sock", rand.Uint32()))
	start := time.Now()
	_, err := c.Agents(context.Background())
	var he *herdr.Error
	if !errors.As(err, &he) || he.Kind != herdr.ErrUnavailable {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if time.Since(start) >= 2*time.Second {
		t.Fatal("too slow")
	}
}

func TestEventStreamReconnectsAfterHerdrRestarts(t *testing.T) {
	fake := herdrtest.New(t, herdrHandler)
	s := herdr.NewEventStream(fake.SocketPath)
	s.Start()
	defer s.Stop()

	if e, ok := nextEvent(s, 3*time.Second); !ok || e.Kind != "resync" {
		t.Fatalf("first = %+v %v", e, ok)
	}
	fake.Stop()
	time.Sleep(200 * time.Millisecond)
	herdrtest.Reusing(t, fake, herdrHandler)
	if e, ok := nextEvent(s, 8*time.Second); !ok || e.Kind != "resync" {
		t.Fatalf("second = %+v %v", e, ok)
	}
}

// Go-only: a hung herdr (accepts, never answers) times out instead of hanging.
func TestCallTimesOutWhenHerdrHangs(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	fake := herdrtest.New(t, func(string, map[string]any) any { <-block; return map[string]any{} })
	c := herdr.NewClient(fake.SocketPath)
	start := time.Now()
	err := c.Call(context.Background(), "agent.list", map[string]any{}, nil, 300*time.Millisecond)
	var he *herdr.Error
	if !errors.As(err, &he) || he.Kind != herdr.ErrTimeout {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("too slow")
	}
}

// Go-only: cancelling the context unblocks a pending call.
func TestCallHonoursContext(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	fake := herdrtest.New(t, func(string, map[string]any) any { <-block; return map[string]any{} })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := herdr.NewClient(fake.SocketPath).Agents(ctx); err == nil {
		t.Fatal("expected an error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("ctx ignored")
	}
}

// Go-only: Watch with a new pane set resubscribes with that set.
func TestWatchResubscribesWithPanes(t *testing.T) {
	fake := herdrtest.New(t, herdrHandler)
	s := herdr.NewEventStream(fake.SocketPath)
	s.Start()
	defer s.Stop()
	if e, _ := nextEvent(s, 3*time.Second); e.Kind != "resync" {
		t.Fatal("no first resync")
	}
	s.Watch([]string{"w1:p2", "w1:p1"})
	if e, _ := nextEvent(s, 3*time.Second); e.Kind != "resync" {
		t.Fatal("no resync after watch")
	}
	p := fake.Params("events.subscribe")
	if want := `{"pane_id":"w1:p1","type":"pane.agent_status_changed"},{"pane_id":"w1:p2","type":"pane.agent_status_changed"}]}`; len(p) < len(want) || p[len(p)-len(want):] != want {
		t.Fatalf("params = %s", p)
	}
}

// Go-only: Stop before Start closes Events, and Stop twice is safe.
func TestStopBeforeStart(t *testing.T) {
	s := herdr.NewEventStream("/tmp/nope")
	s.Stop()
	s.Stop()
	if _, ok := <-s.Events(); ok {
		t.Fatal("events not closed")
	}
	s.Start()
}

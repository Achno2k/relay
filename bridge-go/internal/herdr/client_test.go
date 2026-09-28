package herdr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

func TestClientRoundTrip(t *testing.T) {
	name := "a"
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.list":
			return map[string]any{"agents": []any{herdrtest.AgentJSON("w1:p1", &name, "idle", "/tmp/x", nil, "")}}
		case "agent.get":
			return herdrtest.Error{Code: "pane_not_found", Message: "no pane"}
		}
		return map[string]any{}
	})
	c := herdr.NewClient(fake.SocketPath)
	ctx := context.Background()
	agents, err := c.Agents(ctx)
	if err != nil || len(agents) != 1 || *agents[0].Name != "a" || agents[0].AgentStatus != api.StatusIdle || agents[0].StateChangeSeq != 3 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	_, err = c.Agent(ctx, "w1:p9")
	if e := api.FromError(err); e.Status != 404 || e.Code != "not_found" {
		t.Fatalf("mapped = %+v", e)
	}
	if err := c.SendKeys(ctx, "w1:p1", []string{"esc"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Params("agent.send_keys"); got != `{"keys":["esc"],"target":"w1:p1"}` {
		t.Fatalf("params = %s", got)
	}
	fake.Stop()
	_, err = c.Agents(ctx)
	var he *herdr.Error
	if !errors.As(err, &he) || he.Kind != herdr.ErrUnavailable || api.FromError(err).Code != "herdr_unavailable" {
		t.Fatalf("after stop: %v", err)
	}
}

func TestEventStreamResyncAndEvents(t *testing.T) {
	fake := herdrtest.New(t, func(string, map[string]any) any { return map[string]any{} })
	s := herdr.NewEventStream(fake.SocketPath)
	s.Start()
	defer s.Stop()
	want := func(kind string) herdr.Event {
		select {
		case e := <-s.Events():
			if e.Kind != kind {
				t.Fatalf("got %+v, want %s", e, kind)
			}
			return e
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s", kind)
		}
		return herdr.Event{}
	}
	want("resync")
	fake.Push(`{"event":"pane_agent_status_changed","data":{"pane_id":"w1:p1"}}`)
	if e := want("pane.agent.status.changed"); e.PaneID != "w1:p1" {
		t.Fatalf("%+v", e)
	}
}

func TestMarshalShapes(t *testing.T) {
	a := api.Agent{ID: "w1:p1", Kind: "claude", Status: api.StatusIdle, TranscriptState: api.TranscriptReady}
	b, _ := api.Marshal(a)
	if string(b) != `{"id":"w1:p1","name":null,"kind":"claude","title":"","workspaceId":"","workspaceName":"","cwdName":"","status":"idle","hasTranscript":false,"updatedAt":"","model":null,"modelLabel":null,"permissionMode":null,"effort":null,"sessionId":null,"transcriptState":"ready"}` {
		t.Fatal(string(b))
	}
	b, _ = api.Marshal(api.ReplyLive("w1:p1", nil, nil, 2))
	if string(b) != `{"type":"reply.live","agentId":"w1:p1","text":null,"tool":null,"seq":2}` {
		t.Fatal(string(b))
	}
	b, _ = api.Marshal(api.MessagePage{Messages: []api.Message{{ID: "m", Role: api.RoleUser, Blocks: []api.Block{api.TextBlock("<a & b>")}}}})
	if string(b) != `{"messages":[{"id":"m","role":"user","createdAt":"","blocks":[{"type":"text","text":"<a & b>"}]}],"hasMore":false}` {
		t.Fatal(string(b))
	}
	b, _ = api.Marshal(api.UsageProvider{ID: "claude"})
	if string(b) != `{"id":"claude","label":"","windows":[],"updatedAt":"","source":"","stale":false,"usedBy":[]}` {
		t.Fatal(string(b))
	}
}

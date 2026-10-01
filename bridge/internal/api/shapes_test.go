package api

import (
	"testing"
	"time"
)

// Exact Swift encodings: which nils are omitted, which are null, [] vs null, no HTML escaping.
func TestSwiftShapes(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"agent nils are null", Agent{ID: "w1:p1", Kind: "claude", Status: StatusIdle, TranscriptState: TranscriptReady},
			`{"id":"w1:p1","name":null,"kind":"claude","title":"","workspaceId":"","workspaceName":"","cwdName":"","status":"idle","hasTranscript":false,"updatedAt":"","model":null,"modelLabel":null,"permissionMode":null,"effort":null,"sessionId":null,"transcriptState":"ready"}`},
		{"reply.live nils are null", ReplyLive("w1:p1", nil, nil, 2),
			`{"type":"reply.live","agentId":"w1:p1","text":null,"tool":null,"seq":2}`},
		{"reply.live tool", ReplyLive("w1:p1", Str("hi"), &LiveTool{"Bash", "Ran ls", "running"}, 3),
			`{"type":"reply.live","agentId":"w1:p1","text":"hi","tool":{"name":"Bash","summary":"Ran ls","state":"running"},"seq":3}`},
		{"no html escaping", MessagePage{Messages: []Message{{ID: "m", Role: RoleUser, Blocks: []Block{TextBlock("<a & b> /x")}}}},
			`{"messages":[{"id":"m","role":"user","createdAt":"","blocks":[{"type":"text","text":"<a & b> /x"}]}],"hasMore":false}`},
		{"empty page", MessagePage{}, `{"messages":[],"hasMore":false}`},
		{"usage nils omitted, arrays empty", UsageProvider{ID: "claude"},
			`{"id":"claude","label":"","windows":[],"updatedAt":"","source":"","stale":false,"usedBy":[]}`},
		{"usage window nils omitted", UsageWindow{ID: "session", Label: "Session"}, `{"id":"session","label":"Session"}`},
		{"option freeText omitted", ApprovalOption{Label: "Yes"}, `{"label":"Yes","keys":[]}`},
		{"approval step omitted", Approval{AgentID: "a", Question: "q"}, `{"agentId":"a","question":"q","options":[]}`},
		{"step title omitted", ApprovalStep{Index: 1, Count: 2}, `{"index":1,"count":2}`},
		{"agent controls optionals omitted", AgentControls{},
			`{"models":[],"efforts":[],"modes":[],"supports":{"model":false,"effort":false,"mode":false,"compact":false,"clear":false}}`},
		{"empty effortsByModel kept", AgentControls{EffortsByModel: map[string][]ControlChoice{"m": nil}},
			`{"models":[],"efforts":[],"modes":[],"supports":{"model":false,"effort":false,"mode":false,"compact":false,"clear":false},"effortsByModel":{"m":[]}}`},
		{"error body", NotFound("no agent").Body(), `{"error":{"code":"not_found","message":"no agent"}}`},
		{"health", NewHealth("connected", 42), `{"ok":true,"name":"relay","version":"0.1.0","herdr":"connected","uptimeSeconds":42}`},
		{"empty", Empty{}, `{}`},
		{"tool result", ToolResultBlock("t1", true, "boom"), `{"type":"toolResult","toolCallId":"t1","isError":true,"preview":"boom"}`},
		{"attachment", AttachmentBlock("0123456789abcdef", "a.png", AttachmentImage), `{"type":"attachment","id":"0123456789abcdef","name":"a.png","kind":"image"}`},
	}
	for _, c := range cases {
		b, err := Marshal(c.v)
		if err != nil || string(b) != c.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", c.name, b, err, c.want)
		}
	}
}

func TestTimestamps(t *testing.T) {
	if got := FormatTime(time.Date(2026, 9, 23, 15, 4, 1, 900, time.FixedZone("x", 7200))); got != "2026-09-23T13:04:01+00:00" {
		t.Fatal(got)
	}
	for in, want := range map[any]string{
		"2026-09-23T13:04:01.123Z":   "2026-09-23T13:04:01+00:00",
		"2026-09-23T13:04:01Z":       "2026-09-23T13:04:01+00:00",
		"2026-09-23T15:04:01+02:00":  "2026-09-23T13:04:01+00:00",
		"2026-09-23T15:04:01.5+0200": "2026-09-23T13:04:01+00:00",
		float64(1790168641000):       "2026-09-23T13:04:01+00:00",
	} {
		if got, ok := NormalizeTimestamp(in); !ok || got != want {
			t.Errorf("%v: got %q %v", in, got, ok)
		}
	}
	if _, ok := NormalizeTimestamp("yesterday"); ok {
		t.Error("parsed junk")
	}
}

func TestUsageFreshness(t *testing.T) {
	now := time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC)
	p := UsageProvider{ID: "claude", UpdatedAt: FormatTime(now.Add(-time.Hour))}
	if p.MarkedStale(now, 2*time.Hour).Stale || !p.MarkedStale(now, 30*time.Minute).Stale {
		t.Fatal("staleness")
	}
	if !(UsageProvider{UpdatedAt: "junk"}).MarkedStale(now, time.Hour).Stale {
		t.Fatal("junk date not stale")
	}
	q := p
	q.UpdatedAt, q.Stale = "x", true
	if !p.SameDataExcludingFreshness(q) {
		t.Fatal("freshness compared")
	}
	q.Plan = Str("Max")
	if p.SameDataExcludingFreshness(q) {
		t.Fatal("plan ignored")
	}
}

func TestAgentDecodeDerivesTranscriptState(t *testing.T) {
	var a Agent
	if err := a.UnmarshalJSON([]byte(`{"id":"x","kind":"pi","hasTranscript":false}`)); err != nil || a.TranscriptState != TranscriptPending {
		t.Fatal(a.TranscriptState, err)
	}
}

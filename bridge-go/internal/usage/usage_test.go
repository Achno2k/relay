package usage

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"relay/internal/api"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func findWindow(p *api.UsageProvider, id string) *api.UsageWindow {
	for i := range p.Windows {
		if p.Windows[i].ID == id {
			return &p.Windows[i]
		}
	}
	return nil
}

func find(ps []api.UsageProvider, id string) *api.UsageProvider {
	for i := range ps {
		if ps[i].ID == id {
			return &ps[i]
		}
	}
	return nil
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func slice(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}

// MARK: UsageParsersTests

func TestParsesCodexRateLimits(t *testing.T) {
	p := ParseCodex(fixture(t, "usage-codex-ratelimits.json"), time.Now(), false)
	if p == nil {
		t.Fatal("nil")
	}
	eq(t, "id", p.ID, "codex")
	eq(t, "label", p.Label, "ChatGPT")
	eq(t, "plan", *p.Plan, "Plus")
	eq(t, "stale", p.Stale, false)
	slice(t, "usedBy", p.UsedBy, []string{"codex"})
	eq(t, "windows", len(p.Windows), 2)
	primary := findWindow(p, "primary")
	eq(t, "primary %", *primary.UsedPercent, 39)
	eq(t, "primary mins", *primary.WindowMinutes, 300)
	eq(t, "primary label", primary.Label, "5-hour")
	secondary := findWindow(p, "secondary")
	eq(t, "secondary %", *secondary.UsedPercent, 18)
	eq(t, "secondary label", secondary.Label, "Weekly")
}

func TestParsesCodexWithPiJoined(t *testing.T) {
	p := ParseCodex(fixture(t, "usage-codex-ratelimits.json"), time.Now(), true)
	slice(t, "usedBy", p.UsedBy, []string{"codex", "pi"})
}

func TestCodexMissingResultIsNil(t *testing.T) {
	if ParseCodex([]byte("{}"), time.Now(), false) != nil || ParseCodex([]byte("not json"), time.Now(), false) != nil {
		t.Error("expected nil")
	}
}

func TestParsesClaudeUsageReport(t *testing.T) {
	p := ParseClaude(fixture(t, "usage-claude-stream.jsonl"), fixture(t, "usage-claude-auth-status.json"), time.Now(), false)
	if p == nil {
		t.Fatal("nil")
	}
	eq(t, "id", p.ID, "claude")
	eq(t, "label", p.Label, "Claude")
	eq(t, "plan", *p.Plan, "Max")
	slice(t, "usedBy", p.UsedBy, []string{"claude"})
	eq(t, "windows", len(p.Windows), 2)
	session := findWindow(p, "session")
	eq(t, "session %", *session.UsedPercent, 11)
	eq(t, "session resets", *session.ResetsAt, "2026-09-24T23:00:00+00:00")
	eq(t, "week %", *findWindow(p, "weekly_all").UsedPercent, 11)
	// weekly_scoped (per-model, e.g. Fable) is intentionally left out of v1.
	if findWindow(p, "weekly_scoped") != nil {
		t.Error("weekly_scoped included")
	}
}

func TestClaudeWorksWithoutAuthStatus(t *testing.T) {
	p := ParseClaude(fixture(t, "usage-claude-stream.jsonl"), nil, time.Now(), false)
	if p.Plan != nil || len(p.Windows) != 2 {
		t.Errorf("provider %+v", p)
	}
}

func TestClaudeNilUsageIsNil(t *testing.T) {
	if ParseClaude(nil, nil, time.Now(), false) != nil {
		t.Error("expected nil")
	}
}

func TestClaudeNoUsageReportLineIsNil(t *testing.T) {
	if ParseClaude([]byte("{\"type\":\"system\"}\n{\"type\":\"result\"}\n"), nil, time.Now(), false) != nil {
		t.Error("expected nil")
	}
}

func TestParsesClaudeWithPiJoined(t *testing.T) {
	p := ParseClaude(fixture(t, "usage-claude-stream.jsonl"), nil, time.Now(), true)
	slice(t, "usedBy", p.UsedBy, []string{"claude", "pi"})
}

func TestOpenCodeGoProviderNilWhenPiNotReady(t *testing.T) {
	if OpenCodeGoProvider(false, time.Now()) != nil {
		t.Error("expected nil")
	}
}

func TestOpenCodeGoProviderShapeWhenPiReady(t *testing.T) {
	p := OpenCodeGoProvider(true, time.Now())
	eq(t, "id", p.ID, "opencode-go")
	eq(t, "label", p.Label, "OpenCode Go")
	eq(t, "plan", *p.Plan, "OpenCode Go")
	eq(t, "windows", len(p.Windows), 0)
	eq(t, "stale", p.Stale, false)
	eq(t, "reason", *p.UnavailableReason, "Usage not available from OpenCode")
	slice(t, "usedBy", p.UsedBy, []string{"pi"})
}

// The wire shape: nil optionals are omitted, like Swift's synthesized Codable.
func TestProviderJSONShape(t *testing.T) {
	p := ParseCodex([]byte(`{"result":{"rateLimits":{"primary":{"usedPercent":39}}}}`), time.Unix(0, 0), false)
	b, _ := api.Marshal(p)
	want := `{"id":"codex","label":"ChatGPT","windows":[{"id":"primary","label":"Primary","usedPercent":39}],"updatedAt":"1970-01-01T00:00:00+00:00","source":"codex app-server","stale":false,"usedBy":["codex"]}`
	eq(t, "json", string(b), want)
}

// MARK: UsageMonitorTests

type fakeHub struct {
	mu     sync.Mutex
	count  int
	events []api.ServerEvent
}

func (h *fakeHub) Broadcast(e api.ServerEvent) {
	h.mu.Lock()
	h.events = append(h.events, e)
	h.mu.Unlock()
}

func (h *fakeHub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func (h *fakeHub) setCount(n int) {
	h.mu.Lock()
	h.count = n
	h.mu.Unlock()
}

type probes struct {
	codex, usage, auth []byte
	ready              map[string]bool
}

func monitor(t *testing.T, hub Hub, p probes, o Options) *Monitor {
	t.Helper()
	if o.Codex == nil {
		o.Codex = func(context.Context) []byte { return p.codex }
	}
	o.ClaudeUsage = func(context.Context, string) []byte { return p.usage }
	o.ClaudeAuth = func(context.Context) []byte { return p.auth }
	// ready names the providers `pi auth check` reports ready; everything else is invalid, which
	// matches "pi not installed".
	o.PiAuth = func(_ context.Context, provider string) []byte {
		if p.ready[provider] {
			return []byte(`{"status":"ready"}`)
		}
		return []byte(`{"status":"invalid"}`)
	}
	o.ProbeDir = t.TempDir()
	return NewMonitorWith(hub, o)
}

// requestRefreshAndWait forces an immediate poll and waits for it to land.
func requestRefreshAndWait(m *Monitor) {
	m.RequestRefresh()
	m.bg.Wait()
}

func TestSeedsCacheAtStartupWithNoSubscribers(t *testing.T) {
	m := monitor(t, &fakeHub{}, probes{codex: fixture(t, "usage-codex-ratelimits.json"), usage: fixture(t, "usage-claude-stream.jsonl")}, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	snap := m.Snapshot()
	eq(t, "count", len(snap), 2)
	if c := find(snap, "codex"); c == nil || len(c.Windows) == 0 {
		t.Error("codex missing")
	}
	if c := find(snap, "claude"); c == nil || len(c.Windows) == 0 {
		t.Error("claude missing")
	}
}

func TestUnavailableProbeMarksStaleWithReason(t *testing.T) {
	m := monitor(t, &fakeHub{}, probes{}, Options{})
	requestRefreshAndWait(m)
	codex := find(m.Snapshot(), "codex")
	if codex == nil || !codex.Stale || codex.UnavailableReason == nil || len(codex.Windows) != 0 {
		t.Fatalf("codex %+v", codex)
	}
	slice(t, "usedBy", codex.UsedBy, []string{"codex"})
}

func TestManualRefreshIsThrottled(t *testing.T) {
	m := monitor(t, &fakeHub{}, probes{}, Options{ManualCooldown: 15 * time.Second})
	eq(t, "first", m.RequestRefresh(), true)
	eq(t, "second", m.RequestRefresh(), false)
	m.bg.Wait()
}

func TestStaleAfterWindowMarksCachedDataStale(t *testing.T) {
	var mu sync.Mutex
	now := time.Unix(1_000_000, 0)
	m := monitor(t, &fakeHub{}, probes{codex: fixture(t, "usage-codex-ratelimits.json")}, Options{
		StaleAfter: 60 * time.Second,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		},
	})
	requestRefreshAndWait(m)
	eq(t, "fresh", find(m.Snapshot(), "codex").Stale, false)
	mu.Lock()
	now = now.Add(120 * time.Second)
	mu.Unlock()
	eq(t, "stale", find(m.Snapshot(), "codex").Stale, true)
}

func TestOnlyPollsWhileAClientIsConnected(t *testing.T) {
	hub := &fakeHub{}
	var calls atomic.Int32
	m := monitor(t, hub, probes{}, Options{
		Codex:        func(context.Context) []byte { calls.Add(1); return nil },
		Tick:         50 * time.Millisecond,
		BaseInterval: 10 * time.Millisecond,
		MaxInterval:  50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(120 * time.Millisecond)
	// Startup seed = 1 call; the tick loop shouldn't add more with nobody subscribed.
	afterIdle := calls.Load()
	if afterIdle < 1 {
		t.Fatalf("no seed poll")
	}
	hub.setCount(1)
	time.Sleep(150 * time.Millisecond)
	if calls.Load() <= afterIdle {
		t.Errorf("no polls with a client connected (%d)", calls.Load())
	}
	hub.setCount(0)
}

func TestPiJoinsUsedByWhenReady(t *testing.T) {
	m := monitor(t, &fakeHub{}, probes{codex: fixture(t, "usage-codex-ratelimits.json"), usage: fixture(t, "usage-claude-stream.jsonl"),
		ready: map[string]bool{"openai-codex": true, "anthropic": true}}, Options{})
	requestRefreshAndWait(m)
	snap := m.Snapshot()
	slice(t, "codex usedBy", find(snap, "codex").UsedBy, []string{"codex", "pi"})
	slice(t, "claude usedBy", find(snap, "claude").UsedBy, []string{"claude", "pi"})
}

func TestOpenCodeGoCardAppearsOnlyWhenPiIsReady(t *testing.T) {
	notReady := monitor(t, &fakeHub{}, probes{}, Options{})
	requestRefreshAndWait(notReady)
	if find(notReady.Snapshot(), "opencode-go") != nil {
		t.Error("card without pi auth")
	}
	ready := monitor(t, &fakeHub{}, probes{ready: map[string]bool{"opencode-go": true}}, Options{})
	requestRefreshAndWait(ready)
	card := find(ready.Snapshot(), "opencode-go")
	if card == nil {
		t.Fatal("no card")
	}
	eq(t, "label", card.Label, "OpenCode Go")
	eq(t, "stale", card.Stale, false)
	eq(t, "reason", *card.UnavailableReason, "Usage not available from OpenCode")
	slice(t, "usedBy", card.UsedBy, []string{"pi"})
}

// usage.updated goes out once per provider whose data changed, not on every poll.
func TestBroadcastsOnlyChanges(t *testing.T) {
	hub := &fakeHub{}
	m := monitor(t, hub, probes{codex: fixture(t, "usage-codex-ratelimits.json")}, Options{ManualCooldown: time.Nanosecond})
	requestRefreshAndWait(m)
	first := len(hub.events)
	time.Sleep(time.Millisecond)
	requestRefreshAndWait(m)
	eq(t, "events after an unchanged poll", len(hub.events), first)
}

// Run returns promptly when its context ends, even while a probe is running, and the probe's
// context is cancelled with it (the live probes kill their child process on that).
func TestRunStopsDuringAProbe(t *testing.T) {
	probeCancelled := make(chan struct{})
	m := monitor(t, &fakeHub{}, probes{}, Options{Codex: func(ctx context.Context) []byte {
		<-ctx.Done()
		close(probeCancelled)
		return nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run didn't return")
	}
	<-probeCancelled
	if find(m.Snapshot(), "codex") != nil {
		t.Error("recorded a cancelled probe")
	}
}

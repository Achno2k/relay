package usage

import (
	"context"
	"os"
	"sync"
	"time"

	"relay/internal/api"
)

// Hub is where usage.updated goes; Count is the number of connected /ws clients.
type Hub interface {
	Broadcast(api.ServerEvent)
	Count() int
}

// Options: zero values mean the api.md defaults and the live probes.
type Options struct {
	Codex          func() []byte                // account/rateLimits/read response line
	ClaudeUsage    func(dir string) []byte      // claude -p /usage stdout
	ClaudeAuth     func() []byte                // claude auth status --json stdout
	PiAuth         func(provider string) []byte // pi auth check --provider <p> --json stdout
	ProbeDir       string                       // cwd for claude -p; "" = <relay home>/usage-probe
	Tick           time.Duration                // 15 s
	BaseInterval   time.Duration                // 60 s
	MaxInterval    time.Duration                // 10 min
	StaleAfter     time.Duration                // 15 min
	ManualCooldown time.Duration                // 15 s
	Now            func() time.Time
}

// Monitor polls the Claude and Codex/pi probes, caches the result and broadcasts usage.updated.
type Monitor struct {
	hub Hub
	o   Options

	// poll serialises whole poll passes (the Swift actor did); mu guards the cache, so GET /usage
	// never waits on a running probe.
	poll sync.Mutex
	mu   sync.Mutex

	providers    map[string]api.UsageProvider
	nextEligible map[string]time.Time
	backoff      map[string]time.Duration
	lastManual   time.Time
	bg           sync.WaitGroup
}

func NewMonitor(hub Hub) *Monitor { return NewMonitorWith(hub, Options{}) }

func NewMonitorWith(hub Hub, o Options) *Monitor {
	if o.Codex == nil {
		o.Codex = LiveCodex
	}
	if o.ClaudeUsage == nil {
		o.ClaudeUsage = LiveClaudeUsage
	}
	if o.ClaudeAuth == nil {
		o.ClaudeAuth = LiveClaudeAuth
	}
	if o.PiAuth == nil {
		o.PiAuth = LivePiAuth
	}
	if o.ProbeDir == "" {
		o.ProbeDir = defaultProbeDir()
	}
	def := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	def(&o.Tick, 15*time.Second)
	def(&o.BaseInterval, 60*time.Second)
	def(&o.MaxInterval, 10*time.Minute)
	def(&o.StaleAfter, 15*time.Minute)
	def(&o.ManualCooldown, 15*time.Second)
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Monitor{hub: hub, o: o, providers: map[string]api.UsageProvider{},
		nextEligible: map[string]time.Time{}, backoff: map[string]time.Duration{}}
}

// Run seeds the cache at once (so GET /usage never waits on a subscriber), then polls every tick
// while at least one /ws client is connected. Returns when ctx is done.
func (m *Monitor) Run(ctx context.Context) {
	m.pollAll(true)
	t := time.NewTicker(m.o.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if m.hub.Count() > 0 {
				m.pollAll(false)
			}
		}
	}
}

// Snapshot is GET /usage: the cache only, with stale re-derived against now. opencode-go is
// never marked stale (there's no fetch to go stale).
func (m *Monitor) Snapshot() []api.UsageProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.o.Now()
	out := []api.UsageProvider{}
	for _, id := range []string{"claude", "codex", "opencode-go"} {
		p, ok := m.providers[id]
		if !ok {
			continue
		}
		if id != "opencode-go" {
			p = p.MarkedStale(n, m.o.StaleAfter)
		}
		out = append(out, p)
	}
	return out
}

// RequestRefresh is POST /usage/refresh: polls now, bypassing backoff. false (429) within
// ManualCooldown of the last manual refresh.
func (m *Monitor) RequestRefresh() bool {
	m.mu.Lock()
	n := m.o.Now()
	if !m.lastManual.IsZero() && n.Sub(m.lastManual) < m.o.ManualCooldown {
		m.mu.Unlock()
		return false
	}
	m.lastManual = n
	m.mu.Unlock()
	m.bg.Add(1)
	go func() {
		defer m.bg.Done()
		m.pollAll(true)
	}()
	return true
}

func (m *Monitor) piReady(provider string) bool { return PiReady(m.o.PiAuth(provider)) }

func (m *Monitor) pollAll(force bool) {
	m.poll.Lock()
	defer m.poll.Unlock()
	piCodex := m.piReady("openai-codex")
	piClaude := m.piReady("anthropic")
	piOpenCodeGo := m.piReady("opencode-go")
	m.pollCodex(force, piCodex)
	m.pollClaude(force, piClaude)
	m.pollOpenCodeGo(piOpenCodeGo)
}

func (m *Monitor) pollCodex(force, piReady bool) {
	if !force && !m.eligible("codex") {
		return
	}
	n := m.o.Now()
	raw := m.o.Codex()
	var p *api.UsageProvider
	if raw != nil {
		p = ParseCodex(raw, n, piReady)
	}
	if p == nil {
		m.recordFailure("codex", "codex app-server didn't respond", n)
		return
	}
	m.record(*p, n)
}

func (m *Monitor) pollClaude(force, piReady bool) {
	if !force && !m.eligible("claude") {
		return
	}
	n := m.o.Now()
	_ = os.MkdirAll(m.o.ProbeDir, 0o755)
	usage, auth := m.o.ClaudeUsage(m.o.ProbeDir), m.o.ClaudeAuth()
	p := ParseClaude(usage, auth, n, piReady)
	if p == nil {
		m.recordFailure("claude", "claude -p /usage didn't return usage data", n)
		return
	}
	m.record(*p, n)
}

// pollOpenCodeGo has no backoff: it's only `pi auth check`. When pi loses opencode-go auth the
// card is dropped from the cache ("omitted, not shown empty").
func (m *Monitor) pollOpenCodeGo(piReady bool) {
	n := m.o.Now()
	p := OpenCodeGoProvider(piReady, n)
	if p == nil {
		m.mu.Lock()
		delete(m.providers, "opencode-go")
		m.mu.Unlock()
		return
	}
	m.record(*p, n)
}

func (m *Monitor) eligible(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	next, ok := m.nextEligible[id]
	return !ok || !m.o.Now().Before(next)
}

func (m *Monitor) interval(id string) time.Duration {
	if b, ok := m.backoff[id]; ok {
		return b
	}
	return m.o.BaseInterval
}

func (m *Monitor) record(p api.UsageProvider, n time.Time) {
	m.mu.Lock()
	old, had := m.providers[p.ID]
	changed := !had || !old.SameDataExcludingFreshness(p)
	iv := m.o.BaseInterval
	if !changed {
		iv = min(m.o.MaxInterval, m.interval(p.ID)*2)
	}
	m.backoff[p.ID] = iv
	m.nextEligible[p.ID] = n.Add(iv)
	m.providers[p.ID] = p
	m.mu.Unlock()
	if changed {
		m.hub.Broadcast(api.UsageUpdated(p))
	}
}

func (m *Monitor) recordFailure(id, reason string, n time.Time) {
	m.mu.Lock()
	iv := min(m.o.MaxInterval, m.interval(id)*2)
	m.backoff[id] = iv
	m.nextEligible[id] = n.Add(iv)
	label, source := "ChatGPT", "codex app-server"
	if id == "claude" {
		label, source = "Claude", "claude -p /usage"
	}
	// Keep the last known windows/usedBy (dimmed via stale) rather than blanking them on one failure.
	prev, had := m.providers[id]
	p := api.UsageProvider{ID: id, Label: label, Windows: []api.UsageWindow{}, UpdatedAt: api.FormatTime(n),
		Source: source, Stale: true, UnavailableReason: api.Str(reason), UsedBy: []string{id}}
	if had {
		p.Plan, p.Windows, p.UpdatedAt, p.UsedBy = prev.Plan, prev.Windows, prev.UpdatedAt, prev.UsedBy
	}
	changed := !had || prev.UnavailableReason == nil || *prev.UnavailableReason != reason
	m.providers[id] = p
	m.mu.Unlock()
	if changed {
		m.hub.Broadcast(api.UsageUpdated(p))
	}
}

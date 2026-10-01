package live

import (
	"context"
	"sync"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

// Hub is where frames go. Count is the number of connected /ws clients.
type Hub interface {
	Broadcast(api.ServerEvent)
	Count() int
}

// Agent is what the monitor needs of a live agent, fed from the agent monitor's snapshot.
// Cwd is the pane's cwd (else its foreground cwd), "" when unknown.
type Agent struct {
	ID     string
	Kind   string
	Status api.AgentStatus
	Cwd    string
}

// parsedKinds are the kinds Parse understands (Swift: TranscriptState.parsedKinds).
var parsedKinds = map[string]bool{"claude": true, "pi": true, "codex": true}

// Monitor streams `reply.live` previews (prose and running tool) for every working agent, while
// at least one /ws client is connected. The agent monitor feeds it the agent list it already
// refreshes and the messages its transcript tailers already parse, so this adds exactly one
// `agent.read` per working agent per tick and no other herdr calls.
type Monitor struct {
	herdr    herdr.Client
	hub      Hub
	tracker  *Tracker
	interval time.Duration

	mu     sync.Mutex
	agents map[string]Agent
}

// NewMonitor polls every 250 ms with a tracker throttled to 4 frames per second per agent.
func NewMonitor(h herdr.Client, hub Hub) *Monitor {
	return NewMonitorWith(h, hub, NewTracker(250*time.Millisecond, nil), 250*time.Millisecond)
}

func NewMonitorWith(h herdr.Client, hub Hub, tracker *Tracker, interval time.Duration) *Monitor {
	return &Monitor{herdr: h, hub: hub, tracker: tracker, interval: interval, agents: map[string]Agent{}}
}

// Update is fed by the agent monitor on every refresh with the current live agents.
func (m *Monitor) Update(agents []Agent) {
	next := make(map[string]Agent, len(agents))
	for _, a := range agents {
		next[a.ID] = a
	}
	var events []api.ServerEvent
	m.mu.Lock()
	for id, was := range m.agents {
		if now, ok := next[id]; was.Status == api.StatusWorking && (!ok || now.Status != api.StatusWorking) {
			if ev := m.tracker.Stopped(id); ev != nil {
				events = append(events, *ev)
			}
		}
	}
	for id := range m.agents {
		if _, ok := next[id]; !ok {
			m.tracker.Remove(id)
		}
	}
	m.agents = next
	m.mu.Unlock()
	for _, ev := range events {
		m.hub.Broadcast(ev)
	}
}

// Landed is fed by the agent monitor whenever a transcript message grows, so the preview never
// repeats it.
func (m *Monitor) Landed(agentID string, msg api.Message) {
	if msg.Role != api.RoleAssistant {
		return
	}
	if ev := m.tracker.LandedBlocks(agentID, msg.Blocks); ev != nil {
		m.hub.Broadcast(*ev)
	}
}

// Run polls until ctx is done.
func (m *Monitor) Run(ctx context.Context) {
	for {
		m.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.interval):
		}
	}
}

func (m *Monitor) tick(ctx context.Context) {
	if m.hub.Count() == 0 {
		return
	}
	var working []Agent
	m.mu.Lock()
	for _, a := range m.agents {
		if a.Status == api.StatusWorking && parsedKinds[a.Kind] {
			working = append(working, a)
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, a := range working {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.poll(ctx, a)
		}()
	}
	wg.Wait()
}

// poll reads the visible screen, not recent_unwrapped: herdr refuses to scroll an
// alternate-screen pane's history while it's actively being written to (`agent_not_idle`),
// which a working agent always is. The viewport is all a live poll can read, and it's enough
// for a tail preview. It reads with ANSI so ScreenText can tell Claude's update banner from
// the reply text it lands on.
func (m *Monitor) poll(ctx context.Context, a Agent) {
	read, err := m.herdr.Read(ctx, a.ID, herdr.SourceVisible, 0, true)
	if err != nil {
		return
	}
	parsed := Parse(ScreenText(read.Text), a.Kind)
	scrubber := transcript.NewScrubber(a.Cwd)
	var text *string
	if parsed.Text != nil {
		text = strPtr(scrubber.Scrub(*parsed.Text))
	}
	var tool *api.LiveTool
	generic := false
	if parsed.Tool != nil {
		t := api.NewLiveTool(parsed.Tool.Name, parsed.Tool.Summary(scrubber))
		tool = &t
		generic = parsed.Tool.Generic
	}

	// A read that was in flight when the agent stopped must not reopen the preview: the stop
	// already cleared it and nothing would clear it again. Update runs under the same lock.
	m.mu.Lock()
	cur, ok := m.agents[a.ID]
	var ev *api.ServerEvent
	if ok && cur.Status == api.StatusWorking {
		ev = m.tracker.Offer(a.ID, text, tool, generic)
	}
	m.mu.Unlock()
	if ev != nil {
		m.hub.Broadcast(*ev)
	}
}

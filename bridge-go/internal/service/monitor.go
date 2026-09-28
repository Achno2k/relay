package service

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

// Hub is where the monitor sends WS events. *server.Hub implements it.
type Hub interface {
	Broadcast(api.ServerEvent)
	Count() int
}

// Monitor turns herdr events into WebSocket deltas and keeps one transcript tailer per live agent.
//
// Every event (and a slow timer, as a safety net) triggers a fresh agent.list; the event payload
// is never trusted for status.
type Monitor struct {
	service  *Service
	hub      Hub
	stream   herdr.EventSource
	interval time.Duration
	// Live-reply hooks: fed the agent list on every refresh and every transcript message as it
	// lands, so the live monitor needs no herdr calls of its own beyond agent.read.
	onSnapshots func([]Snapshot)
	onMessage   func(agentID string, m api.Message)

	reachable atomic.Bool

	// Trigger coalescing: one refresh at a time; events during it cause exactly one more.
	trigMu     sync.Mutex
	refreshing bool
	pending    bool

	mu          sync.Mutex // guards known, tailers, initialized
	known       map[string]api.Agent
	tailers     map[string]*transcript.Tailer
	initialized bool
}

type MonitorOptions struct {
	Interval    time.Duration // 0: 5 s
	OnSnapshots func([]Snapshot)
	OnMessage   func(agentID string, m api.Message)
}

func NewMonitor(service *Service, hub Hub, stream herdr.EventSource, opt MonitorOptions) *Monitor {
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Second
	}
	return &Monitor{
		service: service, hub: hub, stream: stream, interval: opt.Interval,
		onSnapshots: opt.OnSnapshots, onMessage: opt.OnMessage,
		known: map[string]api.Agent{}, tailers: map[string]*transcript.Tailer{},
	}
}

// HerdrReachable reports whether the last agent.list/workspace.list round trip succeeded (/health).
func (m *Monitor) HerdrReachable() bool { return m.reachable.Load() }

// Run refreshes on every herdr event and every interval until ctx is done, then stops the tailers.
func (m *Monitor) Run(ctx context.Context) {
	m.stream.Start()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-m.stream.Events():
				if !ok {
					return
				}
				m.Trigger(ctx)
			}
		}
	}()
	go func() {
		defer wg.Done()
		t := time.NewTicker(m.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Trigger(ctx)
			}
		}
	}()
	m.Trigger(ctx)
	<-ctx.Done()
	m.stream.Stop()
	wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.tailers {
		t.Stop()
		delete(m.tailers, id)
	}
}

// Trigger coalesces bursts of events into back-to-back refreshes. It returns at once when a
// refresh is already running; that refresh then runs once more.
func (m *Monitor) Trigger(ctx context.Context) {
	m.trigMu.Lock()
	if m.refreshing {
		m.pending = true
		m.trigMu.Unlock()
		return
	}
	m.refreshing = true
	for {
		m.pending = false
		m.trigMu.Unlock()
		m.refresh(ctx)
		m.trigMu.Lock()
		if !m.pending || ctx.Err() != nil {
			break
		}
	}
	m.refreshing = false
	m.trigMu.Unlock()
}

func (m *Monitor) refresh(ctx context.Context) {
	snaps, err := m.service.Snapshots(ctx)
	if err != nil {
		m.reachable.Store(false)
		return
	}
	m.reachable.Store(true)
	if m.onSnapshots != nil {
		m.onSnapshots(snaps)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]api.Agent, len(snaps))
	for _, s := range snaps {
		next[s.Agent.ID] = s.Agent
		if m.initialized {
			if old, ok := m.known[s.Agent.ID]; ok {
				if !sameExceptTime(old, s.Agent) {
					m.hub.Broadcast(api.AgentUpdated(s.Agent))
				}
			} else {
				m.hub.Broadcast(api.AgentCreated(s.Agent))
			}
		}
		m.syncTailer(s)
	}
	for id := range m.known {
		if _, ok := next[id]; !ok {
			m.hub.Broadcast(api.AgentClosed(id))
			m.service.forget(id)
		}
	}
	for id, t := range m.tailers {
		if _, ok := next[id]; !ok {
			t.Stop()
			delete(m.tailers, id)
		}
	}
	m.known = next
	m.initialized = true
	panes := make([]string, 0, len(next))
	for id := range next {
		panes = append(panes, id)
	}
	slices.Sort(panes)
	m.stream.Watch(panes)
}

// syncTailer keeps one tailer per agent on its current transcript; m.mu is held.
func (m *Monitor) syncTailer(s Snapshot) {
	id := s.Agent.ID
	old := m.tailers[id]
	if s.Transcript == nil {
		if old != nil {
			old.Stop()
			delete(m.tailers, id)
		}
		return
	}
	if old != nil && old.Path() == s.Transcript.Path && !old.Dead() {
		return
	}
	if old != nil {
		old.Stop()
	}
	hub, onMessage := m.hub, m.onMessage
	t := transcript.NewTailer(*s.Transcript, m.service.uploads, func(msg api.Message) {
		hub.Broadcast(api.MessageUpserted(id, msg))
		if onMessage != nil {
			onMessage(id, msg)
		}
	})
	t.Start()
	m.tailers[id] = t
}

// TailerCount is the number of live transcript tailers (tests).
func (m *Monitor) TailerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tailers)
}

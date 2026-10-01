package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

// Event is a herdr event reduced to what the bridge needs: which pane something happened to.
// The payload is only a hint to refresh; status is always re-read with agent.list/agent.get.
// Kind "resync" means anything may have changed (after a (re)connect).
type Event struct {
	Kind   string
	PaneID string
}

// ParseEvent parses one `{"event": ..., "data": {...}}` line. ok is false for responses and junk.
func ParseEvent(line []byte) (Event, bool) {
	var o struct {
		Event *string        `json:"event"`
		Data  map[string]any `json:"data"`
	}
	if json.Unmarshal(line, &o) != nil || o.Event == nil {
		return Event{}, false
	}
	pane, _ := o.Data["pane_id"].(string)
	if pane == "" {
		if p, ok := o.Data["pane"].(map[string]any); ok {
			pane, _ = p["pane_id"].(string)
		}
	}
	return Event{Kind: strings.ReplaceAll(*o.Event, "_", "."), PaneID: pane}, true
}

// EventSource is what the service consumes; *EventStream implements it.
type EventSource interface {
	Start()
	Stop()
	Events() <-chan Event
	// Watch sets the panes to watch for status changes; resubscribes if the set changed.
	Watch(panes []string)
}

var paneEvents = []string{"pane.created", "pane.closed", "pane.updated", "pane.moved", "pane.exited", "pane.agent_detected"}

// EventStream is a long-lived events.subscribe connection with reconnect. herdr needs one
// pane.agent_status_changed subscription per pane, so when the set of agent panes changes the
// stream reconnects with a new subscription list.
type EventStream struct {
	socketPath string
	events     chan Event

	mu      sync.Mutex
	panes   map[string]bool
	conn    net.Conn
	stopped bool
	started bool
	wake    chan struct{} // closed by Stop to cut a backoff sleep short
}

var _ EventSource = (*EventStream)(nil)

func NewEventStream(socketPath string) *EventStream {
	return &EventStream{socketPath: socketPath, events: make(chan Event, 256), panes: map[string]bool{}, wake: make(chan struct{})}
}

// Events is closed after Stop.
func (s *EventStream) Events() <-chan Event { return s.events }

func (s *EventStream) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	go func() {
		s.loop()
		close(s.events)
	}()
}

func (s *EventStream) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	conn, started := s.conn, s.started
	close(s.wake)
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	if !started {
		close(s.events)
	}
}

func (s *EventStream) Watch(panes []string) {
	next := map[string]bool{}
	for _, p := range panes {
		next[p] = true
	}
	s.mu.Lock()
	if maps.Equal(s.panes, next) {
		s.mu.Unlock()
		return
	}
	s.panes = next
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// yield drops the oldest event when the buffer is full (Swift: bufferingNewest(256)).
func (s *EventStream) yield(e Event) {
	for {
		select {
		case s.events <- e:
			return
		default:
		}
		select {
		case <-s.events:
		default:
		}
	}
}

func (s *EventStream) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *EventStream) loop() {
	backoff := 500 * time.Millisecond
	for !s.isStopped() {
		if err := s.session(&backoff); err != nil {
			s.mu.Lock()
			s.conn = nil
			s.mu.Unlock()
			if s.isStopped() {
				return
			}
			select {
			case <-time.After(backoff):
			case <-s.wake:
				return
			}
			backoff = min(backoff*2, 5*time.Second)
		}
	}
}

// session runs one subscription until the connection drops. It always returns an error.
func (s *EventStream) session(backoff *time.Duration) error {
	s.mu.Lock()
	panes := slices.Sorted(maps.Keys(s.panes))
	s.mu.Unlock()
	subs := make([]map[string]any, 0, len(paneEvents)+len(panes))
	for _, t := range paneEvents {
		subs = append(subs, map[string]any{"type": t})
	}
	for _, p := range panes {
		subs = append(subs, map[string]any{"type": "pane.agent_status_changed", "pane_id": p})
	}
	conn, err := Dial(context.Background(), s.socketPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		conn.Close()
		return IOError("stopped")
	}
	s.conn = conn
	s.mu.Unlock()
	defer conn.Close()

	req, _ := json.Marshal(map[string]any{"id": "relay-events", "method": "events.subscribe", "params": map[string]any{"subscriptions": subs}})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return IOError("write: " + errText(err))
	}
	r := bufio.NewReaderSize(conn, 64*1024)
	first, err := readLine(r)
	if err != nil {
		return IOError("read: " + errText(err))
	}
	var resp struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(first, &resp) == nil && resp.Error != nil && string(resp.Error) != "null" {
		// Usually a pane closed between listing and subscribing: ask for a resync and retry.
		s.yield(Event{Kind: "resync"})
		return IOError("subscribe rejected")
	}
	*backoff = 500 * time.Millisecond
	// Anything could have changed while we were disconnected.
	s.yield(Event{Kind: "resync"})
	for {
		line, err := readLine(r)
		if err != nil {
			return IOError("read: " + errText(err))
		}
		if e, ok := ParseEvent(line); ok {
			s.yield(e)
		}
	}
}

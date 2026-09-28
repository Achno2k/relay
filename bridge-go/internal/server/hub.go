package server

import (
	"sync"

	"relay/internal/api"
)

// hubBuffer is how many events a slow client may fall behind before the oldest are dropped.
const hubBuffer = 1000

// Hub fans server events out to every connected WebSocket client. Deltas only; nothing is replayed.
type Hub struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]chan api.ServerEvent
}

func NewHub() *Hub {
	return &Hub{subs: map[uint64]chan api.ServerEvent{}}
}

// Subscribe returns an id for Unsubscribe and the client's event channel, closed on Unsubscribe.
func (h *Hub) Subscribe() (uint64, <-chan api.ServerEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	ch := make(chan api.ServerEvent, hubBuffer)
	h.subs[h.next] = ch
	return h.next, ch
}

func (h *Hub) Unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
}

// Broadcast never blocks: a full client buffer drops its oldest event (Swift's bufferingNewest).
func (h *Hub) Broadcast(e api.ServerEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- e:
			continue
		default:
		}
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- e:
		default:
		}
	}
}

// Count is the number of connected /ws clients.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

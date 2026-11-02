package event

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	ID   uint64          `json:"id"`
	Type string          `json:"type"`
	Time time.Time       `json:"time"`
	Data json.RawMessage `json:"data"`
}

const maxHistory = 256

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	history     []Event
	nextID      atomic.Uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	return h.subscribe(0, false)
}

// SubscribeSince registers a subscriber and replays the retained events after
// lastID. EventSource sends Last-Event-ID automatically on reconnect; replay is
// best-effort and bounded by the channel buffer, after which clients refetch.
func (h *Hub) SubscribeSince(lastID uint64) (<-chan Event, func()) {
	return h.subscribe(lastID, true)
}

func (h *Hub) subscribe(lastID uint64, replay bool) (<-chan Event, func()) {
	channel := make(chan Event, 32)
	h.mu.Lock()
	if replay {
		for _, event := range h.history {
			if event.ID > lastID {
				select {
				case channel <- event:
				default:
				}
			}
		}
	}
	h.subscribers[channel] = struct{}{}
	h.mu.Unlock()
	return channel, func() {
		h.mu.Lock()
		if _, exists := h.subscribers[channel]; exists {
			delete(h.subscribers, channel)
			close(channel)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(eventType string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	event := Event{
		ID:   h.nextID.Add(1),
		Type: eventType,
		Time: time.Now().UTC(),
		Data: data,
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, event)
	if len(h.history) > maxHistory {
		h.history = append([]Event(nil), h.history[len(h.history)-maxHistory:]...)
	}
	for subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			// Slow clients receive the next state snapshot after reconnecting.
		}
	}
}

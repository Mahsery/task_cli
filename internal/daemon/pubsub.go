package daemon

import (
	"log"
	"sync"

	"task_cli/internal/protocol"
)

type PubSub struct {
	mu          sync.RWMutex
	subscribers map[chan protocol.Event]struct{}
}

func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[chan protocol.Event]struct{}),
	}
}

func (ps *PubSub) Subscribe() chan protocol.Event {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan protocol.Event, 64)
	ps.subscribers[ch] = struct{}{}
	return ch
}

func (ps *PubSub) Unsubscribe(ch chan protocol.Event) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.subscribers[ch]; exists {
		delete(ps.subscribers, ch)
		close(ch)
	}
}

func (ps *PubSub) Publish(event protocol.Event) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for ch := range ps.subscribers {
		select {
		case ch <- event:
		default:
			log.Printf("[tkd/pubsub] Warning: subscriber channel full, dropped event %s", event.Type)
		}
	}
}

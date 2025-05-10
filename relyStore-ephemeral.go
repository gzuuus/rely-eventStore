package main

import (
	"context"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

// An in-memory, thread-safe ring-buffer for storing Nostr events with a fixed memory footprint.
type Ephemeral struct {
	mu       sync.RWMutex
	buffer   []*nostr.Event
	capacity int
	len      int
	write    int
}

func NewEphemeral(capacity int) *Ephemeral {
	return &Ephemeral{
		buffer:   make([]*nostr.Event, capacity),
		capacity: capacity,
	}
}

func (e *Ephemeral) Save(ctx context.Context, event *nostr.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.buffer[e.write] = event
	e.write = (e.write + 1) % e.capacity
	e.len = min(e.len+1, e.capacity)
	return nil
}

func (e *Ephemeral) Query(ctx context.Context, filter *nostr.Filter) ([]nostr.Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	limit := e.len
	if filter.Limit > 0 {
		limit = min(filter.Limit, e.len)
	}

	result := make([]nostr.Event, 0, limit)
	for i := range e.len {
		if len(result) >= limit {
			break
		}

		if filter.Matches(e.buffer[i]) {
			result = append(result, *e.buffer[i])
		}
	}

	return result, nil
}

func (e *Ephemeral) Len() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.len
}
package main

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/nbd-wtf/go-nostr"
)

// AtomicCircularBuffer2 is an optimized, lock-free, fixed-size circular buffer for storing Nostr events.
type AtomicCircularBuffer2 struct {
	buffer []*atomic.Pointer[nostr.Event]
	head   atomic.Uint64 // position to write next event
	size   uint64        // fixed size of the buffer
	count  atomic.Uint64 // number of events in buffer
}

// NewAtomicCircularBuffer2 creates a new AtomicCircularBuffer2 with the specified capacity.
func NewAtomicCircularBuffer2(capacity int) *AtomicCircularBuffer2 {
	if capacity <= 0 {
		panic("capacity must be greater than 0")
	}

	buffer := make([]*atomic.Pointer[nostr.Event], capacity)
	for i := range buffer {
		buffer[i] = &atomic.Pointer[nostr.Event]{}
	}

	return &AtomicCircularBuffer2{
		buffer: buffer,
		size:   uint64(capacity),
	}
}

// SaveEvent adds a new event to the circular buffer.
// If the buffer is full, it automatically overwrites the oldest event.
func (cb *AtomicCircularBuffer2) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return errors.New("event cannot be nil")
	}

	head := cb.head.Load()
	cb.buffer[head].Store(evt)
	cb.head.Store((head + 1) % cb.size)

	count := cb.count.Add(1)
	if count > cb.size {
		cb.count.Store(cb.size)
	}

	return nil
}

// QueryEvents returns a slice of events matching the filter.
// This is more efficient than channel-based implementation as it avoids
// goroutine creation and channel operations.
func (cb *AtomicCircularBuffer2) QueryEvents(ctx context.Context, filter nostr.Filter) ([]*nostr.Event, error) {
	count := cb.count.Load()
	head := cb.head.Load()

	if count == 0 {
		return nil, nil
	}

	limit := int(count)
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	result := make([]*nostr.Event, 0, limit)

	tail := uint64(0)
	if count >= cb.size {
		tail = (head + 1) % cb.size
	}

	for i := uint64(0); i < count; i++ {
		idx := (tail + i) % cb.size
		evt := cb.buffer[idx].Load()
		if evt != nil && cb.eventMatchesFilter(evt, filter) {
			result = append(result, evt)
			if len(result) >= limit {
				break
			}
		}
	}

	return result, nil
}

// eventMatchesFilter checks if an event matches the given filter.
// Implements the Nostr filter matching logic for IDs, authors, kinds, tags, and timestamps.
func (cb *AtomicCircularBuffer2) eventMatchesFilter(evt *nostr.Event, filter nostr.Filter) bool {
	if filter.Since != nil && evt.CreatedAt < *filter.Since {
		return false
	}
	if filter.Until != nil && evt.CreatedAt > *filter.Until {
		return false
	}

	if len(filter.Kinds) > 0 {
		hasMatchingKind := false
		for _, k := range filter.Kinds {
			if k == evt.Kind {
				hasMatchingKind = true
				break
			}
		}
		if !hasMatchingKind {
			return false
		}
	}

	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if id == evt.ID {
				found = true
				break
			}
			if len(id) < 64 && len(evt.ID) >= len(id) && evt.ID[:len(id)] == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Authors) > 0 {
		found := false
		for _, author := range filter.Authors {
			if author == evt.PubKey {
				found = true
				break
			}
			if len(author) < 64 && len(evt.PubKey) >= len(author) && evt.PubKey[:len(author)] == author {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for tagName, values := range filter.Tags {
		if len(values) == 0 {
			continue
		}

		found := false
		tagLoop:
		for _, tag := range evt.Tags {
			if len(tag) > 1 && tag[0] == tagName {
				for _, v := range values {
					if v == tag[1] {
						found = true
						break tagLoop
					}
				}
			}
		}
		if !found {
			return false
		}
	}

	return true
}

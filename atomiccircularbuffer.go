package main

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"

	"github.com/nbd-wtf/go-nostr"
)

// AtomicCircularBuffer is a lock-free, fixed-size circular buffer for storing Nostr events.
// It efficiently manages ephemeral events with a fixed memory footprint and automatic
// oldest-event replacement when full using atomic operations for thread safety.
type AtomicCircularBuffer struct {
	buffer []nostr.Event
	head   uint64 // atomic
	tail   uint64 // atomic
	size   uint64
	count  uint64 // atomic
}

// NewAtomicCircularBuffer creates a new AtomicCircularBuffer with the specified capacity.
func NewAtomicCircularBuffer(capacity int) *AtomicCircularBuffer {
	return &AtomicCircularBuffer{
		buffer: make([]nostr.Event, capacity),
		size:   uint64(capacity),
	}
}

// SaveEvent adds a new event to the circular buffer.
// If the buffer is full, it automatically overwrites the oldest event.
func (cb *AtomicCircularBuffer) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return errors.New("event cannot be nil")
	}

	// Atomically get and increment the head
	head := atomic.LoadUint64(&cb.head)
	newHead := (head + 1) % cb.size
	atomic.StoreUint64(&cb.head, newHead)

	// Store the event at the current head position
	cb.buffer[head] = *evt

	// Update count and tail atomically
	count := atomic.AddUint64(&cb.count, 1)
	if count > cb.size {
		// Buffer is full, advance tail to overwrite oldest
		atomic.CompareAndSwapUint64(&cb.count, count, cb.size)
		atomic.StoreUint64(&cb.tail, (atomic.LoadUint64(&cb.tail)+1)%cb.size)
	}

	return nil
}

// QueryEvents returns a channel that will receive all events matching the filter.
// Events are sent asynchronously to avoid blocking.
func (cb *AtomicCircularBuffer) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event)

	go func() {
		defer close(ch)

		// Get a snapshot of the current state
		tail := atomic.LoadUint64(&cb.tail)
		count := atomic.LoadUint64(&cb.count)

		// Apply limit from filter or use all events if no limit
		limit := int(count)
		if filter.Limit > 0 && filter.Limit < limit {
			limit = filter.Limit
		}

		// Pre-allocate the result slice
		result := make([]nostr.Event, 0, limit)

		// Start from the tail (oldest) and move towards head (newest)
		for i, idx := uint64(0), tail; i < count; i++ {
			evt := cb.buffer[idx]
			if cb.eventMatchesFilter(&evt, filter) {
				result = append(result, evt)
				if len(result) >= limit {
					break
				}
			}
			idx = (idx + 1) % cb.size
		}

		// Send matching events to the channel
		for i := range result {
			select {
			case <-ctx.Done():
				return
			case ch <- &result[i]:
			}
		}
	}()

	return ch, nil
}

// eventMatchesFilter checks if an event matches the given filter.
// Implements the Nostr filter matching logic for IDs, authors, kinds, tags, and timestamps.
func (cb *AtomicCircularBuffer) eventMatchesFilter(evt *nostr.Event, filter nostr.Filter) bool {
	// Check IDs
	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if id == evt.ID || (len(id) < 64 && len(evt.ID) >= len(id) && evt.ID[:len(id)] == id) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check Authors
	if len(filter.Authors) > 0 {
		found := false
		for _, author := range filter.Authors {
			if author == evt.PubKey || (len(author) < 64 && len(evt.PubKey) >= len(author) && evt.PubKey[:len(author)] == author) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check Kinds
	if len(filter.Kinds) > 0 {
		hasMatchingKind := slices.Contains(filter.Kinds, evt.Kind)
		if !hasMatchingKind {
			return false
		}
	}

	// Check Tags
	for tagName, values := range filter.Tags {
		if len(values) == 0 {
			continue
		}

		found := false
		for _, tag := range evt.Tags {
			if len(tag) > 1 && tag[0] == tagName {
				if slices.Contains(values, tag[1]) {
					found = true
				}
				if found {
					break
				}
			}
		}
		if !found {
			return false
		}
	}

	// Check Since
	if filter.Since != nil && evt.CreatedAt < *filter.Since {
		return false
	}

	// Check Until
	if filter.Until != nil && evt.CreatedAt > *filter.Until {
		return false
	}

	return true
}

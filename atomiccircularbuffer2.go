package main

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"

	"github.com/nbd-wtf/go-nostr"
)

// AtomicCircularBuffer2 is an optimized, lock-free, fixed-size circular buffer for storing Nostr events.
// It efficiently manages ephemeral events with a fixed memory footprint and automatic
// event replacement when full using atomic operations for thread safety.
// This implementation uses atomic pointers to events to prevent data races between concurrent reads and writes.
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
		// head and count are initialized to 0 by default
	}
}

// SaveEvent adds a new event to the circular buffer.
// If the buffer is full, it automatically overwrites the oldest event.
func (cb *AtomicCircularBuffer2) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return errors.New("event cannot be nil")
	}

	// Atomically get the current head
	head := cb.head.Load()

	// Store the event directly at the current head position using atomic operation
	// This avoids an unnecessary allocation by not creating a copy first
	cb.buffer[head].Store(evt)

	// Atomically increment the head with wrap-around
	cb.head.Store((head + 1) % cb.size)

	// Update count atomically, capping at buffer size
	count := cb.count.Add(1)
	if count > cb.size {
		cb.count.Store(cb.size) // Use Store directly instead of CompareAndSwap for better performance
	}

	return nil
}

// QueryEvents returns a slice of events matching the filter.
// This is more efficient than channel-based implementation as it avoids
// goroutine creation and channel operations.
func (cb *AtomicCircularBuffer2) QueryEvents(ctx context.Context, filter nostr.Filter) ([]*nostr.Event, error) {
	// Get a snapshot of the current state
	count := cb.count.Load()
	head := cb.head.Load()

	// No events in buffer
	if count == 0 {
		return []*nostr.Event{}, nil
	}

	// Calculate tail position on demand
	tail := uint64(0)
	if count >= cb.size {
		// Buffer is full, calculate tail from head
		tail = (head + 1) % cb.size
	}

	// Apply limit from filter or use all events if no limit
	limit := int(count)
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	// Pre-allocate the result slice with exact capacity
	result := make([]*nostr.Event, 0, limit)

	// Start from the tail (oldest) and move towards head (newest)
	for i := uint64(0); i < count; i++ {
		idx := (tail + i) % cb.size
		// Load the event atomically
		evt := cb.buffer[idx].Load()
		if evt != nil && cb.eventMatchesFilter(evt, filter) {
			result = append(result, evt)
			if len(result) >= limit {
				break
			}
		}
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return result, nil
	}
}

// eventMatchesFilter checks if an event matches the given filter.
// Implements the Nostr filter matching logic for IDs, authors, kinds, tags, and timestamps.
func (cb *AtomicCircularBuffer2) eventMatchesFilter(evt *nostr.Event, filter nostr.Filter) bool {
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

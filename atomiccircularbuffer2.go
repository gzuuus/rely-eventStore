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
// oldest-event replacement when full using atomic operations for thread safety.
// This implementation eliminates the explicit tail pointer and uses slice-based query results.
type AtomicCircularBuffer2 struct {
	buffer []nostr.Event
	head   uint64 // atomic - position to write next event
	count  uint64 // atomic - number of events in buffer
	size   uint64 // fixed size of the buffer

	// MaxLimit is the maximum number of events to store in the buffer
	MaxLimit int
}

// NewAtomicCircularBuffer2 creates a new AtomicCircularBuffer2 with the specified capacity.
func NewAtomicCircularBuffer2(capacity int) *AtomicCircularBuffer2 {
	return &AtomicCircularBuffer2{
		buffer:   make([]nostr.Event, capacity),
		size:     uint64(capacity),
		MaxLimit: capacity,
	}
}

// Init initializes the circular buffer.
func (cb *AtomicCircularBuffer2) Init() error {
	if cb.MaxLimit <= 0 {
		return errors.New("max limit must be greater than 0")
	}

	// If buffer is already initialized with correct size, just reset it
	if cb.buffer != nil && len(cb.buffer) == cb.MaxLimit {
		atomic.StoreUint64(&cb.head, 0)
		atomic.StoreUint64(&cb.count, 0)
		return nil
	}

	cb.buffer = make([]nostr.Event, cb.MaxLimit)
	cb.size = uint64(cb.MaxLimit)
	atomic.StoreUint64(&cb.head, 0)
	atomic.StoreUint64(&cb.count, 0)
	return nil
}

// SaveEvent adds a new event to the circular buffer.
// If the buffer is full, it automatically overwrites the oldest event.
func (cb *AtomicCircularBuffer2) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return errors.New("event cannot be nil")
	}

	// Atomically get the current head
	head := atomic.LoadUint64(&cb.head)
	
	// Store the event at the current head position
	cb.buffer[head] = *evt
	
	// Atomically increment the head with wrap-around
	atomic.StoreUint64(&cb.head, (head+1)%cb.size)
	
	// Update count atomically, capping at buffer size
	count := atomic.AddUint64(&cb.count, 1)
	if count > cb.size {
		atomic.CompareAndSwapUint64(&cb.count, count, cb.size)
	}

	return nil
}

// QueryEvents returns a slice of events matching the filter.
// This is more efficient than channel-based implementation as it avoids
// goroutine creation and channel operations.
func (cb *AtomicCircularBuffer2) QueryEvents(ctx context.Context, filter nostr.Filter) ([]*nostr.Event, error) {
	// Get a snapshot of the current state
	head := atomic.LoadUint64(&cb.head)
	count := atomic.LoadUint64(&cb.count)
	
	// No events in buffer
	if count == 0 {
		return []*nostr.Event{}, nil
	}

	// Apply limit from filter or use all events if no limit
	limit := int(count)
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	// Pre-allocate the result slice
	result := make([]*nostr.Event, 0, limit)

	// Calculate tail position (oldest event)
	// When buffer is not full, tail is at position 0
	// When buffer is full, tail is at position head (oldest event gets overwritten)
	tail := uint64(0)
	if count >= cb.size {
		// Buffer is full, tail is at the same position as head
		tail = head
	}

	// Iterate through the buffer starting from the oldest event
	for i := uint64(0); i < count; i++ {
		// Calculate position: start from tail and move forward
		pos := (tail + i) % cb.size
		evt := &cb.buffer[pos]
		if cb.eventMatchesFilter(evt, filter) {
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

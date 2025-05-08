package main

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

// CircularBuffer is a thread-safe, fixed-size circular buffer for storing Nostr events.
// It efficiently manages ephemeral events with a fixed memory footprint and automatic
// oldest-event replacement when full.
type CircularBuffer struct {
	sync.Mutex

	buffer []nostr.Event
	head   int
	tail   int
	size   int
	count  int
}

// NewCircularBuffer creates a new CircularBuffer with the specified capacity.
func NewCircularBuffer(capacity int) *CircularBuffer {
	return &CircularBuffer{
		buffer: make([]nostr.Event, capacity),
		size:   capacity,
	}
}

// SaveEvent adds a new event to the circular buffer.
// If the buffer is full, it automatically overwrites the oldest event.
func (cb *CircularBuffer) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return errors.New("event cannot be nil")
	}

	cb.Lock()
	defer cb.Unlock()

	// Store a copy of the event
	cb.buffer[cb.head] = *evt
	cb.head = (cb.head + 1) % cb.size

	if cb.count == cb.size {
		// Buffer full: advance tail to overwrite oldest
		cb.tail = (cb.tail + 1) % cb.size
	} else {
		cb.count++
	}

	return nil
}

// QueryEvents returns a channel that will receive all events matching the filter.
// Events are sent asynchronously to avoid blocking.
func (cb *CircularBuffer) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event)

	go func() {
		defer close(ch)

		cb.Lock()
		// Create a copy of the events to avoid holding the lock while sending to channel
		matchingEvents := cb.getMatchingEvents(filter)
		cb.Unlock()

		// Send matching events to the channel
		for i := range matchingEvents {
			select {
			case <-ctx.Done():
				return
			case ch <- &matchingEvents[i]:
			}
		}
	}()

	return ch, nil
}

// getMatchingEvents returns a slice of events that match the given filter.
// This function must be called with the lock held.
func (cb *CircularBuffer) getMatchingEvents(filter nostr.Filter) []nostr.Event {
	// Apply limit from filter or use all events if no limit
	limit := cb.count
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	// Pre-allocate the result slice
	result := make([]nostr.Event, 0, limit)

	// Start from the tail (oldest) and move towards head (newest)
	for i, idx := 0, cb.tail; i < cb.count; i++ {
		evt := cb.buffer[idx]
		if cb.eventMatchesFilter(&evt, filter) {
			result = append(result, evt)
			if len(result) >= limit {
				break
			}
		}
		idx = (idx + 1) % cb.size
	}

	return result
}

// eventMatchesFilter checks if an event matches the given filter.
// Implements the Nostr filter matching logic for IDs, authors, kinds, tags, and timestamps.
func (cb *CircularBuffer) eventMatchesFilter(evt *nostr.Event, filter nostr.Filter) bool {
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

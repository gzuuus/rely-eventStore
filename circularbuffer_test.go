package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// createTestEvent creates a simple test event
func createTestEvent(id string, kind int) *nostr.Event {
	return &nostr.Event{
		ID:        id,
		Kind:      kind,
		Tags:      []nostr.Tag{{"e", "test-tag"}},
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		PubKey:    "test-pubkey-" + id,
		Content:   "test content " + id,
	}
}

// BenchmarkSingleWrite_Atomic2 tests single-threaded write performance of AtomicCircularBuffer2
func BenchmarkSingleWrite_Atomic2(b *testing.B) {
	cb := NewAtomicCircularBuffer2(1000)
	cb.Init()
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}
}

// BenchmarkConcurrentWrite_Atomic2 tests concurrent write performance of AtomicCircularBuffer2
func BenchmarkConcurrentWrite_Atomic2(b *testing.B) {
	cb := NewAtomicCircularBuffer2(1000)
	cb.Init()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			evt := createTestEvent(fmt.Sprintf("id-%d", counter), counter%5)
			cb.SaveEvent(ctx, evt)
			counter++
		}
	})
}

// BenchmarkQuery_Atomic2 tests query performance of AtomicCircularBuffer2
func BenchmarkQuery_Atomic2(b *testing.B) {
	cb := NewAtomicCircularBuffer2(1000)
	cb.Init()
	ctx := context.Background()

	// Fill buffer with events
	for i := 0; i < 500; i++ {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}

	filter := nostr.Filter{
		Kinds: []int{1, 2, 3},
		Limit: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cb.QueryEvents(ctx, filter)
		// No need to consume events as we get a slice directly
	}
}

// BenchmarkMixed_Atomic2 tests mixed read/write workload on AtomicCircularBuffer2
func BenchmarkMixed_Atomic2(b *testing.B) {
	cb := NewAtomicCircularBuffer2(1000)
	cb.Init()
	ctx := context.Background()

	// Pre-fill with some data
	for i := 0; i < 500; i++ {
		evt := createTestEvent(fmt.Sprintf("prefill-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}

	filter := nostr.Filter{
		Kinds: []int{1, 2, 3, 4},
		Limit: 50,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			// Alternate between read and write operations
			if counter%2 == 0 {
				_, _ = cb.QueryEvents(ctx, filter)
				// No need to consume events as we get a slice directly
			} else {
				evt := createTestEvent(fmt.Sprintf("mixed-%d", counter), counter%5)
				cb.SaveEvent(ctx, evt)
			}
			counter++
		}
	})
}

// TestAtomicCircularBuffer2 tests the correctness of the AtomicCircularBuffer2 implementation
func TestAtomicCircularBuffer2(t *testing.T) {
	// Test initialization
	cb := NewAtomicCircularBuffer2(5)
	err := cb.Init()
	if err != nil {
		t.Fatalf("Failed to initialize buffer: %v", err)
	}

	ctx := context.Background()

	// Test saving events
	for i := 0; i < 3; i++ {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i)
		err = cb.SaveEvent(ctx, evt)
		if err != nil {
			t.Fatalf("Failed to save event: %v", err)
		}
	}

	// Test querying events
	filter := nostr.Filter{
		Kinds: []int{0, 1, 2},
	}

	events, err := cb.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("Expected 3 events, got %d", len(events))
	}

	// Test buffer wrapping
	for i := 3; i < 8; i++ {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i)
		err = cb.SaveEvent(ctx, evt)
		if err != nil {
			t.Fatalf("Failed to save event: %v", err)
		}
		// Print debug info after each save
		t.Logf("Saved event id-%d, kind=%d", i, i)
	}

	// Print buffer state before query
	t.Logf("Buffer state: head=%d, count=%d, size=%d",
		atomic.LoadUint64(&cb.head),
		atomic.LoadUint64(&cb.count),
		cb.size)

	// Now buffer should have events 3-7 (5 events)
	filter = nostr.Filter{
		Kinds: []int{0, 1, 2, 3, 4, 5, 6, 7}, // Expanded to include all kinds
	}
	events, err = cb.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}

	// Print all events found
	t.Logf("Found %d events:", len(events))
	for i, evt := range events {
		t.Logf("Event %d: ID=%s, Kind=%d", i, evt.ID, evt.Kind)
	}

	if len(events) != 5 {
		t.Fatalf("Expected 5 events after wrapping, got %d", len(events))
	}

	// Check that the oldest events were evicted
	for _, evt := range events {
		id := evt.ID
		if id == "id-0" || id == "id-1" || id == "id-2" {
			t.Fatalf("Event %s should have been evicted", id)
		}
	}

	// Test filtering
	filterKind := nostr.Filter{
		Kinds: []int{3, 5, 7},
	}

	events, err = cb.QueryEvents(ctx, filterKind)
	if err != nil {
		t.Fatalf("Failed to query events with kind filter: %v", err)
	}

	for _, evt := range events {
		if evt.Kind != 3 && evt.Kind != 5 && evt.Kind != 7 {
			t.Fatalf("Event with kind %d should not match filter", evt.Kind)
		}
	}
}

// TestConcurrentSaveAndQuery2 tests concurrent saving and querying with AtomicCircularBuffer2
func TestConcurrentSaveAndQuery2(t *testing.T) {
	cb := NewAtomicCircularBuffer2(1000)
	cb.Init()
	ctx := context.Background()

	// Number of concurrent operations
	const numOps = 100

	// Start writers
	var wg sync.WaitGroup
	wg.Add(numOps)

	for i := 0; i < numOps; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				evt := createTestEvent(fmt.Sprintf("id-%d-%d", i, j), j%5)
				cb.SaveEvent(ctx, evt)
			}
		}(i)
	}

	// Start readers concurrently
	for i := 0; i < numOps/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			filter := nostr.Filter{
				Kinds: []int{1, 2, 3},
				Limit: 50,
			}

			for j := 0; j < 5; j++ {
				events, err := cb.QueryEvents(ctx, filter)
				if err != nil {
					t.Errorf("Error querying events: %v", err)
				}

				// Just verify we can access the events
				for _, evt := range events {
					if evt == nil {
						t.Error("Received nil event")
					}
				}

				// Small sleep to increase chance of interleaving with writes
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

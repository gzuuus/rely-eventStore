package main

import (
	"context"
	"fmt"
	"sync"
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

// BenchmarkSingleWrite_Original tests single-threaded write performance of original CircularBuffer
func BenchmarkSingleWrite_Original(b *testing.B) {
	cb := NewCircularBuffer(1000)
	cb.Init()
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}
}

// BenchmarkSingleWrite_Atomic tests single-threaded write performance of AtomicCircularBuffer
func BenchmarkSingleWrite_Atomic(b *testing.B) {
	cb := NewAtomicCircularBuffer(1000)
	cb.Init()
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}
}

// BenchmarkConcurrentWrite_Original tests concurrent write performance of original CircularBuffer
func BenchmarkConcurrentWrite_Original(b *testing.B) {
	cb := NewCircularBuffer(1000)
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

// BenchmarkConcurrentWrite_Atomic tests concurrent write performance of AtomicCircularBuffer
func BenchmarkConcurrentWrite_Atomic(b *testing.B) {
	cb := NewAtomicCircularBuffer(1000)
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

// BenchmarkQuery_Original tests query performance of original CircularBuffer
func BenchmarkQuery_Original(b *testing.B) {
	cb := NewCircularBuffer(1000)
	cb.Init()
	ctx := context.Background()
	
	// Fill buffer with events
	for i := range 500 {
		evt := createTestEvent(fmt.Sprintf("id-%d", i), i%5)
		cb.SaveEvent(ctx, evt)
	}
	
	filter := nostr.Filter{
		Kinds: []int{1, 2, 3},
		Limit: 100,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := cb.QueryEvents(ctx, filter)
		for range ch {
			// Just consume the events
		}
	}
}

// BenchmarkQuery_Atomic tests query performance of AtomicCircularBuffer
func BenchmarkQuery_Atomic(b *testing.B) {
	cb := NewAtomicCircularBuffer(1000)
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
		ch, _ := cb.QueryEvents(ctx, filter)
		for range ch {
			// Just consume the events
		}
	}
}

// BenchmarkMixed_Original tests mixed read/write workload on original CircularBuffer
func BenchmarkMixed_Original(b *testing.B) {
	cb := NewCircularBuffer(1000)
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
				ch, _ := cb.QueryEvents(ctx, filter)
				for range ch {
					// Just consume the events
				}
			} else {
				evt := createTestEvent(fmt.Sprintf("mixed-%d", counter), counter%5)
				cb.SaveEvent(ctx, evt)
			}
			counter++
		}
	})
}

// BenchmarkMixed_Atomic tests mixed read/write workload on AtomicCircularBuffer
func BenchmarkMixed_Atomic(b *testing.B) {
	cb := NewAtomicCircularBuffer(1000)
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
				ch, _ := cb.QueryEvents(ctx, filter)
				for range ch {
					// Just consume the events
				}
			} else {
				evt := createTestEvent(fmt.Sprintf("mixed-%d", counter), counter%5)
				cb.SaveEvent(ctx, evt)
			}
			counter++
		}
	})
}

// TestConcurrentSaveAndQuery tests concurrent saving and querying
func TestConcurrentSaveAndQuery(t *testing.T) {
	runTest := func(name string, createBuffer func() interface{}) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var saveEvent func(ctx context.Context, evt *nostr.Event) error
			var queryEvents func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error)
			
			buffer := createBuffer()
			switch cb := buffer.(type) {
			case *CircularBuffer:
				cb.Init()
				saveEvent = cb.SaveEvent
				queryEvents = cb.QueryEvents
			case *AtomicCircularBuffer:
				cb.Init()
				saveEvent = cb.SaveEvent
				queryEvents = cb.QueryEvents
			default:
				t.Fatalf("Unknown buffer type: %T", cb)
			}
			
			const numEvents = 1000
			const numWorkers = 10
			
			var wg sync.WaitGroup
			
			// Start writers
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; i < numEvents/numWorkers; i++ {
						evt := createTestEvent(fmt.Sprintf("id-%d-%d", workerID, i), i%5)
						err := saveEvent(ctx, evt)
						if err != nil {
							t.Errorf("Error saving event: %v", err)
						}
					}
				}(w)
			}
			
			// Start readers concurrently
			for r := 0; r < numWorkers/2; r++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					filter := nostr.Filter{
						Kinds: []int{1, 2, 3},
						Limit: 100,
					}
					
					for i := 0; i < 5; i++ {
						ch, err := queryEvents(ctx, filter)
						if err != nil {
							t.Errorf("Error querying events: %v", err)
							return
						}
						
						count := 0
						for range ch {
							count++
						}
					}
				}()
			}
			
			wg.Wait()
		})
	}
	
	runTest("Original", func() interface{} { return NewCircularBuffer(1000) })
	runTest("Atomic", func() interface{} { return NewAtomicCircularBuffer(1000) })
}

package main

import (
	"context"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestRace(t *testing.T) {
	ctx := context.Background()
	ab := NewAtomicCircularBuffer2(1000)
	event := createTestEvent("aaaaaaaaaaaaaaaaaaaaaaa", 1)

	go func() {
		for range 1000000 {
			ab.QueryEvents(ctx, nostr.Filter{IDs: []string{"aaa"}})
		}
	}()

	for range 1000000 {
		ab.SaveEvent(ctx, event)
	}
}

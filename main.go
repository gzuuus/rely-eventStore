package main

import (
	"context"
	"log"
	"slices"

	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/nbd-wtf/go-nostr"
	"github.com/pippellia-btc/rely"
)

var (
	db             sqlite3.SQLite3Backend
	ephemeralStore *AtomicCircularBuffer2
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rely.HandleSignals(cancel)

	db = sqlite3.SQLite3Backend{DatabaseURL: "./rely-sqlite.db"}

	ephemeralStore = NewAtomicCircularBuffer2(500)

	relay := rely.NewRelay()
	relay.OnEvent = Save
	relay.OnFilters = Query

	addr := "localhost:3334"
	log.Printf("[RELAY] running on %s", addr)

	if err := relay.StartAndServe(ctx, addr); err != nil {
	}
}

func Save(c *rely.Client, e *nostr.Event) error {
	log.Printf("[EVENT] received: %s (kind: %d)", e.ID, e.Kind)
	ctx := context.Background()

	switch {
	case nostr.IsEphemeralKind(e.Kind):
		err := ephemeralStore.SaveEvent(ctx, e)
		if err != nil {
			log.Printf("[ERROR] storing ephemeral event: %v", err)
			return err
		}
		log.Printf("[EPHEMERAL] stored: %s", e.ID)
		return nil

	case nostr.IsReplaceableKind(e.Kind), nostr.IsAddressableKind(e.Kind):
		return saveReplaceableEvent(ctx, e)

	default:
		err := db.SaveEvent(ctx, e)
		if err != nil {
			log.Printf("[ERROR] saving regular event: %v", err)
			return err
		}
		log.Printf("[REGULAR] saved: %s", e.ID)
		return nil
	}
}

func saveReplaceableEvent(ctx context.Context, e *nostr.Event) error {
	err := db.ReplaceEvent(ctx, e)
	if err != nil {
		log.Printf("[ERROR] saving replaceable/addressable event: %v", err)
		return err
	}
	log.Printf("[REPLACEABLE] saved: %s", e.ID)
	return nil
}

func Query(ctx context.Context, c *rely.Client, filters nostr.Filters) ([]nostr.Event, error) {
	log.Printf("[QUERY] received filters with %d subscriptions", len(filters))

	capacity := estimateCapacityFromFilters(filters)
	result := make([]nostr.Event, 0, capacity)

	for _, filter := range filters {
		hasEphemeralKinds := false
		if len(filter.Kinds) > 0 {
			hasEphemeralKinds = slices.ContainsFunc(filter.Kinds, nostr.IsEphemeralKind)
			log.Printf("[DEBUG] filter has kinds: %v, hasEphemeralKinds: %v", filter.Kinds, hasEphemeralKinds)
		} else {
			// If no kinds specified, assume all kinds including ephemeral
			hasEphemeralKinds = true
			log.Printf("[DEBUG] filter has no kinds specified, assuming hasEphemeralKinds: true")
		}

		eventChan, err := db.QueryEvents(ctx, filter)
		if err != nil {
			log.Printf("[ERROR] querying events: %v", err)
			return nil, err
		}
		for event := range eventChan {
			result = append(result, *event)
		}

		// Always query ephemeral store for events, regardless of filter kinds
		// This ensures we don't miss any ephemeral events
		log.Printf("[DEBUG] querying ephemeral store for filter: %v", filter)
		events, err := ephemeralStore.QueryEvents(ctx, filter)
		if err != nil {
			log.Printf("[ERROR] querying ephemeral events: %v", err)
		} else {
			for _, event := range events {
				if event != nil {
					result = append(result, *event)
				}
			}
		}
	}

	log.Printf("[QUERY] found %d events matching filters", len(result))
	return result, nil
}

func estimateCapacityFromFilters(filters nostr.Filters) int {
	const defaultCapacity = 16
	const maxCapacity = 2048
	const eventsPerFilter = 32

	if len(filters) == 0 {
		return defaultCapacity
	}

	totalCapacity := 0
	hasExplicitLimits := false

	for _, filter := range filters {
		if filter.Limit > 0 {
			totalCapacity += filter.Limit
			hasExplicitLimits = true
		}
	}

	if !hasExplicitLimits {
		totalCapacity = len(filters) * eventsPerFilter
	}

	if totalCapacity < defaultCapacity {
		return defaultCapacity
	}

	if totalCapacity > maxCapacity {
		return maxCapacity
	}

	return totalCapacity
}

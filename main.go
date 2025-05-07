package main

import (
	"context"
	"log"
	"slices"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/nbd-wtf/go-nostr"
	"github.com/pippellia-btc/rely"
)

var (
	db             sqlite3.SQLite3Backend
	ephemeralStore *slicestore.SliceStore
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rely.HandleSignals(cancel)

	db = sqlite3.SQLite3Backend{DatabaseURL: "./rely-sqlite.db"}
	if err := db.Init(); err != nil {
		panic(err)
	}

	ephemeralStore = &slicestore.SliceStore{MaxLimit: 500}
	if err := ephemeralStore.Init(); err != nil {
		panic(err)
	}

	relay := rely.NewRelay()
	relay.OnEvent = Save
	relay.OnFilters = Query

	addr := "localhost:3334"
	log.Printf("running relay on %s", addr)

	if err := relay.StartAndServe(ctx, addr); err != nil {
		panic(err)
	}
}

func Save(c *rely.Client, e *nostr.Event) error {
	log.Printf("received event: %v", e)
	ctx := context.Background()

	switch {
	case nostr.IsEphemeralKind(e.Kind):
		err := ephemeralStore.SaveEvent(ctx, e)
		if err != nil {
			log.Printf("error storing ephemeral event: %v", err)
			return err
		}
		log.Printf("ephemeral event stored temporarily: %s", e.ID)
		return nil

	case nostr.IsReplaceableKind(e.Kind), nostr.IsAddressableKind(e.Kind):
		return saveReplaceableEvent(ctx, e)

	default:
		err := db.SaveEvent(ctx, e)
		if err != nil {
			log.Printf("error saving regular event: %v", err)
			return err
		}
		log.Printf("regular event saved successfully: %s", e.ID)
		return nil
	}
}

func saveReplaceableEvent(ctx context.Context, e *nostr.Event) error {
	err := db.ReplaceEvent(ctx, e)
	if err != nil {
		log.Printf("error saving replaceable/addressable event: %v", err)
		return err
	}
	log.Printf("replaceable/addressable event saved successfully: %s", e.ID)
	return nil
}

func Query(ctx context.Context, c *rely.Client, filters nostr.Filters) ([]nostr.Event, error) {
	log.Printf("received filters %v", filters)

	capacity := estimateCapacityFromFilters(filters)
	result := make([]nostr.Event, 0, capacity)

	for _, filter := range filters {
		hasEphemeralKinds := false
		if len(filter.Kinds) > 0 {
			hasEphemeralKinds = slices.ContainsFunc(filter.Kinds, nostr.IsEphemeralKind)
		}

		eventChan, err := db.QueryEvents(ctx, filter)
		if err != nil {
			log.Printf("error querying events: %v", err)
			return nil, err
		}
		for event := range eventChan {
			result = append(result, *event)
		}

		if hasEphemeralKinds {
			ephemeralChan, err := ephemeralStore.QueryEvents(ctx, filter)
			if err != nil {
				log.Printf("error querying ephemeral events: %v", err)
			} else {
				for event := range ephemeralChan {
					result = append(result, *event)
				}
			}
		}
	}

	log.Printf("found %d events matching filters", len(result))
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

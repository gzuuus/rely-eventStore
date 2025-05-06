package main

import (
	"context"
	"log"

	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/nbd-wtf/go-nostr"
	"github.com/pippellia-btc/rely"
)

var db sqlite3.SQLite3Backend

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rely.HandleSignals(cancel)

	db = sqlite3.SQLite3Backend{DatabaseURL: "./rely-sqlite.db"}
	if err := db.Init(); err != nil {
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

	err := db.SaveEvent(context.Background(), e)
	if err != nil {
		log.Printf("error saving event: %v", err)
		return err
	}

	log.Printf("event saved successfully: %s", e.ID)
	return nil
}

func Query(ctx context.Context, c *rely.Client, filters nostr.Filters) ([]nostr.Event, error) {
	log.Printf("received filters %v", filters)

	var result []nostr.Event

	for _, filter := range filters {
		eventChan, err := db.QueryEvents(ctx, filter)
		if err != nil {
			log.Printf("error querying events: %v", err)
			return nil, err
		}
		for event := range eventChan {
			result = append(result, *event)
		}
	}

	log.Printf("found %d events matching filters", len(result))
	return result, nil
}

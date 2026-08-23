package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
)

func TestExternalEventsSurviveRestartAndUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backflash.db")
	event := external.ExternalEvent{Source: "polisen", ExternalID: "42", Timestamp: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC), Title: "Första", Summary: "Gammalt", FirstSeenAt: time.Now(), LastSeenAt: time.Now()}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveExternalEvents([]external.ExternalEvent{event}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := event
	updated.Title = "Uppdaterad"
	if err = db.SaveExternalEvents([]external.ExternalEvent{updated}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ExternalEvents("polisen", 10)
	if err != nil {
		t.Fatal(err)
	}
	events, err := ExternalEventFromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Title != "Uppdaterad" {
		t.Fatalf("unexpected persisted events: %#v", events)
	}
	if events[0].Timestamp.IsZero() || events[0].Timestamp.Year() != 2026 || events[0].Timestamp.Hour() != 12 {
		t.Fatalf("eventtid återlästes inte: %#v", events[0].Timestamp)
	}
	_ = db.Close()
}

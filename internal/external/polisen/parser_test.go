package polisen

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePolisenEvents(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "polisen_events.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.UTC)
	events, err := Parse(data, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	if events[0].ExternalID != "123456" || events[0].Title != "Trafikolycka" || events[0].URL != "https://polisen.se/aktuellt/handelser/2026/augusti/23/123456/" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
	if events[0].Latitude == nil || events[0].Longitude == nil || *events[0].Latitude != 59.8586 || *events[0].Longitude != 17.6389 {
		t.Fatalf("coordinates not parsed: %#v", events[0])
	}
	if events[1].Latitude != nil || events[1].Longitude != nil {
		t.Fatal("malformed GPS should be ignored")
	}
}

func TestParseDatetimeWithoutSeconds(t *testing.T) {
	events, err := Parse([]byte(`[{"id":1,"datetime":"2026-08-23 12:34","name":"Test","location":{}}]`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Timestamp.IsZero() || events[0].Timestamp.Year() != 2026 || events[0].Timestamp.Hour() != 12 || events[0].Timestamp.Minute() != 34 {
		t.Fatalf("datum utan sekunder parsades fel: %#v", events)
	}
}

func TestParseDatetimeFromSwedishEventName(t *testing.T) {
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	events, err := Parse([]byte(`[{"id":2,"datetime":"","name":"15 augusti 12.13, Brand, Boden","location":{}}]`), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Timestamp.IsZero() || events[0].Timestamp.Day() != 15 || events[0].Timestamp.Hour() != 12 || events[0].Timestamp.Minute() != 13 {
		t.Fatalf("svensk eventtid parsades fel: %#v", events)
	}
}

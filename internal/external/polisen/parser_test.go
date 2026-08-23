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

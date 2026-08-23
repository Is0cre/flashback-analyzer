package external

import (
	"context"
	"time"
)

// ExternalEvent is deliberately independent from Flashback posts and threads.
// A future matching service may relate the two, but ingestion never does.
type ExternalEvent struct {
	Source       string
	ExternalID   string
	Timestamp    time.Time
	Title        string
	Summary      string
	EventType    string
	LocationName string
	Latitude     *float64
	Longitude    *float64
	URL          string
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
}

type SyncState struct {
	Source       string
	LastSyncedAt time.Time
	Status       string
}

type Provider interface {
	Source() string
	Fetch(context.Context) ([]ExternalEvent, error)
}

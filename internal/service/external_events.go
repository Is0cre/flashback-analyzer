package service

import (
	"context"
	"errors"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/store"
)

type ExternalEventsService struct {
	Store        *store.Store
	Provider     external.Provider
	RefreshAfter time.Duration
	Now          func() time.Time
}

func (s ExternalEventsService) Cached(source string, limit int) ([]external.ExternalEvent, error) {
	rows, err := s.Store.ExternalEvents(source, limit)
	if err != nil {
		return nil, err
	}
	return store.ExternalEventFromRows(rows)
}
func (s ExternalEventsService) Stale(source string) bool {
	state, err := s.Store.ExternalSyncState(source)
	if err != nil {
		return true
	}
	if state.Status == "permanent" {
		return false
	}
	// Older builds failed to parse API datetimes without seconds and persisted
	// year-one timestamps. Force one repair refresh instead of displaying them.
	var invalidTimes int
	if s.Store != nil {
		_ = s.Store.DB.QueryRow(`SELECT COUNT(*) FROM external_events WHERE source=? AND (event_time LIKE '0001-%' OR event_time IS NULL)`, source).Scan(&invalidTimes)
	}
	if invalidTimes > 0 {
		return true
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return state.LastSyncedAt.IsZero() || now.Sub(state.LastSyncedAt) >= s.RefreshAfter
}
func (s ExternalEventsService) Refresh(ctx context.Context) ([]external.ExternalEvent, error) {
	events, err := s.Provider.Fetch(ctx)
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if err != nil {
		status := "fel: " + err.Error()
		var permanent interface{ Permanent() bool }
		if errors.As(err, &permanent) && permanent.Permanent() {
			status = "permanent"
		}
		_ = s.Store.SetExternalSyncState(external.SyncState{Source: s.Provider.Source(), LastSyncedAt: now, Status: status})
		return nil, err
	}
	for i := range events {
		if events[i].FirstSeenAt.IsZero() {
			events[i].FirstSeenAt = now
		}
		events[i].LastSeenAt = now
	}
	if err = s.Store.SaveExternalEvents(events); err != nil {
		return nil, err
	}
	_ = s.Store.SetExternalSyncState(external.SyncState{Source: s.Provider.Source(), LastSyncedAt: now, Status: "ok"})
	return events, nil
}

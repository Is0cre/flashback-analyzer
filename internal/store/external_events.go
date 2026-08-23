package store

import (
	"database/sql"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
)

func (s *Store) SaveExternalEvents(events []external.ExternalEvent) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, event := range events {
		_, err = tx.Exec(`INSERT INTO external_events(source,external_id,event_time,title,summary,event_type,location_name,latitude,longitude,url,first_seen_at,last_seen_at,content_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source,external_id) DO UPDATE SET event_time=excluded.event_time,title=excluded.title,summary=excluded.summary,event_type=excluded.event_type,location_name=excluded.location_name,latitude=excluded.latitude,longitude=excluded.longitude,url=excluded.url,last_seen_at=excluded.last_seen_at,content_hash=excluded.content_hash`, event.Source, event.ExternalID, event.Timestamp, event.Title, event.Summary, event.EventType, event.LocationName, event.Latitude, event.Longitude, event.URL, event.FirstSeenAt, event.LastSeenAt, external.ContentHash(event))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ExternalEvents(source string, limit int) (*sql.Rows, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.DB.Query(`SELECT source,external_id,event_time,title,summary,event_type,location_name,latitude,longitude,url,first_seen_at,last_seen_at FROM external_events WHERE source=? ORDER BY event_time DESC LIMIT ?`, source, limit)
}

func (s *Store) SetExternalSyncState(state external.SyncState) error {
	_, err := s.DB.Exec(`INSERT INTO external_sync_state(source,last_synced_at,status) VALUES(?,?,?) ON CONFLICT(source) DO UPDATE SET last_synced_at=excluded.last_synced_at,status=excluded.status`, state.Source, state.LastSyncedAt, state.Status)
	return err
}
func (s *Store) ExternalSyncState(source string) (external.SyncState, error) {
	var state external.SyncState
	var at sql.NullString
	err := s.DB.QueryRow(`SELECT source,last_synced_at,status FROM external_sync_state WHERE source=?`, source).Scan(&state.Source, &at, &state.Status)
	if at.Valid {
		state.LastSyncedAt, _ = time.Parse(time.RFC3339Nano, at.String)
	}
	return state, err
}
func ExternalEventFromRows(rows *sql.Rows) ([]external.ExternalEvent, error) {
	defer rows.Close()
	var out []external.ExternalEvent
	for rows.Next() {
		var e external.ExternalEvent
		var eventTime, first, last sql.NullString
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&e.Source, &e.ExternalID, &eventTime, &e.Title, &e.Summary, &e.EventType, &e.LocationName, &lat, &lon, &e.URL, &first, &last); err != nil {
			return nil, err
		}
		if eventTime.Valid {
			e.Timestamp, _ = time.Parse(time.RFC3339Nano, eventTime.String)
		}
		if first.Valid {
			e.FirstSeenAt, _ = time.Parse(time.RFC3339Nano, first.String)
		}
		if last.Valid {
			e.LastSeenAt, _ = time.Parse(time.RFC3339Nano, last.String)
		}
		if lat.Valid {
			v := lat.Float64
			e.Latitude = &v
		}
		if lon.Valid {
			v := lon.Float64
			e.Longitude = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

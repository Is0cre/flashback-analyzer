package service

import (
	"context"
	"os"
	"time"

	"github.com/backflash-cli/backflash/internal/store"
)

type HotThread struct {
	ID    string
	Title string
	Posts int
}

// DashboardSnapshot is assembled before rendering. View() must remain a pure
// presentation function: no SQL, network, Gandr vault, or mesh startup lives
// in the render path.
type DashboardSnapshot struct {
	ForumCount    int
	ThreadCount   int
	PostCount     int
	DBSize        int64
	PostsLastHour int
	ActiveThreads int
	ActiveForums  int
	NewThreads    int
	HotThreads    []HotThread
	UpdatedAt     time.Time
	Network       string
	Session       string
	Sync          string
	Mesh          string
	Gandr         string
}

type DashboardService struct {
	Store *store.Store
	Now   func() time.Time
}

func (s *DashboardService) Snapshot(ctx context.Context) (DashboardSnapshot, error) {
	if s == nil || s.Store == nil {
		return DashboardSnapshot{}, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	cutoff := now.Add(-time.Hour).Format(time.RFC3339Nano)
	var out DashboardSnapshot
	for _, q := range []struct {
		dst *int
		sql string
	}{
		{&out.ForumCount, `SELECT COUNT(*) FROM forums`},
		{&out.ThreadCount, `SELECT COUNT(*) FROM threads`},
		{&out.PostCount, `SELECT COUNT(*) FROM posts`},
		{&out.PostsLastHour, `SELECT COUNT(*) FROM posts WHERE timestamp >= ?`},
		{&out.ActiveThreads, `SELECT COUNT(DISTINCT thread_id) FROM posts WHERE timestamp >= ?`},
		{&out.ActiveForums, `SELECT COUNT(DISTINCT ft.forum_id) FROM posts p JOIN forum_threads ft ON ft.thread_id=p.thread_id WHERE p.timestamp >= ?`},
		{&out.NewThreads, `SELECT COUNT(*) FROM threads WHERE last_seen_at >= ?`},
	} {
		var err error
		if q.sql == `SELECT COUNT(*) FROM posts WHERE timestamp >= ?` || q.sql == `SELECT COUNT(DISTINCT thread_id) FROM posts WHERE timestamp >= ?` || q.sql == `SELECT COUNT(DISTINCT ft.forum_id) FROM posts p JOIN forum_threads ft ON ft.thread_id=p.thread_id WHERE p.timestamp >= ?` || q.sql == `SELECT COUNT(*) FROM threads WHERE last_seen_at >= ?` {
			err = s.Store.DB.QueryRowContext(ctx, q.sql, cutoff).Scan(q.dst)
		} else {
			err = s.Store.DB.QueryRowContext(ctx, q.sql).Scan(q.dst)
		}
		if err != nil {
			return DashboardSnapshot{}, err
		}
	}
	if info, err := os.Stat(s.Store.Path); err == nil {
		out.DBSize = info.Size()
	}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT t.id, COALESCE(NULLIF(t.title,''),'—'), COUNT(*) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.timestamp >= ? GROUP BY t.id,t.title ORDER BY COUNT(*) DESC LIMIT 5`, cutoff)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var hot HotThread
		if err := rows.Scan(&hot.ID, &hot.Title, &hot.Posts); err != nil {
			return DashboardSnapshot{}, err
		}
		out.HotThreads = append(out.HotThreads, hot)
	}
	if err := rows.Err(); err != nil {
		return DashboardSnapshot{}, err
	}
	out.UpdatedAt = now
	out.Network = "—"
	out.Session = "ANONYM"
	out.Sync = "VILAR"
	out.Mesh = "AV"
	out.Gandr = "LÅST"
	return out, nil
}

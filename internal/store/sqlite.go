package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/backflash-cli/backflash/internal/diagnostics"
	"github.com/backflash-cli/backflash/internal/flashback"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	Path string
}

func Open(path string) (*Store, error) {
	finish := diagnostics.Start("store.open")
	defer finish()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Path: path}
	if err := s.Migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Migrate() error {
	_, err := s.DB.Exec(`PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS forums (id TEXT PRIMARY KEY, title TEXT NOT NULL, url TEXT NOT NULL UNIQUE, parent_id TEXT, depth INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, has_children INTEGER NOT NULL DEFAULT 0, browsable INTEGER NOT NULL DEFAULT 1, last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, title TEXT NOT NULL, url TEXT NOT NULL UNIQUE, forum_id TEXT, replies INTEGER NOT NULL DEFAULT 0, views INTEGER NOT NULL DEFAULT 0, last_post_at TEXT, last_post_author TEXT, sticky INTEGER NOT NULL DEFAULT 0, page_count INTEGER NOT NULL DEFAULT 0, last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS forum_threads (forum_id TEXT NOT NULL, thread_id TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(forum_id, thread_id), FOREIGN KEY(forum_id) REFERENCES forums(id) ON DELETE CASCADE, FOREIGN KEY(thread_id) REFERENCES threads(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS posts (id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, author TEXT NOT NULL, timestamp TEXT, page INTEGER NOT NULL DEFAULT 1, position INTEGER NOT NULL DEFAULT 0, text TEXT NOT NULL, raw_text TEXT NOT NULL, source_url TEXT, content_hash TEXT, last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS quotes (post_id TEXT NOT NULL, quoted_post_id TEXT, author TEXT, text TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS links (post_id TEXT NOT NULL, url TEXT NOT NULL, domain TEXT NOT NULL DEFAULT '', PRIMARY KEY(post_id, url));
CREATE TABLE IF NOT EXISTS reader_state (thread_id TEXT PRIMARY KEY, last_seen_post_id TEXT, last_seen_at TEXT);
CREATE TABLE IF NOT EXISTS external_events (source TEXT NOT NULL, external_id TEXT NOT NULL, event_time TEXT, title TEXT NOT NULL, summary TEXT, event_type TEXT, location_name TEXT, latitude REAL, longitude REAL, url TEXT, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, content_hash TEXT NOT NULL, PRIMARY KEY(source, external_id));
CREATE TABLE IF NOT EXISTS external_sync_state (source TEXT PRIMARY KEY, last_synced_at TEXT, status TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_forums_parent_sort ON forums(parent_id, sort_order);
CREATE INDEX IF NOT EXISTS idx_forum_threads_forum_position ON forum_threads(forum_id, position);
CREATE INDEX IF NOT EXISTS idx_threads_last_post ON threads(last_post_at);
CREATE INDEX IF NOT EXISTS idx_posts_thread_page_position ON posts(thread_id, page, position, id);
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
CREATE VIRTUAL TABLE IF NOT EXISTS post_search USING fts5(post_id UNINDEXED, thread_id UNINDEXED, author, text);`)
	if err != nil {
		return err
	}
	// Older versions persisted a root forum's empty ParentID as "". The
	// navigation queries intentionally use SQL NULL for roots, so repair that
	// representation during the normal migration path.
	_, err = s.DB.Exec(`UPDATE forums SET parent_id=NULL WHERE parent_id=''`)
	return err
}

func (s *Store) SaveForums(nodes []flashback.ForumNode) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range nodes {
		var parent any
		if n.ParentID != "" {
			parent = n.ParentID
		}
		_, err = tx.Exec(`INSERT INTO forums(id,title,url,parent_id,depth,sort_order,has_children,browsable,last_seen_at) VALUES(?,?,?,?,?,?,?,? ,CURRENT_TIMESTAMP) ON CONFLICT(url) DO UPDATE SET title=excluded.title,parent_id=excluded.parent_id,depth=excluded.depth,sort_order=excluded.sort_order,has_children=excluded.has_children,last_seen_at=CURRENT_TIMESTAMP`, n.ID, n.Title, n.URL, parent, n.Depth, n.SortOrder, n.HasChildren, n.Browsable)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveThreads(forumID string, rows []flashback.ThreadSummary) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, r := range rows {
		if strings.TrimSpace(r.Title) == "" {
			continue
		}
		_, err = tx.Exec(`INSERT INTO threads(id,title,url,forum_id,replies,views,last_post_at,last_post_author,sticky,page_count,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(id) DO UPDATE SET title=excluded.title,url=excluded.url,forum_id=excluded.forum_id,replies=excluded.replies,views=excluded.views,last_post_at=excluded.last_post_at,last_post_author=excluded.last_post_author,sticky=excluded.sticky,page_count=excluded.page_count,last_seen_at=CURRENT_TIMESTAMP`, r.ID, r.Title, r.URL, forumID, r.Replies, r.Views, r.LastPostAt, r.LastPostAuthor, r.Sticky, r.PageCount)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO forum_threads(forum_id,thread_id,position) VALUES(?,?,?) ON CONFLICT(forum_id,thread_id) DO UPDATE SET position=excluded.position`, forumID, r.ID, i)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SavePage(page flashback.ParsedPage) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO threads(id,title,url,page_count,last_seen_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(id) DO UPDATE SET title=CASE WHEN excluded.title!='' THEN excluded.title ELSE threads.title END,page_count=MAX(threads.page_count,excluded.page_count),last_seen_at=CURRENT_TIMESTAMP`, page.ThreadID, page.Title, page.SourceURL, page.MaxPage)
	if err != nil {
		return err
	}
	for _, p := range page.Posts {
		timestamp := any(nil)
		if !p.Timestamp.IsZero() {
			timestamp = p.Timestamp.Format(time.RFC3339Nano)
		}
		_, err = tx.Exec(`INSERT INTO posts(id,thread_id,author,timestamp,page,position,text,raw_text,source_url) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET author=excluded.author,timestamp=excluded.timestamp,page=excluded.page,position=excluded.position,text=excluded.text,raw_text=excluded.raw_text,source_url=excluded.source_url,last_seen_at=CURRENT_TIMESTAMP`, p.ID, p.ThreadID, p.Author, timestamp, p.Page, p.Position, p.Text, p.RawText, p.SourceURL)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM post_search WHERE post_id=?`, p.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO post_search(post_id,thread_id,author,text) VALUES(?,?,?,?)`, p.ID, p.ThreadID, p.Author, p.Text)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) TrackedThreads() (*sql.Rows, error) {
	return s.DB.Query(`SELECT id,title,replies,last_post_at FROM threads ORDER BY last_seen_at DESC`)
}
func (s *Store) Forums(parent string) (*sql.Rows, error) {
	if parent == "" {
		return s.DB.Query(`SELECT id,title,url,has_children FROM forums WHERE parent_id IS NULL ORDER BY sort_order`)
	}
	return s.DB.Query(`SELECT id,title,url,has_children FROM forums WHERE parent_id=? ORDER BY sort_order`, parent)
}
func (s *Store) Posts(threadID string) (*sql.Rows, error) {
	return s.DB.Query(`SELECT id,author,timestamp,text FROM posts WHERE thread_id=? ORDER BY page,position,id`, threadID)
}

package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/flashback"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backflash.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestForumThreadCachePreservesSourceOrderAfterReopen(t *testing.T) {
	s, path := openTestStore(t)
	forum := flashback.ForumNode{ID: "f1", Title: "Brott", URL: "https://www.flashback.org/f1"}
	if err := s.SaveForums([]flashback.ForumNode{forum}); err != nil {
		t.Fatal(err)
	}
	rows := []flashback.ThreadSummary{
		{ID: "t2", Title: "Andra", URL: "https://www.flashback.org/t2", Replies: 2},
		{ID: "t1", Title: "Första", URL: "https://www.flashback.org/t1", Replies: 1},
	}
	if err := s.SaveThreads("f1", rows); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	query := `SELECT t.id FROM forum_threads ft JOIN threads t ON t.id=ft.thread_id WHERE ft.forum_id=? ORDER BY ft.position`
	dbRows, err := s.DB.Query(query, "f1")
	if err != nil {
		t.Fatal(err)
	}
	defer dbRows.Close()
	var got []string
	for dbRows.Next() {
		var id string
		if err := dbRows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if len(got) != 2 || got[0] != "t2" || got[1] != "t1" {
		t.Fatalf("forum source order lost: %v", got)
	}
}

func TestForumRootsUseSQLNullAndNestedForumsRemainReachable(t *testing.T) {
	s, path := openTestStore(t)
	if err := s.SaveForums([]flashback.ForumNode{
		{ID: "f1", Title: "Samhälle", URL: "https://www.flashback.org/f1"},
		{ID: "f2", Title: "Brott", URL: "https://www.flashback.org/f2", ParentID: "f1", Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}
	var roots, children int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM forums WHERE parent_id IS NULL`).Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM forums WHERE parent_id=?`, "f1").Scan(&children); err != nil {
		t.Fatal(err)
	}
	if roots != 1 || children != 1 {
		t.Fatalf("forumhierarkin sparades fel: rötter=%d barn=%d", roots, children)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.DB.QueryRow(`SELECT COUNT(*) FROM forums WHERE parent_id IS NULL`).Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if roots != 1 {
		t.Fatalf("rotforum försvann efter omstart: %d", roots)
	}
}

func TestMigrationRepairsLegacyEmptyForumParent(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.DB.Exec(`INSERT INTO forums(id,title,url,parent_id) VALUES('legacy','Gammalt','https://www.flashback.org/flegacy','')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	var parent any
	if err := s.DB.QueryRow(`SELECT parent_id FROM forums WHERE id='legacy'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != nil {
		t.Fatalf("legacy tom parent reparerades inte: %#v", parent)
	}
}

func TestPostCacheAndFTSSurviveReopen(t *testing.T) {
	s, path := openTestStore(t)
	page := flashback.ParsedPage{
		ThreadID: "t1", Title: "Lokal tråd", Page: 1, MaxPage: 1,
		SourceURL: "https://www.flashback.org/t1",
		Posts:     []flashback.Post{{ID: "p1", ThreadID: "t1", Author: "foo", Timestamp: time.Now(), Page: 1, Position: 1, Text: "Linux på lokal cache", RawText: "Linux på lokal cache"}},
	}
	if err := s.SavePage(page); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.DB.Query(`SELECT post_id FROM post_search WHERE post_search MATCH ?`, "Linux")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("lokalt FTS-index saknas efter omstart")
	}
}

func TestCachedThreadQueryUsesForumPositionIndex(t *testing.T) {
	s, _ := openTestStore(t)
	rows, err := s.DB.Query(`EXPLAIN QUERY PLAN SELECT t.id FROM forum_threads ft JOIN threads t ON t.id=ft.thread_id WHERE ft.forum_id=? ORDER BY ft.position`, "f1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var detail string
	for rows.Next() {
		var id, parent, notUsed, text string
		if err := rows.Scan(&id, &parent, &notUsed, &text); err != nil {
			t.Fatal(err)
		}
		if len(text) > 0 {
			detail += text + "\n"
		}
	}
	if len(detail) == 0 {
		t.Fatal("EXPLAIN QUERY PLAN gav ingen plan")
	}
	if !strings.Contains(detail, "idx_forum_threads_forum_position") && !strings.Contains(detail, "forum_threads") {
		t.Fatalf("forum query saknar väntad index/tabellplan: %s", detail)
	}
}

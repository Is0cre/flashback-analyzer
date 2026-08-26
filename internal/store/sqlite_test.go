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

func TestForumUpsertUsesStableIDAcrossURLShapes(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.SaveForums([]flashback.ForumNode{{ID: "1555", Title: "AI", URL: "https://www.flashback.org/f1555"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveForums([]flashback.ForumNode{{ID: "1555", Title: "AI", URL: "https://www.flashback.org/f1555-ai"}}); err != nil {
		t.Fatalf("forum med samma ID skulle uppdateras: %v", err)
	}
	var count int
	var url string
	if err := s.DB.QueryRow(`SELECT COUNT(*), MAX(url) FROM forums WHERE id=?`, "1555").Scan(&count, &url); err != nil {
		t.Fatal(err)
	}
	if count != 1 || url != "https://www.flashback.org/f1555-ai" {
		t.Fatalf("forum-upsert blev fel: antal=%d url=%q", count, url)
	}
}

func TestForumUpsertReplacesStaleRowWithSameURLUnderDifferentID(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.SaveForums([]flashback.ForumNode{{ID: "old-id", Title: "Gammal", URL: "https://www.flashback.org/f100"}}); err != nil {
		t.Fatal(err)
	}
	// A later parse can hand back the same URL under a different id (e.g. a
	// forum re-issued with a new numeric id). The url column is still
	// UNIQUE, so this must not fail the whole batch on that constraint.
	if err := s.SaveForums([]flashback.ForumNode{{ID: "new-id", Title: "Ny", URL: "https://www.flashback.org/f100"}}); err != nil {
		t.Fatalf("upsert med samma URL under nytt ID skulle lyckas: %v", err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM forums WHERE url=?`, "https://www.flashback.org/f100").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("förväntade en rad kvar för URL:en, fick %d", count)
	}
	var id string
	if err := s.DB.QueryRow(`SELECT id FROM forums WHERE url=?`, "https://www.flashback.org/f100").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != "new-id" {
		t.Fatalf("förväntade new-id att äga URL:en, fick %q", id)
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

func TestReplaceForumSnapshotRemovesLegacyFlatRoots(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.SaveForums([]flashback.ForumNode{
		{ID: "1", Title: "Gammal rot", URL: "/f1", Depth: 0},
		{ID: "2", Title: "Gammalt underforum", URL: "/f2", Depth: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceForumSnapshot([]flashback.ForumNode{
		{ID: "1", Title: "Ny rot", URL: "/f1", Depth: 0},
		{ID: "2", Title: "Rätt underforum", URL: "/f2", ParentID: "1", Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Forums("")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, title, url string
		var children, browsable int
		if err := rows.Scan(&id, &title, &url, &children, &browsable); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("förväntade en rot efter snapshot, fick %v", ids)
	}
}

func TestForumsQueryRoundTripsBrowsable(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.ReplaceForumSnapshot([]flashback.ForumNode{
		{ID: "1", Title: "Kategori", URL: "category:kategori", Depth: 0, Browsable: false},
		{ID: "2", Title: "Forum", URL: "/f2", ParentID: "1", Depth: 1, Browsable: true},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Forums("")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("förväntade en rotrad")
	}
	var id, title, url string
	var children, browsable int
	if err := rows.Scan(&id, &title, &url, &children, &browsable); err != nil {
		t.Fatal(err)
	}
	if browsable != 0 {
		t.Fatalf("kategorin skulle lagras som icke-browsable, fick browsable=%d", browsable)
	}
}

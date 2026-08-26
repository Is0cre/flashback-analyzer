package flashback

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseNavigationUsesForumCellsAndPreservesOddNames(t *testing.T) {
	nodes, err := ParseNavigation(fixture(t, "navigation_root.html"), BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 6 {
		t.Fatalf("got %d forum nodes, want 6", len(nodes))
	}
	for _, bad := range []string{"PeterNoster", "Fl1pst3r", "WeirdRaccoon", "Alibabbla", "Cpt.Pepper", "Fanten", "Svartkatt13", "SC430"} {
		for _, node := range nodes {
			if node.Title == bad {
				t.Fatalf("user leaked into forum tree: %s", bad)
			}
		}
	}
	if nodes[0].Title != "Samhälle" || nodes[1].Title != "Politik" || nodes[3].Title != "Dator och IT" || nodes[5].Title != "Felix042" {
		t.Fatalf("unexpected hierarchy/order: %#v", nodes)
	}
	if nodes[1].ParentID != "10" || nodes[5].ParentID != "20" {
		t.Fatalf("parent links not preserved: %#v", nodes)
	}
}

func TestParseNestedNavigation(t *testing.T) {
	nodes, err := ParseNavigation(fixture(t, "navigation_nested.html"), BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ParentID != "12" {
		t.Fatalf("unexpected nested nodes: %#v", nodes)
	}
}

func TestParsersDecodeLegacyWindows1252ForumText(t *testing.T) {
	html := `<table class="forumslist"><tr><td class="td_forum"><a href="/f9-samhalle">Samh` + string([]byte{0xe4}) + `lle</a></td></tr></table>`
	nodes, err := ParseNavigation(html, BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Title != "Samhälle" {
		t.Fatalf("Windows-1252-text dekoderades fel: %#v", nodes)
	}
}

func TestParseThreadListingExtractsTitlesNotIDs(t *testing.T) {
	rows, err := ParseThreadListing(fixture(t, "forum_listing.html"), "https://www.flashback.org/f10", "10")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Title != "Viktig information" || rows[1].Title != "Vanlig tråd" {
		t.Fatalf("unexpected thread rows: %#v", rows)
	}
	if rows[0].ID != "1001" || rows[0].Replies != 12 || rows[0].Views != 1204 || !rows[0].Sticky {
		t.Fatalf("metadata not parsed: %#v", rows[0])
	}
}

func TestParseThreadListingSupportsStructItems(t *testing.T) {
	rows, err := ParseThreadListing(fixture(t, "forum_listing_structitem.html"), BaseURL+"f3-droger", "3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Title != "Tråd från ny forumlayout" || rows[1].ID != "2002" {
		t.Fatalf("structItem-trådar parsades fel: %#v", rows)
	}
}

func TestParseThreadListingSupportsLegacyThreadRows(t *testing.T) {
	rows, err := ParseThreadListing(fixture(t, "forum_listing_thread_rows.html"), BaseURL+"f3-droger", "3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "3001" || rows[1].Title != "Första sidan: vanlig tråd" {
		t.Fatalf("äldre thread-rader parsades fel: %#v", rows)
	}
}

func TestParseThreadListingSupportsFlashbackThreadsList(t *testing.T) {
	rows, err := ParseThreadListing(fixture(t, "forum_listing_flashback_real.html"), BaseURL+"f246-retrospel-60622", "246")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("Flashback-listan gav %d rader, väntade 2: %#v", len(rows), rows)
	}
	if rows[0].Title != "Jag söker ett spel - RETRO-tråden" || rows[0].ID != "833642" || rows[0].Replies != 5546 || rows[0].Views != 554074 || !rows[0].Sticky {
		t.Fatalf("första Flashback-tråden parsades fel: %#v", rows[0])
	}
	if rows[0].LastPostAuthor != "fearreaper" || rows[0].LastPostAt.IsZero() {
		t.Fatalf("senaste inlägg parsades fel: %#v", rows[0])
	}
	if rows[1].Title != "Värderingstråden för retrospel, konsoler och tillbehör." {
		t.Fatalf("andra trådens titel parsades fel: %#v", rows[1])
	}
}

func TestParseThreadListingUsesScopedForumdisplayFallback(t *testing.T) {
	rows, err := ParseThreadListing(fixture(t, "forum_listing_forumdisplay.html"), BaseURL+"f3-droger", "3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "4001" || rows[0].Title != "Forumdisplay-tråd" {
		t.Fatalf("forumdisplay-fallback parsades fel: %#v", rows)
	}
}

func TestParseSearchResults(t *testing.T) {
	rows, err := ParseSearchResults(fixture(t, "search_results.html"), BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Title != "Linuxtråden" || rows[0].Author != "PeterNoster" || rows[0].ThreadID != "7001" {
		t.Fatalf("unexpected search rows: %#v", rows)
	}
}

func TestParseThreadPage(t *testing.T) {
	page, err := ParseThreadPage(fixture(t, "thread_page.html"), "https://www.flashback.org/t999", "999", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Testtråd" || len(page.Posts) != 2 || page.Posts[0].Author != "Alice" || page.Posts[0].Timestamp.IsZero() || page.Posts[0].Timestamp.Hour() != 10 || page.MaxPage != 7 {
		t.Fatalf("unexpected page: %#v", page)
	}
	if page.Posts[0].Text != "Jag tror att X stämmer. källa" || len(page.Posts[0].Quotes) != 1 {
		t.Fatalf("quote was not separated from original text: %#v", page.Posts[0])
	}
}

func TestParseThreadPageReadsTotalPagesFromFirstPageMetadata(t *testing.T) {
	html := `<!doctype html><html><body>
		<h1>Sidtest</h1>
		<span class="input-page-jump" data-total-pages="42" data-page="1">Sidan 1 av 42</span>
		<div class="post" id="post_9001"><div class="post-user-username">Alice</div><div class="post_message">Hej</div></div>
	</body></html>`
	page, err := ParseThreadPage(html, BaseURL+"t9000", "9000", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.MaxPage != 42 {
		t.Fatalf("first-page pagination metadata was not parsed: got %d, want 42", page.MaxPage)
	}
}

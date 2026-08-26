package flashback

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParseSitemapNavigationPreservesNestedForumTree(t *testing.T) {
	// Flashback's real sitemap places a node's children as a following
	// sibling <li><ul>...</ul></li>, never nested inside its own <li>, and
	// top-level category labels (<strong>, no href) are not forums. See
	// tests/fixtures/sitemap_navigation.html.
	nodes, err := ParseSitemapNavigation(fixture(t, "sitemap_navigation.html"), BaseURL+"sitemap/index.php/")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 6 {
		t.Fatalf("got %d sitemap forums, want 6: %#v", len(nodes), nodes)
	}
	if nodes[0].Title != "Dator och IT" || nodes[0].ParentID != "" || nodes[0].Browsable {
		t.Fatalf("root category parsed incorrectly: %#v", nodes[0])
	}
	if nodes[1].Title != "AI - Artificiell intelligens" || nodes[1].ParentID != nodes[0].ID || nodes[1].URL != BaseURL+"f555" {
		t.Fatalf("category child parsed incorrectly: %#v", nodes[1])
	}
	if nodes[2].Title != "Dator- och konsolspel" || nodes[2].ParentID != nodes[0].ID || nodes[2].URL != BaseURL+"f245" {
		t.Fatalf("second category child parsed incorrectly: %#v", nodes[2])
	}
	if nodes[3].Title != "Retrospel" || nodes[3].ParentID != "245" {
		t.Fatalf("deep forum parent parsed incorrectly: %#v", nodes[3])
	}
	if nodes[5].Title != "Felix042" || nodes[5].ParentID != "" {
		t.Fatalf("standalone root forum received an unexpected parent: %#v", nodes[5])
	}
	for _, bad := range []string{"PeterNoster", "En tråd", "Sök"} {
		for _, node := range nodes {
			if node.Title == bad {
				t.Fatalf("unrelated sitemap link leaked into forum tree: %s", bad)
			}
		}
	}
}

func TestClassifySitemapForumURL(t *testing.T) {
	if got := ClassifyURL("/sitemap/index.php/f-555.html", BaseURL); got != LinkForum {
		t.Fatalf("sitemap forum link classified as %v", got)
	}
	if got := CanonicalForumURL(BaseURL + "sitemap/index.php/f-555.html"); got != BaseURL+"f555" {
		t.Fatalf("sitemap forum URL not canonicalised: %q", got)
	}
}

func TestParseSitemapNavigationKeepsUnlinkedCategoryParents(t *testing.T) {
	html := `<nav id="forum-sitemap"><ul>
		<li>Droger<ul>
			<li><a href="/sitemap/index.php/f-10.html">Bensodiazepiner</a></li>
			<li><a href="/sitemap/index.php/f-11.html">Cannabis</a></li>
		</ul></li>
	</ul></nav><div><a href="/u99">Bensodiazepiner</a></div>`
	nodes, err := ParseSitemapNavigation(html, BaseURL+"sitemap/index.php/")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].Title != "Droger" || nodes[0].Browsable || nodes[1].ParentID != nodes[0].ID || nodes[2].ParentID != nodes[0].ID {
		t.Fatalf("kategori/underforum parsades fel: %#v", nodes)
	}
	if nodes[0].URL != nodes[0].ID || strings.Contains(nodes[0].URL, "category:category:") {
		t.Fatalf("kategori-URL fick dubbelt prefix: id=%q url=%q", nodes[0].ID, nodes[0].URL)
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
	if rows[0].Title != "Jag söker ett spel - RETRO-tråden" || rows[0].ID != "833642" || rows[0].Replies != 5546 || rows[0].Views != 554074 || rows[0].PageCount != 264 || !rows[0].Sticky {
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

func TestForumPageURLPreservesFlashbackForumSlug(t *testing.T) {
	if got := ForumPageURL("https://www.flashback.org/f445-cykel-60823", 2); got != "https://www.flashback.org/f445p2-cykel-60823" {
		t.Fatalf("forum pagination URL fel: %q", got)
	}
	if got := ForumPageURL("https://www.flashback.org/f445", 3); got != "https://www.flashback.org/f445p3" {
		t.Fatalf("forum pagination URL utan slug fel: %q", got)
	}
}

package flashback

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var numberPattern = regexp.MustCompile(`[0-9][0-9\s\x{00a0}.,]*`)

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// ParseNavigation only walks Flashback's forum-list cells and category
// containers. This is deliberate: last-post cells contain /f<ID>lp links to
// users, while the page also contains unrelated profile and thread links.
func ParseNavigation(html, sourceURL string) ([]ForumNode, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	var nodes []ForumNode
	seen := map[string]int{}
	order := 0
	add := func(a *goquery.Selection, parent string, depth int, hasChildren bool) {
		href, ok := a.Attr("href")
		if !ok || ClassifyURL(href, sourceURL) != LinkForum {
			return
		}
		title := clean(a.Text())
		if title == "" {
			return
		}
		full := NormalizeURL(href, sourceURL)
		id := ForumID(full)
		if i, ok := seen[full]; ok {
			if hasChildren {
				nodes[i].HasChildren = true
			}
			return
		}
		seen[full] = len(nodes)
		nodes = append(nodes, ForumNode{ID: id, Title: title, URL: full, ParentID: parent, Depth: depth, SortOrder: order, HasChildren: hasChildren, Browsable: true})
		order++
	}
	doc.Find("table.forumslist").Each(func(_ int, table *goquery.Selection) {
		parent := ""
		depth := 0
		if category := table.PrevAllFiltered("div.navbar-forum").First(); category.Length() > 0 {
			if a := category.Find("a.forum-title[href]").First(); a.Length() > 0 {
				add(a, "", 0, true)
				parent = ForumID(NormalizeURL(mustAttr(a, "href"), sourceURL))
				depth = 1
			}
		} else if breadcrumb := table.PrevAllFiltered("div.list-forum-title").First(); breadcrumb.Length() > 0 {
			if a := breadcrumb.Find("ol.breadcrumb a[href]").Last(); a.Length() > 0 {
				parent = ForumID(NormalizeURL(mustAttr(a, "href"), sourceURL))
				depth = 1
			}
		}
		table.Find("td.td_forum > a[href]").Each(func(_ int, a *goquery.Selection) { add(a, parent, depth, a.Find("table.forumslist").Length() > 0) })
	})
	return nodes, nil
}

func mustAttr(s *goquery.Selection, key string) string { v, _ := s.Attr(key); return v }

func ParseThreadListing(html, sourceURL, forumID string) ([]ThreadSummary, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	result := []ThreadSummary{}
	seen := map[string]bool{}
	doc.Find(".threads .thread, ul.threads > li, table.threads tr.thread").Each(func(_ int, row *goquery.Selection) {
		a := row.Find("a[href]").FilterFunction(func(_ int, s *goquery.Selection) bool {
			href, _ := s.Attr("href")
			return ClassifyURL(href, sourceURL) == LinkThread
		}).First()
		if a.Length() == 0 {
			return
		}
		href := mustAttr(a, "href")
		full := NormalizeURL(href, sourceURL)
		id := ThreadID(full)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		text := clean(row.Text())
		ts := ThreadSummary{ID: id, Title: clean(a.Text()), URL: full, ForumID: forumID, Author: clean(row.Find(".author, [data-author], .username").First().Text()), Replies: findCount(text, `(?i)([0-9][0-9\s\x{00a0}.,]*)\s*svar`), Views: findCount(text, `(?i)([0-9][0-9\s\x{00a0}.,]*)\s*visningar`), LastPostAuthor: clean(row.Find(".last-post-author, [data-last-author]").First().Text()), Sticky: strings.Contains(strings.ToLower(row.AttrOr("class", "")), "sticky") || strings.Contains(strings.ToLower(text), "klistrad")}
		if t, ok := row.Find("time[datetime], time").First().Attr("datetime"); ok {
			ts.LastPostAt = parseTime(t)
		}
		ts.PageCount = maxPage(row.Text())
		result = append(result, ts)
	})
	return result, nil
}

func findCount(text, pattern string) int {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	return parseNumber(m[1])
}
func parseNumber(s string) int {
	s = numberPattern.FindString(s)
	s = strings.NewReplacer(" ", "", "\u00a0", "", ".", "", ",", "").Replace(s)
	n, _ := strconv.Atoi(s)
	return n
}
func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "02.01.2006 15:04"} {
		if t, err := time.Parse(layout, strings.Replace(s, "Z", "+00:00", 1)); err == nil {
			return t
		}
	}
	return time.Time{}
}
func maxPage(s string) int {
	max := 0
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^/t[0-9]+p([0-9]+)`),
		regexp.MustCompile(`(?i)[?&/]p(?:age)?[=/]?([0-9]+)`),
	}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(m[1])
			if n > max {
				max = n
			}
		}
	}
	return max
}

func ParseSearchResults(html, sourceURL string) ([]SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	var result []SearchResult
	doc.Find("#posts > div.post.post-small").Each(func(_ int, post *goquery.Selection) {
		id := PostID(mustAttr(post.Find(".post-body a[href]").FilterFunction(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			return ClassifyURL(h, sourceURL) == LinkPost
		}).First(), "href"))
		if id == "" {
			id = postIDFromAttr(post.AttrOr("id", ""))
		}
		if id == "" {
			return
		}
		postLink := post.Find(".post-body a[href]").FilterFunction(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			return ClassifyURL(h, sourceURL) == LinkPost
		}).First()
		href := mustAttr(postLink, "href")
		if href == "" {
			href = "/p" + id
		}
		forum := post.Find(".post-heading a[href]").FilterFunction(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			return ClassifyURL(h, sourceURL) == LinkForum
		}).First()
		author := post.Find(".post-body a[href]").FilterFunction(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			return ClassifyURL(h, sourceURL) == LinkUser
		}).First()
		htmlText, _ := post.Html()
		result = append(result, SearchResult{ResultType: "post", PostID: id, ThreadID: ThreadIDFromHTML(htmlText), Title: clean(postLink.Text()), Author: clean(author.Text()), Forum: clean(forum.Text()), Snippet: clean(post.Find(".post_message").Text()), URL: NormalizeURL(href, sourceURL)})
	})
	return result, nil
}

func postIDFromAttr(s string) string {
	m := regexp.MustCompile(`(?i)post_?(\d+)`).FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
func ThreadIDFromHTML(s string) string {
	m := regexp.MustCompile(`(?i)/t(\d+)`).FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func ParseThreadPage(html, sourceURL, threadID string, page int) (ParsedPage, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ParsedPage{}, err
	}
	pageData := ParsedPage{ThreadID: threadID, Page: page, MaxPage: page, SourceURL: sourceURL, Title: clean(doc.Find("h1").First().Text())}
	doc.Find(".post").Each(func(i int, post *goquery.Selection) {
		id := postIDFromAttr(post.AttrOr("id", ""))
		if id == "" {
			return
		}
		author := clean(post.Find(".post-user-username a, .post-user-username").First().Text())
		msg := post.Find(".post_message").First()
		raw := clean(msg.Text())
		pageData.Posts = append(pageData.Posts, Post{ID: id, ThreadID: threadID, Author: author, Page: page, Position: i + 1, Text: raw, RawText: raw, SourceURL: sourceURL})
	})
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		h, _ := a.Attr("href")
		if ClassifyURL(h, sourceURL) == LinkThread {
			if n := ThreadID(NormalizeURL(h, sourceURL)); n == threadID {
				p := maxPage(h)
				if p > pageData.MaxPage {
					pageData.MaxPage = p
				}
			}
		}
	})
	return pageData, nil
}

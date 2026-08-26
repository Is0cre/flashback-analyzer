package flashback

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/backflash-cli/backflash/internal/diagnostics"
)

type Client struct {
	HTTP      *http.Client
	Session   SessionProvider
	BaseURL   string
	UserAgent string
}

func NewClient(session SessionProvider) *Client {
	if session == nil {
		session = AnonymousSession{}
	}
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Session: session, BaseURL: BaseURL, UserAgent: "BACKFLASH/0.1 (+https://github.com/backflash-cli/backflash)"}
}

func (c *Client) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	finish := diagnostics.Start("flashback.fetch")
	defer finish()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	for _, cookie := range c.Session.Cookies() {
		req.AddCookie(cookie)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Flashback svarade med HTTP %s", res.Status)
	}
	return io.ReadAll(res.Body)
}

func (c *Client) Forum(ctx context.Context, rawURL string) ([]ForumNode, error) {
	body, err := c.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return ParseNavigation(string(body), rawURL)
}

func (c *Client) Threads(ctx context.Context, forum ForumNode) ([]ThreadSummary, error) {
	return c.ThreadsPage(ctx, forum, 1)
}

// ThreadsPage follows Flashback's forum pagination format: /f<ID>p2 and,
// when a slug is present, /f<ID>p2-slug. The slug is retained because it is
// part of the normal browsable URL emitted by Flashback.
func (c *Client) ThreadsPage(ctx context.Context, forum ForumNode, page int) ([]ThreadSummary, error) {
	rawURL := forum.URL
	if page > 1 {
		rawURL = ForumPageURL(rawURL, page)
	}
	body, err := c.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return ParseThreadListing(string(body), rawURL, forum.ID)
}

var forumPathPattern = regexp.MustCompile(`(?i)(/f[0-9]+)(p[0-9]+)?(-[^/?#]*)?$`)

func ForumPageURL(rawURL string, page int) string {
	if page <= 1 {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	m := forumPathPattern.FindStringSubmatch(u.Path)
	if len(m) != 4 {
		return rawURL
	}
	u.Path = m[1] + "p" + strconv.Itoa(page) + m[3]
	return u.String()
}

// ForumPage parses one fetched forum page in both semantic contexts. Forum
// pages may contain subforums, threads, or both; sharing the response avoids
// two identical network requests when a cached node is incomplete.
func (c *Client) ForumPage(ctx context.Context, forum ForumNode) ([]ForumNode, []ThreadSummary, error) {
	body, err := c.Fetch(ctx, forum.URL)
	if err != nil {
		return nil, nil, err
	}
	forums, err := ParseNavigation(string(body), forum.URL)
	if err != nil {
		return nil, nil, err
	}
	threads, err := ParseThreadListing(string(body), forum.URL, forum.ID)
	if err != nil {
		return nil, nil, err
	}
	return forums, threads, nil
}

func (c *Client) Search(ctx context.Context, query string, page int) ([]SearchResult, error) {
	u, _ := url.Parse(c.BaseURL + "sok/")
	q := u.Query()
	q.Set("so", "pd")
	q.Set("query", query)
	q.Set("sp", "1")
	q.Set("search_post", "1")
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	u.RawQuery = q.Encode()
	body, err := c.Fetch(ctx, u.String())
	if err != nil {
		return nil, err
	}
	return ParseSearchResults(string(body), u.String())
}

func (c *Client) Thread(ctx context.Context, threadID string, page int) (ParsedPage, error) {
	path := c.BaseURL + "t" + threadID
	if page > 1 {
		path += "p" + strconv.Itoa(page)
	}
	body, err := c.Fetch(ctx, path)
	if err != nil {
		return ParsedPage{}, err
	}
	return ParseThreadPage(string(body), path, threadID, page)
}

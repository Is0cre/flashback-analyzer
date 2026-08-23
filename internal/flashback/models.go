package flashback

import "time"

type LinkType int

const (
	LinkOther LinkType = iota
	LinkForum
	LinkThread
	LinkPost
	LinkUser
)

type ForumNode struct {
	ID         string
	Title      string
	URL        string
	ParentID   string
	Depth      int
	SortOrder  int
	HasChildren bool
	Browsable  bool
}

type ThreadSummary struct {
	ID             string
	Title          string
	URL            string
	ForumID        string
	Author         string
	Replies        int
	Views          int
	LastPostAt     time.Time
	LastPostAuthor string
	Sticky         bool
	PageCount      int
}

type SearchResult struct {
	ResultType string
	ThreadID   string
	PostID     string
	Title      string
	Author     string
	Forum      string
	Timestamp  time.Time
	Snippet    string
	URL        string
}

type Quote struct {
	PostID   string
	Author   string
	Text     string
}

type Post struct {
	ID         string
	ThreadID   string
	Author     string
	Timestamp  time.Time
	Page       int
	Position   int
	Text       string
	RawText    string
	SourceURL  string
	Quotes     []Quote
}

type ParsedPage struct {
	ThreadID string
	Title    string
	Page     int
	MaxPage  int
	SourceURL string
	Posts    []Post
}

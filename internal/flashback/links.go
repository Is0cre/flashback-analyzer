package flashback

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	forumPath = regexp.MustCompile(`(?i)^/f([0-9]+)(-[^/]*)?$`)
	// Flashback uses the same thread route for the normal title, page links,
	// the thread status link (n) and the latest-post shortcut (s/lp).
	threadPath = regexp.MustCompile(`(?i)^/t([0-9]+)(p[0-9]+|n|s|lp)?(-[^/]*)?$`)
	postPath   = regexp.MustCompile(`(?i)^/p([0-9]+)$`)
	userPath   = regexp.MustCompile(`(?i)^/u[0-9]+$`)
)

const BaseURL = "https://www.flashback.org/"

func NormalizeURL(href, base string) string {
	if base == "" {
		base = BaseURL
	}
	resolved, err := url.Parse(base)
	if err != nil {
		return strings.TrimSpace(href)
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	resolved = resolved.ResolveReference(ref)
	resolved.Scheme = strings.ToLower(resolved.Scheme)
	resolved.Host = strings.ToLower(resolved.Host)
	resolved.Fragment = ""
	if strings.HasSuffix(strings.ToLower(resolved.Host), "flashback.org") {
		resolved.RawQuery = ""
	}
	if resolved.Path != "/" {
		resolved.Path = strings.TrimRight(resolved.Path, "/")
	}
	return resolved.String()
}

func ClassifyURL(href, base string) LinkType {
	u, err := url.Parse(NormalizeURL(href, base))
	if err != nil {
		return LinkOther
	}
	path := u.Path
	if forumPath.MatchString(path) || u.Query().Get("f") != "" {
		return LinkForum
	}
	if strings.HasSuffix(strings.ToLower(path), "lp") && strings.HasPrefix(strings.ToLower(path), "/f") || userPath.MatchString(path) {
		return LinkUser
	}
	if threadPath.MatchString(path) {
		return LinkThread
	}
	if postPath.MatchString(path) || strings.HasSuffix(path, "/showpost.php") && u.Query().Get("p") != "" {
		return LinkPost
	}
	return LinkOther
}

func ForumID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if m := forumPath.FindStringSubmatch(u.Path); len(m) >= 2 {
		return m[1]
	}
	return u.Query().Get("f")
}

func ThreadID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if m := threadPath.FindStringSubmatch(u.Path); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func PostID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if m := postPath.FindStringSubmatch(u.Path); len(m) == 2 {
		return m[1]
	}
	return ""
}

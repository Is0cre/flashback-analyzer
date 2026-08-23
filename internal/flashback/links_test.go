package flashback

import "testing"

func TestClassifyFlashbackURLs(t *testing.T) {
	cases := map[string]LinkType{
		"/f21-linux-60823": LinkForum,
		"/f21lp": LinkUser,
		"/t7001": LinkThread,
		"/t7001p2": LinkThread,
		"/p9001#p9001": LinkPost,
		"/u42": LinkUser,
		"/sok/?query=linux": LinkOther,
	}
	for href, want := range cases { if got := ClassifyURL(href, BaseURL); got != want { t.Errorf("ClassifyURL(%q) = %v, want %v", href, got, want) } }
}

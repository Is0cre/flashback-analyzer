package flashback

import "net/http"

type SessionProvider interface {
	Cookies() []*http.Cookie
	Authenticated() bool
}

type AnonymousSession struct{}

func (AnonymousSession) Cookies() []*http.Cookie { return nil }
func (AnonymousSession) Authenticated() bool     { return false }

type CookieSession struct{ Values []*http.Cookie }

func (s CookieSession) Cookies() []*http.Cookie { return s.Values }
func (s CookieSession) Authenticated() bool     { return len(s.Values) > 0 }

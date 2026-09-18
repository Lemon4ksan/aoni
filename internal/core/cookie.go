package core

import (
	"context"
	"net/http"
	"net/url"
)

// CookieJar defines a context-aware HTTP cookie storage interface supporting RFC 6265bis CHIPS.
//
// Unlike the standard [net/http/cookiejar.Jar], this interface accepts a [context.Context]
// to extract proxy and partition keys for strict cookie isolation.
type CookieJar interface {
	// SetCookies handles the receipt of the cookies in a reply for the
	// given URL. It may or may not choose to save the cookies, depending
	// on the jar's policy and implementation.
	SetCookies(ctx context.Context, u *url.URL, cookies []*http.Cookie)

	// Cookies returns the cookies to send in a request for the given URL.
	// It is up to the implementation to honor the standard cookie use
	// restrictions such as in RFC 6265.
	Cookies(ctx context.Context, u *url.URL) []*http.Cookie
}

package cookie

import (
	"net/http"

	"github.com/lemon4ksan/foundation/net/http/header"
)

// Transport intercepts HTTP responses to extract and store Set-Cookie headers,
// and injects active cookies from the CookieJar into outbound requests.
//
// Designed to sit below telemetry and retry layers, but above raw connection pooling.
type Transport struct {
	Next      http.RoundTripper
	CookieJar Jar
}

// RoundTrip executes a single HTTP transaction, applying and harvesting cookies.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.CookieJar != nil && req.URL != nil {
		jar := t.CookieJar
		if jar != nil {
			cookies := jar.Cookies(req.Context(), req.URL)
			if len(cookies) > 0 {
				cookieHeader := BuildCookieHeader(cookies)
				if cookieHeader != "" {
					if existing := req.Header.Get(header.Cookie); existing != "" {
						req.Header.Set(header.Cookie, existing+"; "+cookieHeader)
					} else {
						req.Header.Set(header.Cookie, cookieHeader)
					}
				}
			}
		}
	}

	resp, err := t.Next.RoundTrip(req)

	reqToPass := req
	if resp != nil && resp.Request != nil {
		reqToPass = resp.Request
	}

	if err != nil || resp == nil || t.CookieJar == nil || reqToPass.URL == nil {
		return resp, err
	}

	jar := t.CookieJar
	if jar != nil {
		cookies := resp.Cookies()
		if len(cookies) > 0 {
			jar.SetCookies(reqToPass.Context(), reqToPass.URL, cookies)
		}
	}

	return resp, err
}

// Clone creates a shallow copy of the transport.
func (t *Transport) Clone() *Transport {
	return &Transport{
		Next:      t.Next,
		CookieJar: t.CookieJar,
	}
}

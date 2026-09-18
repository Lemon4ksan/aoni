package cookie

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/psl"
	"github.com/lemon4ksan/foundation/silicon/clock"
)

// MemoryJar is a fully RFC 6265 and RFC 6265bis (CHIPS) compliant in-memory cookie jar.
// It implements the context-aware [cookie.core.CookieJar] interface to natively support Partitioned cookies.
type MemoryJar struct {
	cookies generic.Safe[map[cookieKey]Cookie]
}

// NewMemoryJar creates a new [MemoryJar].
func NewMemoryJar() *MemoryJar {
	return &MemoryJar{
		cookies: *generic.NewSafe(make(map[cookieKey]Cookie)),
	}
}

// SetCookies handles the receipt of the cookies in a reply for the given URL.
func (mj *MemoryJar) SetCookies(ctx context.Context, u *url.URL, cookies []*http.Cookie) {
	now := clock.CoarseTime()
	partitionKey := GetPartitionKey(ctx)

	mj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		for _, c := range cookies {
			domain := c.Domain
			if domain == "" {
				domain = u.Hostname()
			}
			domain = strings.ToLower(domain)

			// If cookie explicitly specified a domain, check public suffix
			if c.Domain != "" {
				if suffix := psl.List.PublicSuffix(domain); suffix == domain {
					continue // RFC 6265 §5.3 step 5
				}
				if !DomainMatch(u.Hostname(), domain) {
					continue // RFC 6265 §5.3 step 6
				}
			}

			path := generic.Coalesce(c.Path, "/")

			// CHIPS: If not partitioned, store under empty partition key so it's shared.
			pk := generic.Ternary(c.Partitioned, partitionKey, "")

			key := cookieKey{domain: domain, path: path, name: c.Name, partitionKey: pk}

			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				delete(*m, key)
				continue
			}

			parsed := FromStd(c, domain, path)
			parsed.PartitionKey = pk
			(*m)[key] = parsed
		}

		purgeExpiredCookies(*m, now)
	})
}

// Cookies returns the cookies to send in a request for the given URL.
func (mj *MemoryJar) Cookies(ctx context.Context, u *url.URL) []*http.Cookie {
	now := clock.CoarseTime()
	partitionKey := GetPartitionKey(ctx)
	reqHost := u.Hostname()
	reqPath := generic.Coalesce(u.Path, "/")
	isSecure := u.Scheme == "https"

	var validCookies []*http.Cookie

	mj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		for k, c := range *m {
			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				delete(*m, k)
				continue
			}

			if c.PartitionKey != "" && c.PartitionKey != partitionKey {
				continue
			}

			if !DomainMatch(reqHost, c.Domain) {
				continue
			}

			if !PathMatch(reqPath, c.Path) {
				continue
			}

			if c.Secure && !isSecure {
				continue
			}

			validCookies = append(validCookies, toStd(c))
		}
	})

	SortForBrowser(validCookies)
	return validCookies
}

// toStd converts a Cookie DTO back to a standard library *http.Cookie
func toStd(c Cookie) *http.Cookie {
	var sameSite http.SameSite
	switch c.SameSite {
	case "Lax":
		sameSite = http.SameSiteLaxMode
	case "Strict":
		sameSite = http.SameSiteStrictMode
	case "None":
		sameSite = http.SameSiteNoneMode
	}

	return &http.Cookie{
		Name:        c.Name,
		Value:       c.Value,
		Domain:      c.Domain,
		Path:        c.Path,
		Expires:     c.Expires,
		HttpOnly:    c.HTTPOnly,
		Secure:      c.Secure,
		SameSite:    sameSite,
		Partitioned: c.Partitioned,
		MaxAge:      c.MaxAge,
	}
}

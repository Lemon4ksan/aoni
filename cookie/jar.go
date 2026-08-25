// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	asyncctx "github.com/lemon4ksan/foundation/async/context"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/psl"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/clock"
)

type (
	proxyCtxKey     struct{}
	partitionCtxKey struct{}
)

// WithProxyAddress returns a new Context carrying the active proxy URL string for cookie jar partitioning.
// Yields a child context containing proxyCtxKey with value addr.
func WithProxyAddress(ctx context.Context, addr string) context.Context {
	return asyncctx.WithValue(ctx, proxyCtxKey{}, addr)
}

// GetProxyAddress retrieves the active proxy URL string stored in the context.
// Returns the proxy URL string if present; otherwise returns an empty string.
func GetProxyAddress(ctx context.Context) string {
	return asyncctx.GetOr(ctx, proxyCtxKey{}, "")
}

// WithPartitionKey returns a Context carrying a CHIPS (RFC 6265bis) top-level site partition key.
//
// Specification Adherence:
// Conforms to RFC 6265bis CHIPS (Cookies Having Independent Partitioned State) specification.
func WithPartitionKey(ctx context.Context, key string) context.Context {
	return asyncctx.WithValue(ctx, partitionCtxKey{}, key)
}

// GetPartitionKey retrieves the active CHIPS top-level site partition key from context.
func GetPartitionKey(ctx context.Context) string {
	return asyncctx.GetOr(ctx, partitionCtxKey{}, "")
}

type cookieKey struct {
	domain       string
	path         string
	name         string
	partitionKey string
}

// ProxyIsolatedJar provides thread-safe, per-proxy and CHIPS partitioned cookie storage isolation.
//
// Specification Adherence:
// Implements RFC 6265 cookie isolation augmented with per-proxy session segregation and RFC 6265bis CHIPS partitioning.
//
// Thread Safety & Concurrency:
// 100% thread-safe for concurrent read and write operations via atomic [generic.ConcurrentMap].
type ProxyIsolatedJar struct {
	jars    generic.ConcurrentMap[string, http.CookieJar]
	backend generic.Safe[Storage]
}

// NewProxyIsolatedJar creates a new, thread-safe [ProxyIsolatedJar] ready for concurrent request execution.
func NewProxyIsolatedJar() *ProxyIsolatedJar {
	return &ProxyIsolatedJar{}
}

// SetCookies satisfies the standard [http.CookieJar] interface.
// Delegates to the default (unproxied) internal jar when invoked without a proxy-aware context.
func (p *ProxyIsolatedJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if jar := p.GetJarForProxy(""); jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies satisfies the standard [http.CookieJar] interface.
// Returns cookies matching destination u from the default (unproxied) internal jar when context is absent.
func (p *ProxyIsolatedJar) Cookies(u *url.URL) []*http.Cookie {
	if jar := p.GetJarForProxy(""); jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJarForProxy retrieves or lazily initializes an isolated [http.CookieJar] bound to the specified proxyURL.
// Thread-safe and lock-free on cache hits via atomic [generic.ConcurrentMap].
func (p *ProxyIsolatedJar) GetJarForProxy(proxyURL string) http.CookieJar {
	if jar, ok := p.jars.Load(proxyURL); ok {
		return jar
	}

	baseJar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: psl.List,
	})
	if err != nil {
		return nil
	}

	var jar http.CookieJar = baseJar

	backend := p.backend.Get()

	if backend != nil {
		jar = p.initPersistentJar(proxyURL, baseJar, backend)
	}

	actual, _ := p.jars.LoadOrStore(proxyURL, jar)

	return actual
}

// WithStorageBackend configures persistent storage for cookie jars and returns p.
func (p *ProxyIsolatedJar) WithStorageBackend(backend Storage) *ProxyIsolatedJar {
	p.backend.Set(backend)
	return p
}

// CookiesForProxy retrieves cookies associated with a specific proxy URL and target URL.
func (p *ProxyIsolatedJar) CookiesForProxy(proxyURL string, u *url.URL) []*http.Cookie {
	if jar := p.GetJarForProxy(proxyURL); jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// SetCookiesForProxy manually stores cookies for a specific proxy URL and target URL.
func (p *ProxyIsolatedJar) SetCookiesForProxy(proxyURL string, u *url.URL, cookies []*http.Cookie) {
	if jar := p.GetJarForProxy(proxyURL); jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// GetJar extracts the active proxy URL from context and yields the corresponding isolated jar.
func (p *ProxyIsolatedJar) GetJar(ctx context.Context) http.CookieJar {
	return p.GetJarForProxy(GetProxyAddress(ctx))
}

// StartJanitor launches a background goroutine that periodically purges expired cookies across all proxy jars.
func (p *ProxyIsolatedJar) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.PurgeExpired()
			}
		}
	}()
}

// PurgeExpired removes all expired cookies across all active proxy jars without holding global locks during backend I/O.
func (p *ProxyIsolatedJar) PurgeExpired() {
	p.jars.Range(func(_ string, jar http.CookieJar) bool {
		if pJar, ok := jar.(*PersistentJar); ok {
			pJar.purgeExpired()
		}

		return true
	})
}

func (p *ProxyIsolatedJar) initPersistentJar(proxyURL string, baseJar http.CookieJar, backend Storage) http.CookieJar {
	initialMap := make(map[cookieKey]Cookie)
	pJar := &PersistentJar{
		CookieJar: baseJar,
		proxyURL:  proxyURL,
		backend:   backend,
		cookies:   *generic.NewSafe(initialMap),
	}

	cookies, err := backend.Load(proxyURL)
	if err != nil || len(cookies) == 0 {
		return pJar
	}

	pJar.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		for _, c := range cookies {
			key := cookieKey{domain: c.Domain, path: c.Path, name: c.Name, partitionKey: c.PartitionKey}
			(*m)[key] = c

			scheme := generic.Ternary(c.Secure, "https", "http")
			domain := strings.TrimPrefix(c.Domain, ".")

			u, parseErr := url.Parse(scheme + "://" + domain + c.Path)
			if parseErr == nil {
				baseJar.SetCookies(u, []*http.Cookie{
					{ //nolint:gosec
						Name:     c.Name,
						Value:    c.Value,
						Domain:   c.Domain,
						Path:     c.Path,
						Expires:  c.Expires,
						HttpOnly: c.HTTPOnly,
						Secure:   c.Secure,
					},
				})
			}
		}
	})

	return pJar
}

// PersistentJar decorates an [http.CookieJar] to synchronize updates to a Storage backend and enforce CHIPS partitioning.
type PersistentJar struct {
	http.CookieJar
	proxyURL string
	backend  Storage
	cookies  generic.Safe[map[cookieKey]Cookie]
}

func isExpiredCookie(expires time.Time, maxAge int, now time.Time) bool {
	return (!expires.IsZero() && expires.Before(now)) || maxAge < 0
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimPrefix(domain, "."))
}

func deleteMatchingCookie(m map[cookieKey]Cookie, name, domain string) bool {
	normDomain := strings.TrimPrefix(domain, ".")
	deleted := false

	for k := range m {
		if k.name == name && bytesconv.EqualFoldASCII(strings.TrimPrefix(k.domain, "."), normDomain) {
			delete(m, k)

			deleted = true
		}
	}

	return deleted
}

func purgeExpiredCookies(m map[cookieKey]Cookie, now time.Time) bool {
	changed := false

	for k, c := range m {
		if isExpiredCookie(c.Expires, c.MaxAge, now) {
			delete(m, k)

			changed = true
		}
	}

	return changed
}

// Cookies returns non-expired cookies matching the target URL and partition key.
func (pj *PersistentJar) Cookies(u *url.URL) []*http.Cookie {
	cookies := pj.CookieJar.Cookies(u)
	if len(cookies) == 0 {
		return nil
	}

	now := clock.CoarseTime()
	validCookies := make([]*http.Cookie, 0, len(cookies))

	var flushList []Cookie

	pj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		hasExpired := false

		for _, c := range cookies {
			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				hasExpired = deleteMatchingCookie(*m, c.Name, c.Domain) || hasExpired
				continue
			}

			validCookies = append(validCookies, c)
		}

		if hasExpired && pj.backend != nil {
			flushList = generic.Values(*m)
		}
	})

	if len(flushList) > 0 && pj.backend != nil {
		_ = pj.backend.Save(pj.proxyURL, flushList)
	}

	return validCookies
}

func (pj *PersistentJar) purgeExpired() {
	now := clock.CoarseTime()

	var flushList []Cookie

	pj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		if purgeExpiredCookies(*m, now) && pj.backend != nil {
			flushList = generic.Values(*m)
		}
	})

	if len(flushList) > 0 && pj.backend != nil {
		_ = pj.backend.Save(pj.proxyURL, flushList)
	}
}

// SetCookies stores cookies in the inner jar and flushes non-expired cookies to persistent storage.
func (pj *PersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	pj.CookieJar.SetCookies(u, cookies)

	now := clock.CoarseTime()

	var flushList []Cookie

	pj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		changed := false

		for _, c := range cookies {
			domain := strings.ToLower(generic.Coalesce(c.Domain, u.Hostname()))
			path := generic.Coalesce(c.Path, "/")
			key := cookieKey{domain: domain, path: path, name: c.Name}

			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				changed = deleteMatchingCookie(*m, c.Name, domain) || changed
				continue
			}

			(*m)[key] = FromStd(c, domain, path)
			changed = true
		}

		if purgeExpiredCookies(*m, now) {
			changed = true
		}

		if changed && pj.backend != nil {
			flushList = generic.Values(*m)
		}
	})

	if len(flushList) > 0 && pj.backend != nil {
		_ = pj.backend.Save(pj.proxyURL, flushList)
	}
}

// FindCookie searches for a cookie by name for a given URL and reports whether it was found.
func (p *ProxyIsolatedJar) FindCookie(u *url.URL, name string) (*http.Cookie, bool) {
	if p == nil || u == nil {
		return nil, false
	}

	return generic.Find(p.Cookies(u), func(c *http.Cookie) bool {
		return c != nil && c.Name == name
	})
}

// FindCookieOptional searches for a cookie by name for a given URL and returns it wrapped in a [generic.Optional].
func (p *ProxyIsolatedJar) FindCookieOptional(u *url.URL, name string) generic.Optional[*http.Cookie] {
	if c, ok := p.FindCookie(u, name); ok {
		return generic.Some(c)
	}

	return generic.None[*http.Cookie]()
}

// GetCookieValue retrieves the value of a named cookie.
func (p *ProxyIsolatedJar) GetCookieValue(u *url.URL, name string) (string, bool) {
	if c, ok := p.FindCookie(u, name); ok && c != nil {
		return c.Value, true
	}

	return "", false
}

// GetCookieValueOptional retrieves the value of a named cookie as a [generic.Optional].
func (p *ProxyIsolatedJar) GetCookieValueOptional(u *url.URL, name string) generic.Optional[string] {
	if val, ok := p.GetCookieValue(u, name); ok {
		return generic.Some(val)
	}

	return generic.None[string]()
}

// HasCookies reports whether the jar stores any active cookies for URL u.
func (p *ProxyIsolatedJar) HasCookies(u *url.URL) bool {
	return p != nil && len(p.Cookies(u)) > 0
}

package cookie

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/async/ctxkit"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/lemon4ksan/foundation/net/http/nik"
)

type (
	proxyCtxKey     struct{}
	partitionCtxKey struct{}
)

// WithProxyAddress returns a new Context carrying the active proxy URL string for cookie jar partitioning.
func WithProxyAddress(ctx context.Context, addr string) context.Context {
	return ctxkit.WithValue(ctx, proxyCtxKey{}, addr)
}

// GetProxyAddress retrieves the active proxy URL string stored in the context.
func GetProxyAddress(ctx context.Context) string {
	return ctxkit.GetOr(ctx, proxyCtxKey{}, "")
}

// WithPartitionKey returns a Context carrying a CHIPS (RFC 6265bis) top-level site partition key.
func WithPartitionKey(ctx context.Context, key string) context.Context {
	return ctxkit.WithValue(ctx, partitionCtxKey{}, key)
}

// GetPartitionKey retrieves the active CHIPS top-level site partition key or Network Isolation Key from context.
func GetPartitionKey(ctx context.Context) string {
	if k := ctxkit.GetOr(ctx, partitionCtxKey{}, ""); k != "" {
		return k
	}

	if nikKey, ok := nik.FromContext(ctx); ok {
		return nikKey.KeyString()
	}

	return ""
}

type cookieKey struct {
	domain       string
	path         string
	name         string
	partitionKey string
}

// ProxyIsolatedJar provides thread-safe, per-proxy and CHIPS partitioned cookie storage isolation.
type ProxyIsolatedJar struct {
	jars    generic.ConcurrentMap[string, Jar]
	backend generic.Safe[Storage]
}

// NewProxyIsolatedJar creates a new, thread-safe [ProxyIsolatedJar].
func NewProxyIsolatedJar() *ProxyIsolatedJar {
	return &ProxyIsolatedJar{}
}

// SetCookies satisfies the context-aware [cookie.Jar] interface.
func (p *ProxyIsolatedJar) SetCookies(ctx context.Context, u *url.URL, cookies []*http.Cookie) {
	if jar := p.GetJar(ctx); jar != nil {
		jar.SetCookies(ctx, u, cookies)
	}
}

// Cookies satisfies the context-aware [cookie.Jar] interface.
func (p *ProxyIsolatedJar) Cookies(ctx context.Context, u *url.URL) []*http.Cookie {
	if jar := p.GetJar(ctx); jar != nil {
		return jar.Cookies(ctx, u)
	}
	return nil
}

// GetJarForProxy retrieves or lazily initializes an isolated [Jar] bound to the specified proxyURL.
func (p *ProxyIsolatedJar) GetJarForProxy(proxyURL string) Jar {
	if jar, ok := p.jars.Load(proxyURL); ok {
		return jar
	}

	baseJar := NewMemoryJar()

	var jar Jar = baseJar

	backend := p.backend.Get()

	if backend != nil {
		jar = p.initPersistentJar(proxyURL, baseJar, backend)
	}

	actual, _ := p.jars.LoadOrStore(proxyURL, jar)

	return actual
}

// WithStorageBackend configures a persistent storage backend.
func (p *ProxyIsolatedJar) WithStorageBackend(backend Storage) *ProxyIsolatedJar {
	p.backend.Set(backend)
	return p
}

// GetJar extracts the active proxy URL from context and yields the corresponding isolated jar.
func (p *ProxyIsolatedJar) GetJar(ctx context.Context) Jar {
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

// PurgeExpired removes all expired cookies.
func (p *ProxyIsolatedJar) PurgeExpired() {
	p.jars.Range(func(_ string, jar Jar) bool {
		if pJar, ok := jar.(*PersistentJar); ok {
			pJar.purgeExpired()
		}
		return true
	})
}

func (p *ProxyIsolatedJar) initPersistentJar(proxyURL string, baseJar Jar, backend Storage) Jar {
	initialMap := make(map[cookieKey]Cookie)
	pJar := &PersistentJar{
		Inner:     baseJar,
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
				// Initialize inner jar without overwriting partition keys
				stdCookie := &http.Cookie{ //nolint:gosec
					Name:     c.Name,
					Value:    c.Value,
					Domain:   c.Domain,
					Path:     c.Path,
					Expires:  c.Expires,
					HttpOnly: c.HTTPOnly,
					Secure:   c.Secure,
					Partitioned: c.Partitioned,
				}
				// We need a context with the specific partition key to feed the inner jar
				ctx := WithPartitionKey(context.Background(), c.PartitionKey)
				baseJar.SetCookies(ctx, u, []*http.Cookie{stdCookie})
			}
		}
	})

	return pJar
}

// PersistentJar decorates a [Jar] to synchronize updates to a Storage backend and enforce CHIPS partitioning.
type PersistentJar struct {
	Inner    Jar
	proxyURL string
	backend  Storage
	cookies  generic.Safe[map[cookieKey]Cookie]
}

func isExpiredCookie(expires time.Time, maxAge int, now time.Time) bool {
	return (!expires.IsZero() && expires.Before(now)) || maxAge < 0
}

func deleteMatchingCookie(m map[cookieKey]Cookie, name, domain, partitionKey string) bool {
	normDomain := strings.TrimPrefix(domain, ".")
	deleted := false

	for k := range m {
		if k.name == name && k.partitionKey == partitionKey && bytesconv.EqualFoldASCII(strings.TrimPrefix(k.domain, "."), normDomain) {
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
func (pj *PersistentJar) Cookies(ctx context.Context, u *url.URL) []*http.Cookie {
	cookies := pj.Inner.Cookies(ctx, u)
	if len(cookies) == 0 {
		return nil
	}
	
	partitionKey := GetPartitionKey(ctx)
	now := clock.CoarseTime()
	validCookies := make([]*http.Cookie, 0, len(cookies))

	var flushList []Cookie

	pj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		hasExpired := false

		for _, c := range cookies {
			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				hasExpired = deleteMatchingCookie(*m, c.Name, c.Domain, partitionKey) || hasExpired
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
func (pj *PersistentJar) SetCookies(ctx context.Context, u *url.URL, cookies []*http.Cookie) {
	pj.Inner.SetCookies(ctx, u, cookies)

	now := clock.CoarseTime()
	partitionKey := GetPartitionKey(ctx)

	var flushList []Cookie

	pj.cookies.Mutate(func(m *map[cookieKey]Cookie) {
		changed := false

		for _, c := range cookies {
			domain := strings.ToLower(generic.Coalesce(c.Domain, u.Hostname()))
			path := generic.Coalesce(c.Path, "/")
			
			pk := ""
			if c.Partitioned {
				pk = partitionKey
			}
			
			key := cookieKey{domain: domain, path: path, name: c.Name, partitionKey: pk}

			if isExpiredCookie(c.Expires, c.MaxAge, now) {
				changed = deleteMatchingCookie(*m, c.Name, domain, pk) || changed
				continue
			}

			parsed := FromStd(c, domain, path)
			parsed.PartitionKey = pk
			(*m)[key] = parsed
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

// Finder defines a capability interface for cookie jars that support direct named cookie lookups.
type Finder interface {
	FindCookie(ctx context.Context, u *url.URL, name string) (*http.Cookie, bool)
}

var _ Finder = (*ProxyIsolatedJar)(nil)

// FindCookie searches for a cookie by name for a given URL and reports whether it was found.
func (p *ProxyIsolatedJar) FindCookie(ctx context.Context, u *url.URL, name string) (*http.Cookie, bool) {
	if p == nil || u == nil {
		return nil, false
	}

	return generic.Find(p.Cookies(ctx, u), func(c *http.Cookie) bool {
		return c != nil && c.Name == name
	})
}

// GetCookieValue retrieves the value of a named cookie.
func (p *ProxyIsolatedJar) GetCookieValue(ctx context.Context, u *url.URL, name string) (string, bool) {
	if c, ok := p.FindCookie(ctx, u, name); ok && c != nil {
		return c.Value, true
	}

	return "", false
}

// HasCookies reports whether the jar stores any active cookies for URL u.
func (p *ProxyIsolatedJar) HasCookies(ctx context.Context, u *url.URL) bool {
	return p != nil && len(p.Cookies(ctx, u)) > 0
}

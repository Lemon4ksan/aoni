// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
)

// DefaultUserAgent is the default User-Agent string used for HTTP requests.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	bytePool = sync.Pool{
		New: func() any {
			b := make([]byte, 32*1024)
			return &b
		},
	}
	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

type (
	capturerCtxKey           struct{}
	decoderCtxKey            struct{}
	errorModelCtxKey         struct{}
	downloadProgressCtxKey   struct{}
	hedgingCtxKey            struct{}
	queryErrorCtxKey         struct{}
	bodyErrorCtxKey          struct{}
	happyEyeballsDelayCtxKey struct{}
	multiReadCtxKey          struct{}
	ssrfGuardCtxKey          struct{}
	fallbackCtxKey           struct{}
	debugCtxKey              struct{}
	orderedHeadersCtxKey     struct{}
	ja4ReportCtxKey          struct{}
	ja4CallbackCtxKey        struct{}
	alpnOverrideCtxKey       struct{}
	p0fSignatureCtxKey       struct{}
)

// DefaultSensitiveHeaders is the list of sensitive headers that are scrubbed from requests on redirect.
var DefaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// Unwrapper is an interface for objects that can be unwrapped to reveal their underlying [Requester] implementation.
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient recursively unwraps any decorator or wrapper layers from the Requester interface,
// reaching down to the original concrete structure *Client.
// Returns nil if the passed object is a clean test mock.
func UnwrapClient(r Requester) *Client {
	for {
		if client, ok := r.(*Client); ok {
			return client
		}

		u, ok := r.(Unwrapper)
		if !ok {
			break
		}

		r = u.Unwrap()
	}

	return nil
}

// HTTPDoer defines the interface for objects executing an [http.Request].
// It is implemented by [http.Client] and can be customized via [DoerFunc].
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc is an adapter allowing ordinary functions to act as an [HTTPDoer].
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the HTTP request by calling the underlying function.
// It returns an [http.Response] or an error if the request fails.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Requester defines the contract for executing raw HTTP requests.
// It supports relative paths joining, query encoding, and custom modifiers.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider defines an optional interface for a [Requester]
// to provide a [BaseResponse] wrapper for structured decoding.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// ProgressFunc defines the callback signature for tracking transfer progress.
// The total parameter represents the content length, or -1 if unknown.
type ProgressFunc func(current, total int64)

// BaseResponse defines the contract for structured response wrappers.
// It handles success status checking and decoding destination binding.
// It is passed to the json decoder to unmarshal the response body into the target data structure.
// Implement UnmarshalJSON to handle the actual JSON decoding logic.
type BaseResponse interface {
	// IsSuccess reports whether the response indicates a successful operation.
	IsSuccess() bool
	// Error returns an error representation if IsSuccess returns false.
	Error() error
	// SetData sets the data into the response.
	SetData(data any)
}

// DefaultRedirectPolicy returns a redirect policy function that stops after maxRedirects,
// and scrubs sensitiveHeaders from the request. If sensitiveHeaders is empty, [DefaultSensitiveHeaders] are used.
func DefaultRedirectPolicy(
	maxRedirects int,
	sensitiveHeaders ...string,
) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if maxRedirects >= 0 && len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		if len(via) == 0 {
			return nil
		}

		if len(sensitiveHeaders) == 0 {
			sensitiveHeaders = DefaultSensitiveHeaders
		}

		if isCrossOrigin(req.URL, via[0].URL) {
			for _, h := range sensitiveHeaders {
				req.Header.Del(h)
			}
		}

		return nil
	}
}

// Client is a thread-safe, immutable HTTP client implementing [Requester].
// It manages base URLs, default headers, and custom transport options.
// Use [NewClient] to initialize a new instance.
type Client struct {
	http               HTTPDoer
	baseURL            *url.URL
	headers            http.Header
	baseResponse       func() BaseResponse
	hedgingDelay       time.Duration
	beforeRequest      []func(req *http.Request)
	afterResponse      []func(resp *http.Response, err error)
	maxResponseSize    int64
	ssrfGuard          bool
	happyEyeballsDelay time.Duration
	multiReadThreshold int64
	logger             Logger

	sourceRotator    *SourceIPRotator
	dnsResolver      DNSResolver
	defaultMods      []RequestModifier
	headersCookieJar http.CookieJar
	ja4Callback      func(ja4.Report)
	tlsBrowserID     BrowserID
	fragmentConfig   *FragmentConfig
	hostRewrite      *HostRewriteConfig
	p0fSignature     *p0f.Signature
	h2Settings       *HTTP2Settings
}

// NewClient initializes a new [Client] instance with [DefaultUserAgent].
// If the provided httpClient is nil, a default [http.Client] is used.
// The default client has a 15-second timeout and scrubs sensitive
// headers on redirect using [DefaultRedirectPolicy].
func NewClient(httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: DefaultRedirectPolicy(10),
		}
	}

	c := &Client{
		http:               httpClient,
		baseURL:            &url.URL{},
		headers:            make(http.Header),
		maxResponseSize:    10 * 1024 * 1024,
		happyEyeballsDelay: 300 * time.Millisecond,
	}

	c.applyDialers()

	return c.WithUserAgent(DefaultUserAgent)
}

// Clone returns a deep copy of the [Client] instance.
// It is used internally to maintain immutability across configuration calls.
func (c *Client) Clone() *Client {
	beforeCopy := make([]func(req *http.Request), len(c.beforeRequest))
	copy(beforeCopy, c.beforeRequest)

	afterCopy := make([]func(resp *http.Response, err error), len(c.afterResponse))
	copy(afterCopy, c.afterResponse)

	return &Client{
		http:               c.http,
		baseURL:            c.baseURL,
		headers:            c.headers.Clone(),
		baseResponse:       c.baseResponse,
		hedgingDelay:       c.hedgingDelay,
		beforeRequest:      beforeCopy,
		afterResponse:      afterCopy,
		maxResponseSize:    c.maxResponseSize,
		ssrfGuard:          c.ssrfGuard,
		happyEyeballsDelay: c.happyEyeballsDelay,
		multiReadThreshold: c.multiReadThreshold,
		logger:             c.logger,
		sourceRotator:      c.sourceRotator,
		dnsResolver:        c.dnsResolver,
		defaultMods:        c.defaultMods,
		headersCookieJar:   c.headersCookieJar,
		ja4Callback:        c.ja4Callback,
		tlsBrowserID:       c.tlsBrowserID,
		fragmentConfig:     c.fragmentConfig,
		hostRewrite:        c.hostRewrite,
		p0fSignature:       c.p0fSignature,
		h2Settings:         c.h2Settings,
	}
}

// Request constructs and executes an HTTP request using the configured transport.
// It resolves relative paths against the configured base URL.
//
// Context cancellation or timeouts are fully propagated to the underlying transport.
// If SSRF guarding is enabled, requests resolving to private or loopback IPs return [ErrSSRFBlocked].
// If response size limits are set, reading past the limit returns [ErrResponseTooLarge].
//
// If path is empty, it resolves directly to the base URL.
// Nil modifiers in the mods slice are safely ignored.
//
// The method performs automatic decompression (brotli, zstd, gzip) and content-type
// charset transcoding to UTF-8. It registers a garbage collection finalizer on the
// response body to prevent socket leaks, and caches responses below the multi-read threshold.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	if c.ssrfGuard {
		ctx = context.WithValue(ctx, ssrfGuardCtxKey{}, true)
	}

	ctx = context.WithValue(ctx, happyEyeballsDelayCtxKey{}, c.happyEyeballsDelay)

	if c.ja4Callback != nil {
		ctx = context.WithValue(ctx, ja4CallbackCtxKey{}, c.ja4Callback)
	}

	if c.p0fSignature != nil {
		ctx = context.WithValue(ctx, p0fSignatureCtxKey{}, c.p0fSignature)
	}

	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("aoni: invalid path: %w", err)
	}

	u := c.baseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.headers)

	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "zstd, br, gzip")
	}

	for _, mod := range c.defaultMods {
		if mod != nil {
			mod(req)
		}
	}

	for _, mod := range mods {
		if mod != nil {
			mod(req)
		}
	}

	if errVal := req.Context().Value(bodyErrorCtxKey{}); errVal != nil {
		if serializationErr, ok := errVal.(error); ok {
			return nil, fmt.Errorf("aoni: body encoding failed: %w", serializationErr)
		}
	}

	if errVal := req.Context().Value(queryErrorCtxKey{}); errVal != nil {
		if serializationErr, ok := errVal.(error); ok {
			return nil, fmt.Errorf("aoni: query encoding failed: %w", serializationErr)
		}
	}

	for _, hook := range c.beforeRequest {
		hook(req)
	}

	if isolatedJar, ok := c.headersCookieJar.(*ProxyIsolatedCookieJar); ok {
		jar := isolatedJar.getJar(ctx)
		if jar != nil {
			for _, cookie := range jar.Cookies(req.URL) {
				req.AddCookie(cookie)
			}
		}
	}

	var (
		resp   *http.Response
		reqErr error
	)

	var hedgingDelay time.Duration
	if req.Context().Value(hedgingCtxKey{}) != nil {
		hedgingDelay = req.Context().Value(hedgingCtxKey{}).(time.Duration)
	} else {
		hedgingDelay = c.hedgingDelay
	}

	if hedgingDelay > 0 {
		resp, reqErr = c.executeWithHedging(ctx, hedgingDelay, req)
	} else {
		resp, reqErr = c.http.Do(req)
	}

	// Copy TLS JA4 report from the dialer store to the target TraceInfo.
	// The store was set by TraceJA4; dialTLSWithUTLS wrote the TLS report during handshake.
	if store, ok := req.Context().Value(ja4ReportCtxKey{}).(*ja4ReportStore); ok && store.target != nil &&
		store.report != nil {
		store.target.JA4.JA4 = store.report.JA4
		store.target.JA4.Protocol = store.report.Protocol
		store.target.JA4.Version = store.report.Version
		store.target.JA4.SNI = store.report.SNI
		store.target.JA4.CipherCount = store.report.CipherCount
		store.target.JA4.ExtCount = store.report.ExtCount
		store.target.JA4.ALPN = store.report.ALPN
	}

	if isolatedJar, ok := c.headersCookieJar.(*ProxyIsolatedCookieJar); ok {
		jar := isolatedJar.getJar(ctx)
		if jar != nil {
			if rc := resp.Cookies(); len(rc) > 0 {
				jar.SetCookies(req.URL, rc)
			}
		}
	}

	for _, hook := range c.afterResponse {
		hook(resp, reqErr)
	}

	if reqErr != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", reqErr)
	}

	if resp != nil && resp.Body != nil {
		// Trigger download progress callback.
		if onProgress, ok := req.Context().Value(downloadProgressCtxKey{}).(ProgressFunc); ok && onProgress != nil {
			resp.Body = &progressReader{
				reader:     resp.Body,
				total:      resp.ContentLength,
				onProgress: onProgress,
			}
		}

		// Handle automatic response decompression.
		switch resp.Header.Get("Content-Encoding") {
		case "br":
			resp.Body = &decompressReadCloser{
				Reader: brotli.NewReader(resp.Body),
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1

		case "zstd":
			zstdDec, err := zstd.NewReader(resp.Body)
			if err == nil {
				resp.Body = &decompressReadCloser{
					Reader: zstdDec,
					closer: resp.Body,
				}
				resp.Header.Del("Content-Encoding")
				resp.Header.Del("Content-Length")
				resp.ContentLength = -1
			}

		case "gzip":
			gzReader, err := gzip.NewReader(resp.Body)
			if err == nil {
				resp.Body = &decompressReadCloser{
					Reader: gzReader,
					closer: resp.Body,
				}
				resp.Header.Del("Content-Encoding")
				resp.Header.Del("Content-Length")
				resp.ContentLength = -1
			}
		}

		// Transcode response from non-UTF-8 character set.
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			if _, params, err := mime.ParseMediaType(contentType); err == nil {
				if charset := params["charset"]; charset != "" {
					charset = strings.ToLower(charset)
					if charset != "utf-8" && charset != "utf8" {
						if enc, err := htmlindex.Get(charset); err == nil {
							resp.Body = struct {
								io.Reader
								io.Closer
							}{
								Reader: transform.NewReader(resp.Body, enc.NewDecoder()),
								Closer: resp.Body,
							}
						}
					}
				}
			}
		}
	}

	if resp != nil && resp.Body != nil {
		// Enforce response size limits.
		if c.maxResponseSize > 0 {
			if resp.ContentLength > c.maxResponseSize {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
			}

			resp.Body = &limitCheckingReadCloser{
				ReadCloser: resp.Body,
				limit:      c.maxResponseSize,
			}
		}

		// Enable replayable multi-read body caching.
		var multiReadThreshold int64
		if val := req.Context().Value(multiReadCtxKey{}); val != nil {
			multiReadThreshold = val.(int64)
		} else {
			multiReadThreshold = c.multiReadThreshold
		}

		if multiReadThreshold > 0 {
			mBody, err := newMultiReadBody(resp.Body, multiReadThreshold)
			if err == nil {
				resp.Body = mBody
			}
		}

		// Prevent socket leaks via finalizer.
		resp.Body = newFinalizerReadCloser(resp.Body)
	}

	return resp, nil
}

// ConnectionPoolConfig defines tuning parameters for the client's connection pool.
// Apply these settings to a client using [Client.WithConnectionPool].
type ConnectionPoolConfig struct {
	// MaxIdleConns is the maximum number of idle connections across all hosts.
	MaxIdleConns int
	// MaxIdleConnsPerHost is the maximum number of idle connections kept per host.
	MaxIdleConnsPerHost int
	// MaxConnsPerHost is the maximum total number of connections allowed per host.
	MaxConnsPerHost int
	// IdleConnTimeout is the maximum duration an idle connection is kept open.
	IdleConnTimeout time.Duration
	// ResponseHeaderTimeout is the maximum duration to wait for reading response headers.
	ResponseHeaderTimeout time.Duration
}

// BrowserID defines browser TLS configurations used for JA3 fingerprint evasion.
// Pass these identifiers to [Client.WithTLSFingerprint].
type BrowserID int

const (
	// BrowserNone disables TLS fingerprint emulation.
	BrowserNone BrowserID = iota
	// BrowserChrome emulates Google Chrome TLS fingerprints.
	BrowserChrome
	// BrowserFirefox emulates Mozilla Firefox TLS fingerprints.
	BrowserFirefox
	// BrowserSafari emulates Apple Safari TLS fingerprints.
	BrowserSafari
)

// WithLogger returns a new [Client] with the given logger.
func (c *Client) WithLogger(l Logger) *Client {
	newClient := c.Clone()
	newClient.logger = l
	return newClient
}

// WithModifiers returns a new [Client] with the given request modifiers applied by default.
func (c *Client) WithModifiers(mods ...RequestModifier) *Client {
	newClient := c.Clone()
	newClient.defaultMods = append(newClient.defaultMods, mods...)
	return newClient
}

// WithMultiReadBody returns a [RequestModifier] setting the body re-readability threshold.
// Passing a threshold <= 0 disables body caching for the request.
func WithMultiReadBody(threshold int64) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), multiReadCtxKey{}, threshold)
		*req = *req.WithContext(ctx)
	}
}

// WithBaseResponse returns a new [Client] utilizing the given provider for responses.
// Pass nil to clear any previously configured provider.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := c.Clone()
	newClient.baseResponse = provider
	return newClient
}

// WithBaseURL returns a new [Client] with the specified base URL.
// An empty raw string clears the base URL.
// Relative paths in [Client.Request] are resolved against this base URL.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		newClient := c.Clone()
		newClient.baseURL = &url.URL{}
		return newClient
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, _ := url.Parse(raw)

	newClient := c.Clone()
	newClient.baseURL = baseURL

	return newClient
}

// WithHeader returns a new [Client] with the given default header set.
// It overwrites any existing header with the same key.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := c.Clone()
	newClient.headers.Set(key, value)
	return newClient
}

// WithTimeout returns a new [Client] configured with the specified request timeout.
// This configuration is only applied if the underlying [HTTPDoer] is an [http.Client].
// A duration <= 0 represents no timeout.
func (c *Client) WithTimeout(d time.Duration) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Timeout = d
		newClient.http = &cloned
	}

	return newClient
}

// WithRedirectLimit returns a new [Client] with a custom redirect handling limit.
// A max value of 0 disables redirects. A negative value restores default Go behavior.
func (c *Client) WithRedirectLimit(max int) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		switch {
		case max == 0:
			cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		case max > 0:
			cloned.CheckRedirect = DefaultRedirectPolicy(max)
		default:
			cloned.CheckRedirect = DefaultRedirectPolicy(10)
		}

		newClient.http = &cloned
	}

	return newClient
}

// WithLocalAddr returns a new [Client] bound to the specified local IP address.
// An invalid IP address string will prevent the custom dialer from binding.
// This option is only applied if the underlying [HTTPDoer] is an [http.Client] with an [http.Transport].
func (c *Client) WithLocalAddr(addr string) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			localAddr, err := net.ResolveIPAddr("ip", addr)
			if err == nil {
				dialer := &net.Dialer{
					LocalAddr: &net.TCPAddr{IP: localAddr.IP},
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}
				// Wrap existing DialContext instead of cloning the transport.
				prevDial := transport.DialContext
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					if prevDial != nil {
						return prevDial(ctx, network, addr)
					}

					return dialer.DialContext(ctx, network, addr)
				}
			}
		}
	}

	return newClient
}

// WithHedging returns a new [Client] configured with the specified hedging delay.
// Hedging sends a secondary request if the primary request does not respond within the delay.
// A delay <= 0 disables request hedging.
func (c *Client) WithHedging(d time.Duration) *Client {
	newClient := c.Clone()
	newClient.hedgingDelay = d
	return newClient
}

// WithMaxResponseSize returns a new [Client] enforcing the specified maximum response size.
// Setting size <= 0 disables any response size limits.
func (c *Client) WithMaxResponseSize(size int64) *Client {
	newClient := c.Clone()
	newClient.maxResponseSize = size
	return newClient
}

// WithSSRFGuard returns a new [Client] with SSRF protection enabled.
// When enabled, requests resolving to private or loopback IP ranges are blocked.
func (c *Client) WithSSRFGuard() *Client {
	newClient := c.Clone()
	newClient.ssrfGuard = true
	newClient.applyDialers()

	return newClient
}

// WithHappyEyeballs returns a new [Client] configured with a Happy Eyeballs staggered delay.
// Setting delay <= 0 disables staggered dialing and uses a single connection attempt.
func (c *Client) WithHappyEyeballs(delay time.Duration) *Client {
	newClient := c.Clone()
	newClient.happyEyeballsDelay = delay
	newClient.applyDialers()

	return newClient
}

// WithMultiReadBody returns a new [Client] with the default response body caching threshold.
// Setting threshold <= 0 disables automatic response body caching.
func (c *Client) WithMultiReadBody(threshold int64) *Client {
	newClient := c.Clone()
	newClient.multiReadThreshold = threshold
	return newClient
}

// WithLocalAddrPool returns a new [Client] with the given local address pool.
func (c *Client) WithLocalAddrPool(addrs []string) *Client {
	rotator, err := NewSourceIPRotator(addrs)
	if err != nil {
		return c
	}

	newClient := c.Clone()
	newClient.sourceRotator = rotator
	newClient.applyDialers()

	return newClient
}

// WithDNSResolver returns a new [Client] with the given DNS resolver.
func (c *Client) WithDNSResolver(resolver DNSResolver) *Client {
	newClient := c.Clone()
	newClient.dnsResolver = resolver
	newClient.applyDialers()

	return newClient
}

// WithDoT returns a new [Client] with the given DNS-over-TLS resolver.
func (c *Client) WithDoT(endpoint, host string) *Client {
	return c.WithDNSResolver(NewDoTResolver(endpoint, host))
}

// WithDoH returns a new [Client] with the given DNS-over-HTTPS resolver.
func (c *Client) WithDoH(endpoint, host string) *Client {
	return c.WithDNSResolver(NewDoHResolver(endpoint, host))
}

// WithBeforeRequest returns a new [Client] with the given request hook registered.
// Before-request hooks are executed in the order they are registered.
func (c *Client) WithBeforeRequest(hook func(req *http.Request)) *Client {
	newClient := c.Clone()
	newClient.beforeRequest = append(newClient.beforeRequest, hook)
	return newClient
}

// WithAfterResponse returns a new [Client] with the given response hook registered.
// After-response hooks are executed regardless of whether the request succeeded or failed.
func (c *Client) WithAfterResponse(hook func(resp *http.Response, err error)) *Client {
	newClient := c.Clone()
	newClient.afterResponse = append(newClient.afterResponse, hook) //nolint:bodyclose
	return newClient
}

// WithUserAgent returns a new [Client] with the specified User-Agent header.
func (c *Client) WithUserAgent(ua string) *Client {
	return c.WithHeader("User-Agent", ua)
}

// UserAgent returns the default User-Agent header value configured on the client.
func (c *Client) UserAgent() string {
	return c.headers.Get("User-Agent")
}

// WithOrigin returns a new [Client] configured with the specified Origin header.
func (c *Client) WithOrigin(origin string) *Client {
	return c.WithHeader("Origin", origin)
}

// WithBearer returns a new [Client] configured with the specified Bearer token.
func (c *Client) WithBearer(token string) *Client {
	return c.WithHeader("Authorization", "Bearer "+token)
}

// WithBasicAuth returns a new [Client] configured with the specified Basic Auth credentials.
func (c *Client) WithBasicAuth(username, password string) *Client {
	return c.WithHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
}

// WithCookieJar returns a new [Client] configured with the specified cookie jar.
// This configuration is only applied if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) WithCookieJar(jar http.CookieJar) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Jar = jar
		newClient.http = &cloned
	}

	return newClient
}

// WithConnectionPool returns a new [Client] with the transport pool tuned to the given configuration.
// It is only effective if the client is using an [http.Client] with an [http.Transport].
// If config fields are <= 0, they are ignored and the original transport settings are kept.
func (c *Client) WithConnectionPool(cfg ConnectionPoolConfig) *Client {
	newClient := c.Clone()
	if transport := newClient.Transport(); transport != nil {
		// Pool settings are safe to modify directly — Go's http.Transport
		// uses atomic operations for these fields. No transport clone needed.
		if cfg.MaxIdleConns > 0 {
			transport.MaxIdleConns = cfg.MaxIdleConns
		}

		if cfg.MaxIdleConnsPerHost > 0 {
			transport.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
		}

		if cfg.MaxConnsPerHost > 0 {
			transport.MaxConnsPerHost = cfg.MaxConnsPerHost
		}

		if cfg.IdleConnTimeout > 0 {
			transport.IdleConnTimeout = cfg.IdleConnTimeout
		}

		if cfg.ResponseHeaderTimeout > 0 {
			transport.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
		}
	}

	return newClient
}

// WithTLSFingerprint returns a new [Client] configured to use uTLS for JA3 signature evasion.
// Passing [BrowserNone] disables TLS fingerprint emulation.
// This option is only effective if the client is using an [http.Client] with an [http.Transport].
func (c *Client) WithTLSFingerprint(browser BrowserID) *Client {
	newClient := c.Clone()
	if browser == BrowserNone {
		return newClient
	}

	// Store browser ID on the Client so it works with any HTTPDoer type
	// (ProxyRotator, LoadBalancer, etc.), not just *http.Client.
	newClient.tlsBrowserID = browser

	if transport := newClient.Transport(); transport != nil {
		callback := newClient.ja4Callback // capture by value
		tlsConfig := transport.TLSClientConfig
		// Set DialTLSContext directly — no transport clone needed.
		// This preserves the existing connection pool.
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialTLSWithUTLS(
				ctx,
				network,
				addr,
				browser,
				newClient.sourceRotator,
				newClient.dnsResolver,
				callback,
				tlsConfig,
			)
		}
	}

	return newClient
}

// WithJA4Callback returns a new [Client] that invokes fn with the JA4 fingerprint
// after each TLS handshake. The callback receives both the TLS (JA4) fingerprint
// and, if [TraceJA4] is used, the HTTP (JA4H) fingerprint.
//
// This option requires [WithTLSFingerprint] to be enabled.
func (c *Client) WithJA4Callback(fn func(ja4.Report)) *Client {
	newClient := c.Clone()
	newClient.ja4Callback = fn
	return newClient
}

// WithFragmentation returns a new [Client] configured with TLS fragmentation settings.
// When set, the TLS ClientHello is split into smaller chunks across multiple TCP segments.
func (c *Client) WithFragmentation(cfg FragmentConfig) *Client {
	newClient := c.Clone()
	newClient.fragmentConfig = &cfg
	return newClient
}

// WithHostRewrite returns a new [Client] that rewrites hostnames to IP addresses
// while preserving the original hostname for TLS SNI.
func (c *Client) WithHostRewrite(rules map[string]string) *Client {
	newClient := c.Clone()
	newClient.hostRewrite = &HostRewriteConfig{Rules: rules}
	return newClient
}

// WithProxyIsolatedCookieJar returns a new [Client] that uses per-proxy cookie isolation.
// Each proxy gets its own cookie jar, preventing cross-proxy cookie leakage.
func (c *Client) WithProxyIsolatedCookieJar(jar *ProxyIsolatedCookieJar) *Client {
	newClient := c.Clone()
	newClient.headersCookieJar = jar
	return newClient
}

// WithDNSCache returns a new [Client] that wraps the current DNS resolver
// with an in-memory cache using the specified TTL.
func (c *Client) WithDNSCache(ttl time.Duration) *Client {
	newClient := c.Clone()
	newClient.dnsResolver = NewInMemoryDNSCache(ttl, c.dnsResolver)
	return newClient
}

// WithHTTP2Settings returns a new [Client] configured with custom HTTP/2 parameters.
func (c *Client) WithHTTP2Settings(settings HTTP2Settings) *Client {
	newClient := c.Clone()
	newClient.h2Settings = &settings
	return newClient
}

// WithP0fSignature configures TCP/IP spoofing based on a p0f signature.
// After the TCP connection is established, spoofable fields (TTL, DF, window)
// are applied via syscalls to make the connection appear as the specified OS
// to passive fingerprinters like p0f.
func (c *Client) WithP0fSignature(sig *p0f.Signature) *Client {
	newClient := c.Clone()
	newClient.p0fSignature = sig
	return newClient
}

// Logger is an interface for logging messages.
type Logger interface {
	Debug(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	Warn(msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Logger returns the logger used by the client.
// If no logger is set, a no-op logger is returned.
func (c *Client) Logger() Logger {
	if c.logger == nil {
		return &noopLogger{}
	}

	return c.logger
}

// BaseResponse returns a new [BaseResponse] wrapper if a provider is configured on the client.
// Returns nil if no provider is set.
func (c *Client) BaseResponse() BaseResponse {
	if c.baseResponse == nil {
		return nil
	}

	return c.baseResponse()
}

// HTTP returns the underlying [HTTPDoer] interface.
func (c *Client) HTTP() HTTPDoer {
	return c.http
}

// Transport returns the underlying [http.Transport] of the client.
// Returns nil if the [HTTPDoer] is not an [http.Client] or its transport is not an [http.Transport].
func (c *Client) Transport() *http.Transport {
	if httpClient, ok := c.http.(*http.Client); ok {
		if httpClient.Transport == nil {
			httpClient.Transport = &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
		}

		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			return transport
		}
	}

	return nil
}

// CloseIdleConnections closes any idle keep-alive connections maintained by the client.
// This only works if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.http.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

func (c *Client) applyDialers() {
	if transport := c.Transport(); transport != nil {
		// Set DialContext directly — called once at construction time,
		// no need to clone the transport and its connection pool.
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return happyEyeballsDial(
				ctx,
				network,
				addr,
				c.happyEyeballsDelay,
				c.ssrfGuard,
				c.sourceRotator,
				c.dnsResolver,
			)
		}
	}
}

func dialTLSWithUTLS(
	ctx context.Context,
	network, addr string,
	browser BrowserID,
	sourceRotator *SourceIPRotator,
	dnsResolver DNSResolver,
	ja4Callback func(ja4.Report),
	tlsConfig *tls.Config,
) (net.Conn, error) {
	// Read callback from context (set by Client.Request) — the closure-captured
	// value may be stale if WithJA4Callback was called after WithTLSFingerprint.
	if cb, ok := ctx.Value(ja4CallbackCtxKey{}).(func(ja4.Report)); ok && cb != nil {
		ja4Callback = cb
	}

	ssrfGuard := ctx.Value(ssrfGuardCtxKey{}) != nil

	var delay time.Duration
	if val := ctx.Value(happyEyeballsDelayCtxKey{}); val != nil {
		delay = val.(time.Duration)
	} else {
		delay = 300 * time.Millisecond
	}

	conn, err := happyEyeballsDial(ctx, network, addr, delay, ssrfGuard, sourceRotator, dnsResolver)
	if err != nil {
		return nil, err
	}

	var spec utls.ClientHelloID
	switch browser {
	case BrowserFirefox:
		spec = utls.HelloFirefox_Auto
	case BrowserSafari:
		spec = utls.HelloSafari_Auto
	default:
		spec = utls.HelloChrome_Auto
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	uConfig := &utls.Config{
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	}

	if tlsConfig != nil {
		uConfig.InsecureSkipVerify = tlsConfig.InsecureSkipVerify
		uConfig.RootCAs = tlsConfig.RootCAs
	}

	if alpn, ok := ctx.Value(alpnOverrideCtxKey{}).([]string); ok && len(alpn) > 0 {
		uConfig.NextProtos = alpn
	}

	uConn := utls.UClient(conn, uConfig, spec)
	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	alpnProtos := []string{"http/1.1"}
	if alpn, ok := ctx.Value(alpnOverrideCtxKey{}).([]string); ok && len(alpn) > 0 {
		alpnProtos = alpn
	}

	uConn.Extensions = forceALPN(uConn.Extensions, alpnProtos)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	report := extractJA4FromUConn(uConn, host)

	// Write JA4 report to the store in the request context (set by TraceJA4).
	// The request context flows through to DialTLSContext.
	if store, ok := ctx.Value(ja4ReportCtxKey{}).(*ja4ReportStore); ok {
		store.report = &report
	}

	if ja4Callback != nil {
		ja4Callback(report)
	}

	return uConn, nil
}

// extractJA4FromUConn computes a JA4 fingerprint from a uTLS connection after handshake.
func extractJA4FromUConn(uConn *utls.UConn, _ string) ja4.Report {
	_ = uConn.BuildHandshakeState()

	hello := uConn.HandshakeState.Hello

	var (
		extensions    []uint16
		sigAlgorithms []uint16
	)

	if len(hello.Raw) > 0 {
		extensions, sigAlgorithms = ja4.ParseExtensionsFromRaw(hello.Raw)
	}

	// Convert signature algorithms to uint16
	sigAlgos := make([]uint16, len(sigAlgorithms))
	for i, s := range sigAlgorithms {
		sigAlgos[i] = uint16(s)
	}

	sni := hello.ServerName != ""
	fingerprint := ja4.ComputeJA4(
		hello.CipherSuites,
		extensions,
		hello.SupportedVersions,
		sni,
		hello.AlpnProtocols,
		sigAlgos,
	)

	report := ja4.Report{
		JA4:         fingerprint,
		Protocol:    "t",
		CipherCount: len(ja4.FilterGREASE(hello.CipherSuites)),
		ExtCount:    len(ja4.FilterGREASE(extensions)),
	}

	// Parse version from fingerprint
	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	if sni {
		report.SNI = "d"
	} else {
		report.SNI = "i"
	}

	if len(hello.AlpnProtocols) > 0 && hello.AlpnProtocols[0] != "" {
		report.ALPN = string(hello.AlpnProtocols[0][0]) + string(hello.AlpnProtocols[0][len(hello.AlpnProtocols[0])-1])
	} else {
		report.ALPN = "00"
	}

	return report
}

// ja4ReportStore is a shared pointer that allows dialTLSWithUTLS to write the JA4 report
// and Client.Request to copy it to the target TraceInfo after the request completes.
type ja4ReportStore struct {
	report *ja4.Report
	target *TraceInfo
}

func (c *Client) executeWithHedging(
	ctx context.Context,
	delay time.Duration,
	req *http.Request,
) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}

	resultsCh := make(chan result, 2)
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	var (
		cleaned bool
		mu      sync.Mutex
	)

	cleanup := func(winner int) {
		mu.Lock()
		defer mu.Unlock()

		if cleaned {
			return
		}

		cleaned = true

		switch winner {
		case 1:
			cancel2()
		case 2:
			cancel1()
		default:
			cancel1()
			cancel2()
		}
	}

	defer func() {
		cleanup(0)
	}()

	cloneReq := func(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
		cloned := orig.Clone(reqCtx)
		if orig.Body != nil && orig.Body != http.NoBody {
			if orig.GetBody != nil {
				body, err := orig.GetBody()
				if err != nil {
					return nil, err
				}

				cloned.Body = body
			} else {
				return nil, errors.New("aoni: request body cannot be duplicated for hedging")
			}
		}

		return cloned, nil
	}

	req1, err := cloneReq(req, ctx1)
	if err != nil {
		return nil, err
	}

	go func() {
		resp, err := c.http.Do(req1) //nolint:bodyclose
		resultsCh <- result{resp: resp, err: err}
	}()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var (
		req2Started bool
		firstErr    error
	)

	activeCount := 1

	for activeCount > 0 {
		select {
		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				winner := 1

				cancelWinner := cancel1
				if res.resp.Request != nil && res.resp.Request.Context() == ctx2 {
					winner = 2
					cancelWinner = cancel2
				}

				cleanup(winner)

				res.resp.Body = &contextCancelingReadCloser{
					ReadCloser: res.resp.Body,
					cancel:     cancelWinner,
				}

				return res.resp, nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				select {
				case <-timer.C:
				default:
				}

				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					return nil, err
				}

				activeCount++

				go func() {
					resp, err := c.http.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-timer.C:
			if !req2Started {
				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					break
				}

				activeCount++

				go func() {
					resp, err := c.http.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, firstErr
}

func isCrossOrigin(u1, u2 *url.URL) bool {
	if u1.Scheme != u2.Scheme {
		return true
	}

	if u1.Host != u2.Host {
		return true
	}

	return false
}

func unwrapBody(c io.Closer) io.Closer {
	for {
		u, ok := c.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		c = u.Unwrap()
	}

	return c
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_ = resp.Body.Close()

	if rb, ok := unwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}

	// Check private IP ranges.
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}

	if ip6 := ip.To16(); ip6 != nil {
		// Check unique local IPv6.
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}

func happyEyeballsDial(
	ctx context.Context,
	network, addr string,
	delay time.Duration,
	ssrfGuard bool,
	rotator *SourceIPRotator,
	dnsResolver DNSResolver,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	if cfg, ok := ctx.Value(hostRewriteCtxKey{}).(*HostRewriteConfig); ok && cfg != nil {
		if rewritten, exists := cfg.Rules[host]; exists {
			if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
				host = newHost

				if newPort != "" {
					port = newPort
				}
			}
		}
	}

	resolver := dnsResolver
	if resolver == nil {
		resolver = &net.Resolver{}
	}

	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	var filtered []net.IP
	for _, ia := range addrs {
		if ssrfGuard && isBlockedIP(ia.IP) {
			continue
		}

		filtered = append(filtered, ia.IP)
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("%w: %s resolves to blocked or empty IPs", ErrSSRFBlocked, host)
	}

	if len(filtered) == 1 || delay <= 0 {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		if rotator != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: rotator.Next()}
		}

		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(filtered[0].String(), port))
		if err != nil {
			return nil, err
		}

		if cfg, ok := ctx.Value(fragmentCtxKey{}).(FragmentConfig); ok && cfg.ChunkSize > 0 {
			conn = wrapWithFragmentation(conn, cfg)
		}

		// After fragmentation wrapping, apply p0f spoofing if configured
		if cfg, ok := ctx.Value(p0fSignatureCtxKey{}).(*p0f.Signature); ok && cfg != nil {
			spoofer := p0f.NewSpoofer(cfg)
			_ = spoofer.Apply(conn) // best-effort
		}

		return conn, nil
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}

	resultCh := make(chan dialResult, len(filtered))

	dialCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	var (
		wg   sync.WaitGroup
		done uint32
	)

	for i, ip := range filtered {
		wg.Add(1)

		go func(targetIP net.IP, idx int) {
			defer wg.Done()

			if idx > 0 {
				select {
				case <-dialCtx.Done():
					return
				case <-time.After(time.Duration(idx) * delay):
				}
			}

			if atomic.LoadUint32(&done) == 1 {
				return
			}

			dialer := &net.Dialer{Timeout: 30 * time.Second}

			conn, err := dialer.DialContext(dialCtx, network, net.JoinHostPort(targetIP.String(), port))
			if err == nil {
				if atomic.CompareAndSwapUint32(&done, 0, 1) {
					resultCh <- dialResult{conn: conn}

					cancelAll()
				} else {
					_ = conn.Close()
				}
			} else {
				resultCh <- dialResult{err: err}
			}
		}(ip, i)
	}

	var firstErr error

	failedCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-resultCh:
			if res.conn != nil {
				conn := res.conn

				if cfg, ok := ctx.Value(fragmentCtxKey{}).(FragmentConfig); ok && cfg.ChunkSize > 0 {
					conn = wrapWithFragmentation(conn, cfg)
				}

				// Apply p0f spoofing if configured
				if cfg, ok := ctx.Value(p0fSignatureCtxKey{}).(*p0f.Signature); ok && cfg != nil {
					spoofer := p0f.NewSpoofer(cfg)
					_ = spoofer.Apply(conn) // best-effort
				}

				if order, ok := ctx.Value(orderedHeadersCtxKey{}).([]string); ok && len(order) > 0 {
					return &headerOrderingConn{Conn: conn, orderedKeys: order}, nil
				}

				return conn, nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			failedCount++
			if failedCount == len(filtered) {
				return nil, firstErr
			}
		}
	}
}

func forceALPN(extensions []utls.TLSExtension, protos []string) []utls.TLSExtension {
	found := false
	filtered := make([]utls.TLSExtension, 0, len(extensions))

	for _, ext := range extensions {
		switch ext.(type) {
		case *utls.ALPNExtension:
			filtered = append(filtered, &utls.ALPNExtension{
				AlpnProtocols: protos,
			})
			found = true
		case *utls.ApplicationSettingsExtension:
			if slices.Contains(protos, "h2") {
				filtered = append(filtered, ext)
			}
		default:
			filtered = append(filtered, ext)
		}
	}

	if !found {
		filtered = append(filtered, &utls.ALPNExtension{
			AlpnProtocols: protos,
		})
	}

	return filtered
}

type noopLogger struct{}

func (l noopLogger) Debug(_ string, _ ...any)                           {}
func (l noopLogger) DebugContext(_ context.Context, _ string, _ ...any) {}
func (l noopLogger) Info(_ string, _ ...any)                            {}
func (l noopLogger) InfoContext(_ context.Context, _ string, _ ...any)  {}
func (l noopLogger) Warn(_ string, _ ...any)                            {}
func (l noopLogger) WarnContext(_ context.Context, _ string, _ ...any)  {}
func (l noopLogger) Error(_ string, _ ...any)                           {}
func (l noopLogger) ErrorContext(_ context.Context, _ string, _ ...any) {}

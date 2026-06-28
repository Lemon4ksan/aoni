// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
	"github.com/lemon4ksan/aoni/profiles"
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
	proxyDNSCtxKey           struct{}
	proxyAddrCtxKey          struct{}
	sessionCacheCtxKey       struct{}
	packetPaddingCtxKey      struct{}
)

// DefaultSensitiveHeaders lists headers removed from requests during
// cross-origin redirects. Used by [DefaultRedirectPolicy].
var DefaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// Unwrapper allows nested decorators to be peeled away to reach the
// underlying [Requester]. [Client] does not implement this interface;
// wrapper types returned by [NewStdClient] or [Chain] do.
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient strips all [Unwrapper] layers from r and returns the
// innermost [Client]. Returns nil if r is not a *Client and no
// Unwrapper chain leads to one.
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

// HTTPDoer executes an [http.Request] and returns a response.
// [http.Client] satisfies this interface. Pass a [DoerFunc] to adapt
// a plain function.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc adapts a function to the [HTTPDoer] interface.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do calls f(req).
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Requester sends an HTTP request and returns the response.
// [Client] is the primary implementation. Relative paths are resolved
// against the base URL. Request modifiers are applied before execution.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider optionally provides a [BaseResponse] for
// structured decoding. Implemented by response wrapper types used
// with [Client.WithBaseResponse].
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// ProgressFunc is called periodically during response body reads.
// current is the bytes read so far; total is the Content-Length
// value or -1 if unknown.
type ProgressFunc func(current, total int64)

// BaseResponse is implemented by user-defined response wrappers that
// participate in [GetJSON] and similar generic request helpers. The
// decoder calls IsSuccess, SetData, and Error to route the result.
type BaseResponse interface {
	// IsSuccess reports whether the response indicates a successful operation.
	IsSuccess() bool
	// Error returns an error representation if IsSuccess returns false.
	Error() error
	// SetData sets the data into the response.
	SetData(data any)
}

// DefaultRedirectPolicy returns a function suitable for
// [http.Client.CheckRedirect]. It stops after maxRedirects and strips
// sensitiveHeaders on cross-origin redirects. When sensitiveHeaders
// is empty, [DefaultSensitiveHeaders] is used.
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

// Client is an immutable, concurrency-safe HTTP client built on [HTTPDoer].
// Every With* method returns a new clone, so the original remains usable
// by other goroutines. Use [NewClient] to create the first instance.
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
	proxyDNS         bool
	proxyAddr        *url.URL
	dynamicHedging   *DynamicHedgingConfig
	sessionCache     *ProxyAwareSessionCache
	packetPadding    *PaddingConfig
	transportProxy   func(*http.Request) (*url.URL, error)
}

// NewClient creates a [Client] wrapping httpClient. When httpClient
// is nil a default [http.Client] with a 15-second timeout and
// [DefaultRedirectPolicy] (10 hops) is used. The returned client
// has [DefaultUserAgent] set and a transport dialer configured for
// Happy Eyeballs.
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

// Clone returns a deep copy of c. The cloned client shares nothing
// mutable with the original - transport, cookie jar, and config
// structs are all independently copied.
func (c *Client) Clone() *Client {
	beforeCopy := make([]func(req *http.Request), len(c.beforeRequest))
	copy(beforeCopy, c.beforeRequest)

	afterCopy := make([]func(resp *http.Response, err error), len(c.afterResponse))
	copy(afterCopy, c.afterResponse)

	var defaultModsCopy []RequestModifier
	if c.defaultMods != nil {
		defaultModsCopy = make([]RequestModifier, len(c.defaultMods))
		copy(defaultModsCopy, c.defaultMods)
	}

	cloned := &Client{
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
		defaultMods:        defaultModsCopy,
		headersCookieJar:   c.headersCookieJar,
		ja4Callback:        c.ja4Callback,
		tlsBrowserID:       c.tlsBrowserID,
		proxyDNS:           c.proxyDNS,
		proxyAddr:          c.proxyAddr,
		sessionCache:       c.sessionCache,
		transportProxy:     c.transportProxy,
	}

	// Clone http.Client and its transport to avoid race conditions.
	// If the transport is wrapped in cookieJarTransport, unwrap, clone the
	// base transport, and re-wrap to preserve the cookie jar binding.
	if httpClient, ok := cloned.http.(*http.Client); ok {
		clonedHTTP := *httpClient
		baseTransport := clonedHTTP.Transport

		var wrappedJar *ProxyIsolatedCookieJar

		if cjTrans, ok := baseTransport.(*cookieJarTransport); ok {
			wrappedJar = cjTrans.cookieJar
			baseTransport = cjTrans.next
		}

		if transport, ok := baseTransport.(*http.Transport); ok && transport != nil {
			baseTransport = transport.Clone()
		}

		if wrappedJar != nil {
			clonedHTTP.Transport = &cookieJarTransport{
				next:      baseTransport,
				cookieJar: wrappedJar,
			}
		} else {
			clonedHTTP.Transport = baseTransport
		}

		cloned.http = &clonedHTTP
	}

	if c.dynamicHedging != nil {
		dhCopy := *c.dynamicHedging
		cloned.dynamicHedging = &dhCopy
	}

	// Deep copy mutable config structs so mutations in one clone don't affect others.
	if c.fragmentConfig != nil {
		fragCopy := *c.fragmentConfig
		cloned.fragmentConfig = &fragCopy
	}

	if c.hostRewrite != nil && c.hostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(c.hostRewrite.Rules))
		maps.Copy(rulesCopy, c.hostRewrite.Rules)
		cloned.hostRewrite = &HostRewriteConfig{Rules: rulesCopy}
	}

	if c.p0fSignature != nil {
		sigCopy := *c.p0fSignature
		if len(sigCopy.Options) > 0 {
			optsCopy := make([]string, len(sigCopy.Options))
			copy(optsCopy, sigCopy.Options)
			sigCopy.Options = optsCopy
		}

		if len(sigCopy.Quirks) > 0 {
			qCopy := make([]string, len(sigCopy.Quirks))
			copy(qCopy, sigCopy.Quirks)
			sigCopy.Quirks = qCopy
		}

		cloned.p0fSignature = &sigCopy
	}

	if c.h2Settings != nil {
		h2Copy := *c.h2Settings
		cloned.h2Settings = &h2Copy
	}

	if c.packetPadding != nil {
		padCopy := *c.packetPadding
		cloned.packetPadding = &padCopy
	}

	// Clone the http.Client and its transport so mutations don't affect the original.
	if httpClient, ok := cloned.http.(*http.Client); ok {
		clonedHTTP := *httpClient
		if transport, ok := clonedHTTP.Transport.(*http.Transport); ok {
			if transport != nil {
				clonedHTTP.Transport = transport.Clone()
			}
		}

		cloned.http = &clonedHTTP
	}

	return cloned
}

// Request sends an HTTP request and returns the response. path is
// resolved against [Client.WithBaseURL] when set; an empty path
// targets the base URL directly. Nil modifiers are ignored.
//
// Decompression (gzip, brotli, zstd) and charset transcoding to
// UTF-8 are applied automatically. The response body is wrapped
// with a GC finalizer so that unclosed bodies eventually release
// the underlying connection.
//
// Returns [ErrSSRFBlocked] when SSRF guarding is on and the target
// resolves to a private or loopback address. Returns
// [ErrResponseTooLarge] when a response size limit is configured
// and the body exceeds it.
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

	if c.proxyDNS {
		ctx = context.WithValue(ctx, proxyDNSCtxKey{}, true)
		if c.proxyAddr != nil {
			ctx = context.WithValue(ctx, proxyAddrCtxKey{}, c.proxyAddr)
		}
	}

	if c.sessionCache != nil {
		ctx = context.WithValue(ctx, sessionCacheCtxKey{}, c.sessionCache)
		if c.proxyAddr != nil {
			c.sessionCache.SetProxyKey(c.proxyAddr.String())
		}
	}

	if c.packetPadding != nil {
		ctx = context.WithValue(ctx, packetPaddingCtxKey{}, c.packetPadding)
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

	generic.ApplyOptions(req, c.defaultMods...)
	generic.ApplyOptions(req, mods...)

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

	// Apply packet padding: add random padding header to disrupt DPI length analysis.
	if c.packetPadding != nil {
		if padding := GeneratePadding(*c.packetPadding); len(padding) > 0 {
			headerName := PaddingHeaderName(*c.packetPadding)
			req.Header.Set(headerName, hex.EncodeToString(padding))
		}
	}

	var (
		resp   *http.Response
		reqErr error
	)

	requestStart := time.Now()

	var hedgingDelay time.Duration
	switch {
	case req.Context().Value(hedgingCtxKey{}) != nil:
		hedgingDelay = req.Context().Value(hedgingCtxKey{}).(time.Duration)
	case c.dynamicHedging != nil:
		hedgingDelay = c.dynamicHedging.ComputeDelay()
	default:
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

	for _, hook := range c.afterResponse {
		hook(resp, reqErr)
	}

	if reqErr != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", reqErr)
	}

	// Record RTT for dynamic hedging if configured.
	if c.dynamicHedging != nil && c.dynamicHedging.Tracker != nil {
		rtt := time.Since(requestStart)
		c.dynamicHedging.Tracker.Record(rtt)
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
			} else {
				resp.Header.Del("Content-Encoding")
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
			} else {
				resp.Header.Del("Content-Encoding")
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

// ConnectionPoolConfig tunes the [http.Transport] connection pool.
// Apply it with [Client.WithConnectionPool].
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

// BrowserID selects a uTLS ClientHello profile for JA3 fingerprint
// emulation. Pass to [Client.WithTLSFingerprint].
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

// WithLogger returns a clone of c that logs diagnostics through l.
func (c *Client) WithLogger(l Logger) *Client {
	newClient := c.Clone()
	newClient.logger = l
	return newClient
}

// WithModifiers returns a clone of c that applies mods to every
// outgoing request before the middleware chain.
func (c *Client) WithModifiers(mods ...RequestModifier) *Client {
	newClient := c.Clone()
	newClient.defaultMods = append(newClient.defaultMods, mods...)
	return newClient
}

// WithMultiReadBody returns a [RequestModifier] that overrides the
// body caching threshold for a single request. Responses smaller
// than threshold are buffered in memory so the body can be read
// multiple times. A value <= 0 disables caching for the request.
func WithMultiReadBody(threshold int64) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), multiReadCtxKey{}, threshold)
		*req = *req.WithContext(ctx)
	}
}

// WithBaseResponse returns a clone of c that uses provider to create
// [BaseResponse] wrappers for structured decoding. Pass nil to clear.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := c.Clone()
	newClient.baseResponse = provider
	return newClient
}

// WithBaseURL returns a clone of c that resolves relative paths in
// [Client.Request] against raw. An empty string clears the base URL.
// If raw is not a valid URL, the original client is returned unchanged.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		newClient := c.Clone()
		newClient.baseURL = &url.URL{}
		return newClient
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, err := url.Parse(raw)
	if err != nil {
		return c
	}

	newClient := c.Clone()
	newClient.baseURL = baseURL

	return newClient
}

// WithHeader returns a clone of c with key set to value on every
// outgoing request. Overwrites any existing value for key.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := c.Clone()
	newClient.headers.Set(key, value)
	return newClient
}

// WithTimeout returns a clone of c whose requests time out after d.
// Only works when the underlying [HTTPDoer] is an [http.Client].
// A duration <= 0 means no timeout.
func (c *Client) WithTimeout(d time.Duration) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Timeout = d
		newClient.http = &cloned
	}

	return newClient
}

// WithBrowserProfile configures both the TLS fingerprint, matching HTTP/2 framed settings,
// and default browser headers (like User-Agent, Sec-Ch-Ua, and Accept orders) in a single call.
// This prevents fingerprint mismatches between TLS and HTTP/2 layers.
// Use [WithH2FramedTransport] and [WithUserAgent] to configure HTTP/2 settings and User-Agent separately.
func (c *Client) WithBrowserProfile(browser BrowserID, os profiles.OSKey) *Client {
	newClient := c.WithTLSFingerprint(browser)

	var (
		h2Settings HTTP2Settings
		ua         string
	)

	switch browser {
	case BrowserFirefox:
		// Use Firefox presets
		h2Settings = HTTP2Settings{
			HeaderTableSize:   65536,
			EnablePush:        0,
			InitialWindowSize: 131072,
			MaxFrameSize:      16384,
			ConnectionFlow:    12517377,
			PriorityWeight:    41,
		}

		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0"
		if os.IsMobile() {
			ua = "Mozilla/5.0 (Android 16; Mobile; rv:148.0) Gecko/148.0 Firefox/148.0"
		}

	default:
		// Default to Chrome presets
		h2Settings = HTTP2Settings{
			HeaderTableSize:   65536,
			EnablePush:        0,
			InitialWindowSize: 6291456,
			MaxHeaderListSize: 262144,
			ConnectionFlow:    15663105,
			PriorityWeight:    255,
			PriorityExclusive: true,
		}
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	}

	newClient = newClient.WithH2FramedTransport(h2Settings)
	newClient = newClient.WithUserAgent(ua)

	return newClient
}

// WithRedirectLimit returns a clone of c that stops following
// redirects after max. A value of 0 disables redirects entirely.
// A negative value restores Go's default behavior (10 hops).
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

// WithLocalAddr returns a clone of c that binds outgoing connections
// to addr. The local address is only used when its IP family
// (v4/v6) matches the target's family. Ignored when the underlying
// [HTTPDoer] is not an [http.Client] with an [http.Transport].
func (c *Client) WithLocalAddr(addr string) *Client {
	newClient := c.Clone()
	if transport := newClient.Transport(); transport != nil {
		localAddr, err := net.ResolveIPAddr("ip", addr)
		if err == nil {
			prevDial := transport.DialContext
			transport.DialContext = func(ctx context.Context, network, raddr string) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}

				// Only bind local address if IP families match to avoid EAFNOSUCE.
				host, _, splitErr := net.SplitHostPort(raddr)
				if splitErr == nil {
					if targetIP := net.ParseIP(host); targetIP != nil {
						localIsV4 := localAddr.IP.To4() != nil

						targetIsV4 := targetIP.To4() != nil
						if localIsV4 == targetIsV4 {
							dialer.LocalAddr = &net.TCPAddr{IP: localAddr.IP}
						}
					}
				}

				if prevDial != nil {
					return prevDial(ctx, network, raddr)
				}

				return dialer.DialContext(ctx, network, raddr)
			}
		}
	}

	return newClient
}

// WithHedging returns a clone of c that dispatches a second request
// after d if the first has not completed. A duration <= 0 disables
// hedging.
func (c *Client) WithHedging(d time.Duration) *Client {
	newClient := c.Clone()
	newClient.hedgingDelay = d
	return newClient
}

// WithDynamicHedging returns a clone of c that computes the hedging
// delay dynamically from the p95 RTT of recent requests. When config
// is nil, [DefaultDynamicHedgingConfig] values are used.
func (c *Client) WithDynamicHedging(config *DynamicHedgingConfig) *Client {
	newClient := c.Clone()
	if config == nil {
		cfg := DefaultDynamicHedgingConfig()
		newClient.dynamicHedging = &cfg
	} else {
		newClient.dynamicHedging = config
	}

	return newClient
}

// WithProxyAwareSessionCache returns a clone of c that resumes TLS
// sessions via a [ProxyAwareSessionCache]. The cache is invalidated
// automatically when the proxy or source IP changes, preventing
// servers from correlating sessions across different exit nodes.
func (c *Client) WithProxyAwareSessionCache() *Client {
	newClient := c.Clone()
	newClient.sessionCache = NewProxyAwareSessionCache()
	return newClient
}

// WithPacketPadding returns a clone of c that constrains TCP MSS and
// adds random padding headers to disrupt DPI length analysis. See
// [PaddingConfig] for available fields.
func (c *Client) WithPacketPadding(cfg PaddingConfig) *Client {
	newClient := c.Clone()
	newClient.packetPadding = &cfg
	newClient.applyDialers()

	return newClient
}

// WithMaxResponseSize returns a clone of c that rejects response
// bodies larger than size bytes. A value <= 0 removes the limit.
func (c *Client) WithMaxResponseSize(size int64) *Client {
	newClient := c.Clone()
	newClient.maxResponseSize = size
	return newClient
}

// WithSSRFGuard returns a clone of c that blocks requests resolving
// to private or loopback IP addresses. Returns [ErrSSRFBlocked]
// from [Client.Request] when triggered.
func (c *Client) WithSSRFGuard() *Client {
	newClient := c.Clone()
	newClient.ssrfGuard = true
	newClient.applyDialers()

	return newClient
}

// WithHappyEyeballs returns a clone of c that staggers parallel
// connection attempts by delay per address. A duration <= 0
// disables staggering and tries all addresses simultaneously.
func (c *Client) WithHappyEyeballs(delay time.Duration) *Client {
	newClient := c.Clone()
	newClient.happyEyeballsDelay = delay
	newClient.applyDialers()

	return newClient
}

// WithMultiReadBody returns a clone of c that caches response bodies
// smaller than threshold bytes so they can be re-read. A value <= 0
// disables caching.
func (c *Client) WithMultiReadBody(threshold int64) *Client {
	newClient := c.Clone()
	newClient.multiReadThreshold = threshold
	return newClient
}

// WithLocalAddrPool returns a clone of c that round-robins source IP
// addresses from addrs. Each outgoing connection binds to the next
// address in the pool. Invalid addresses are silently ignored.
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

// WithDNSResolver returns a clone of c that resolves hostnames
// through resolver instead of the system resolver.
func (c *Client) WithDNSResolver(resolver DNSResolver) *Client {
	newClient := c.Clone()
	newClient.dnsResolver = resolver
	newClient.applyDialers()

	return newClient
}

// WithDoT returns a clone of c that resolves DNS via
// DNS-over-TLS using endpoint as the resolver address and host as
// the TLS server name. See [NewDoTResolver].
func (c *Client) WithDoT(endpoint, host string) *Client {
	return c.WithDNSResolver(NewDoTResolver(endpoint, host))
}

// WithDoH returns a clone of c that resolves DNS via
// DNS-over-HTTPS using endpoint as the resolver URL and host as
// the HTTP Host header. See [NewDoHResolver].
func (c *Client) WithDoH(endpoint, host string) *Client {
	return c.WithDNSResolver(NewDoHResolver(endpoint, host))
}

// WithBeforeRequest returns a clone of c that calls hook before
// every request. Hooks execute in registration order.
func (c *Client) WithBeforeRequest(hook func(req *http.Request)) *Client {
	newClient := c.Clone()
	newClient.beforeRequest = append(newClient.beforeRequest, hook)
	return newClient
}

// WithAfterResponse returns a clone of c that calls hook after every
// request, regardless of success or failure.
func (c *Client) WithAfterResponse(hook func(resp *http.Response, err error)) *Client {
	newClient := c.Clone()
	newClient.afterResponse = append(newClient.afterResponse, hook) //nolint:bodyclose
	return newClient
}

// WithUserAgent returns a clone of c that sends ua as the
// User-Agent header on every request.
func (c *Client) WithUserAgent(ua string) *Client {
	return c.WithHeader("User-Agent", ua)
}

// UserAgent returns the User-Agent header configured on c.
func (c *Client) UserAgent() string {
	return c.headers.Get("User-Agent")
}

// WithOrigin returns a clone of c that sends origin as the Origin
// header on every request.
func (c *Client) WithOrigin(origin string) *Client {
	return c.WithHeader("Origin", origin)
}

// WithBearer returns a clone of c that sends token as a Bearer
// Authorization header on every request.
func (c *Client) WithBearer(token string) *Client {
	return c.WithHeader("Authorization", "Bearer "+token)
}

// WithBasicAuth returns a clone of c that sends Basic authentication
// credentials on every request.
func (c *Client) WithBasicAuth(username, password string) *Client {
	return c.WithHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
}

// WithCookieJar returns a clone of c that stores and sends cookies
// through jar. Only effective when the underlying [HTTPDoer] is an
// [http.Client].
func (c *Client) WithCookieJar(jar http.CookieJar) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Jar = jar
		newClient.http = &cloned
	}

	return newClient
}

// WithConnectionPool returns a clone of c with the transport pool
// tuned to cfg. Fields left at zero keep the existing transport
// settings. Only effective when the underlying [HTTPDoer] is an
// [http.Client] with an [http.Transport].
func (c *Client) WithConnectionPool(cfg ConnectionPoolConfig) *Client {
	newClient := c.Clone()
	if transport := newClient.Transport(); transport != nil {
		transport.MaxIdleConns = generic.Coalesce(cfg.MaxIdleConns, transport.MaxIdleConns)
		transport.MaxIdleConnsPerHost = generic.Coalesce(cfg.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
		transport.MaxConnsPerHost = generic.Coalesce(cfg.MaxConnsPerHost, transport.MaxConnsPerHost)
		transport.IdleConnTimeout = generic.Coalesce(cfg.IdleConnTimeout, transport.IdleConnTimeout)
		transport.ResponseHeaderTimeout = generic.Coalesce(cfg.ResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	}

	return newClient
}

// WithTLSFingerprint returns a clone of c that uses uTLS to emulate
// a browser's TLS ClientHello. [BrowserNone] disables emulation.
// Only effective when the underlying [HTTPDoer] is an [http.Client]
// with an [http.Transport].
func (c *Client) WithTLSFingerprint(browser BrowserID) *Client {
	newClient := c.Clone()
	if browser == BrowserNone {
		return newClient
	}

	// Store browser ID on the Client so it works with any HTTPDoer type
	// (ProxyRotator, LoadBalancer, etc.), not just *http.Client.
	newClient.tlsBrowserID = browser

	if transport := newClient.Transport(); transport != nil {
		callback := newClient.ja4Callback
		tlsConfig := transport.TLSClientConfig
		proxyFn := transport.Proxy
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var proxyURL *url.URL
			if proxyFn != nil {
				proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
			}

			return dialTLSWithUTLS(
				ctx,
				network,
				addr,
				browser,
				newClient.sourceRotator,
				newClient.dnsResolver,
				callback,
				tlsConfig,
				proxyURL,
			)
		}
	}

	return newClient
}

// WithJA4Callback returns a clone of c that calls fn after each TLS
// handshake with the [ja4.Report]. Requires [Client.WithTLSFingerprint]
// to be enabled.
func (c *Client) WithJA4Callback(fn func(ja4.Report)) *Client {
	newClient := c.Clone()
	newClient.ja4Callback = fn
	return newClient
}

// WithFragmentation returns a clone of c that splits the TLS
// ClientHello across multiple TCP segments according to cfg. See
// [FragmentConfig].
func (c *Client) WithFragmentation(cfg FragmentConfig) *Client {
	newClient := c.Clone()
	newClient.fragmentConfig = &cfg
	return newClient
}

// WithHostRewrite returns a clone of c that resolves hostnames in
// requests according to rules (host -> ip), while keeping the
// original hostname for TLS SNI.
func (c *Client) WithHostRewrite(rules map[string]string) *Client {
	newClient := c.Clone()
	newClient.hostRewrite = &HostRewriteConfig{Rules: rules}
	return newClient
}

// WithProxyIsolatedCookieJar returns a clone of c that stores
// cookies per proxy URL in jar, preventing cross-proxy session
// leakage. See [ProxyIsolatedCookieJar].
func (c *Client) WithProxyIsolatedCookieJar(jar *ProxyIsolatedCookieJar) *Client {
	newClient := c.Clone()
	newClient.headersCookieJar = jar

	if httpClient, ok := newClient.http.(*http.Client); ok {
		clonedHTTP := *httpClient

		baseTransport := clonedHTTP.Transport
		if baseTransport == nil {
			baseTransport = http.DefaultTransport
		}

		// Unwrap existing cookieJarTransport to avoid stacking wrappers.
		if cjTrans, ok := baseTransport.(*cookieJarTransport); ok {
			baseTransport = cjTrans.next
		}

		clonedHTTP.Transport = &cookieJarTransport{
			next:      baseTransport,
			cookieJar: jar,
		}
		newClient.http = &clonedHTTP
	}

	return newClient
}

// WithDNSCache returns a clone of c that caches DNS results for ttl.
// The cache wraps the current DNS resolver.
func (c *Client) WithDNSCache(ttl time.Duration) *Client {
	newClient := c.Clone()
	newClient.dnsResolver = NewInMemoryDNSCache(ttl, c.dnsResolver)
	return newClient
}

// WithHTTP2Settings returns a clone of c with custom HTTP/2
// connection parameters. These values are stored on the client
// but only take effect when [Client.WithH2FramedTransport] is also
// configured.
func (c *Client) WithHTTP2Settings(settings HTTP2Settings) *Client {
	newClient := c.Clone()
	newClient.h2Settings = &settings
	return newClient
}

// WithH2FramedTransport returns a clone of c that injects browser-
// specific SETTINGS and PRIORITY frames into the HTTP/2 connection
// preface. This makes the HTTP/2 fingerprint match the TLS profile
// set by [Client.WithTLSFingerprint].
func (c *Client) WithH2FramedTransport(settings HTTP2Settings) *Client {
	newClient := c.Clone()
	newClient.h2Settings = &settings

	if transport := newClient.Transport(); transport != nil {
		framed := NewH2FramedTransport(transport, settings)
		if httpClient, ok := newClient.http.(*http.Client); ok {
			httpClient.Transport = framed
		}
	}

	return newClient
}

// WithProfileH2Settings returns a clone of c with HTTP/2 settings
// extracted from s. See [H2SettingsFromProfile].
func (c *Client) WithProfileH2Settings(s profiles.H2Settings) *Client {
	settings := H2SettingsFromProfile(s)
	return c.WithH2FramedTransport(settings)
}

// WithP0fSignature returns a clone of c that spoofs TCP/IP fields
// (TTL, Don't Fragment, window size) to match sig. The spoofing is
// applied via Dialer.Control before the SYN packet is sent, making
// the connection appear as the OS described by sig to passive
// fingerprinters such as p0f.
func (c *Client) WithP0fSignature(sig *p0f.Signature) *Client {
	newClient := c.Clone()
	newClient.p0fSignature = sig
	return newClient
}

// WithProxyDNS returns a clone of c that resolves hostnames through
// the configured SOCKS5 or HTTP CONNECT proxy instead of the local
// system resolver. This prevents the local ISP from observing DNS
// queries.
func (c *Client) WithProxyDNS() *Client {
	newClient := c.Clone()
	newClient.proxyDNS = true
	newClient.applyDialers()

	return newClient
}

// WithProxy returns a clone of c configured to route requests through proxyURL.
// Supported schemes: http, socks5, socks5h (for remote DNS resolution).
// When proxyURL is nil, proxy routing is disabled.
func (c *Client) WithProxy(proxyURL *url.URL) *Client {
	newClient := c.Clone()
	newClient.proxyAddr = proxyURL

	if proxyURL != nil {
		newClient.transportProxy = http.ProxyURL(proxyURL)
	}

	newClient.applyDialers()

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
	proxyURL *url.URL,
) (net.Conn, error) {
	// Read callback from context (set by Client.Request) — the closure-captured
	// value may be stale if WithJA4Callback was called after WithTLSFingerprint.
	if cb, ok := ctx.Value(ja4CallbackCtxKey{}).(func(ja4.Report)); ok && cb != nil {
		ja4Callback = cb
	}

	ssrfGuard := ctx.Value(ssrfGuardCtxKey{}) != nil

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	var delay time.Duration
	if val := ctx.Value(happyEyeballsDelayCtxKey{}); val != nil {
		delay = val.(time.Duration)
	} else {
		delay = 300 * time.Millisecond
	}

	// Route through proxy if configured — prevents direct IP leak.
	var conn net.Conn
	if proxyURL != nil {
		conn, err = dialViaProxy(ctx, network, host, port, proxyURL)
	} else {
		conn, err = happyEyeballsDial(ctx, network, addr, delay, ssrfGuard, sourceRotator, dnsResolver)
	}

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

	uConfig := &utls.Config{
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	}

	if tlsConfig != nil {
		uConfig.InsecureSkipVerify = tlsConfig.InsecureSkipVerify
		uConfig.RootCAs = tlsConfig.RootCAs
		uConfig.MinVersion = tlsConfig.MinVersion
		uConfig.MaxVersion = tlsConfig.MaxVersion
		uConfig.CipherSuites = tlsConfig.CipherSuites

		if len(tlsConfig.CurvePreferences) > 0 {
			uConfig.CurvePreferences = make([]utls.CurveID, len(tlsConfig.CurvePreferences))
			for i, id := range tlsConfig.CurvePreferences {
				uConfig.CurvePreferences[i] = utls.CurveID(id)
			}
		}
	}

	// Use proxy-aware session cache if available in context.
	if cache, ok := ctx.Value(sessionCacheCtxKey{}).(*ProxyAwareSessionCache); ok && cache != nil {
		uConfig.ClientSessionCache = cache
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
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}

	// Check private IP ranges.
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 0 ||
			ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}

	if ip6 := ip.To16(); ip6 != nil {
		// Check unique local IPv6.
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}

// wrapConn applies connection-level wrappers (MSS limiting, fragmentation,
// header ordering) based on the request context. It is called after dialing
// a TCP connection, before any TLS handshake.
func wrapConn(ctx context.Context, conn net.Conn) net.Conn {
	if cfg, ok := ctx.Value(packetPaddingCtxKey{}).(*PaddingConfig); ok && cfg != nil &&
		cfg.MaxSegmentSize > 0 {
		conn = wrapWithMSSLimit(conn, cfg.MaxSegmentSize)
	}

	if cfg, ok := ctx.Value(fragmentCtxKey{}).(FragmentConfig); ok && cfg.ChunkSize > 0 {
		conn = wrapWithFragmentation(conn, cfg)
	}

	if order, ok := ctx.Value(orderedHeadersCtxKey{}).([]string); ok && len(order) > 0 {
		conn = &headerOrderingConn{Conn: conn, orderedKeys: order}
	}

	return conn
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

	// Proxy DNS: route DNS resolution through the proxy to prevent local DNS leaks.
	if _, ok := ctx.Value(proxyDNSCtxKey{}).(bool); ok {
		proxyURL, _ := ctx.Value(proxyAddrCtxKey{}).(*url.URL)
		if proxyURL != nil && net.ParseIP(host) == nil {
			return dialViaProxy(ctx, network, host, port, proxyURL)
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

		// Apply p0f spoofing BEFORE the SYN packet is sent, via Dialer.Control.
		if cfg, ok := ctx.Value(p0fSignatureCtxKey{}).(*p0f.Signature); ok && cfg != nil {
			spoofer := p0f.NewSpoofer(cfg)
			dialer.Control = func(network, address string, c syscall.RawConn) error {
				return spoofer.ApplyToRawConn(c)
			}
		}

		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(filtered[0].String(), port))
		if err != nil {
			return nil, err
		}

		return wrapConn(ctx, conn), nil
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

			// Apply p0f spoofing BEFORE the SYN packet, via Dialer.Control.
			if cfg, ok := dialCtx.Value(p0fSignatureCtxKey{}).(*p0f.Signature); ok && cfg != nil {
				spoofer := p0f.NewSpoofer(cfg)
				dialer.Control = func(network, address string, c syscall.RawConn) error {
					return spoofer.ApplyToRawConn(c)
				}
			}

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
				return wrapConn(ctx, res.conn), nil
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

// dialViaProxy connects to a target host through a SOCKS5 proxy, performing DNS
// resolution on the proxy side to prevent local DNS leaks. For HTTP CONNECT
// proxies, the proxy resolves the hostname when handling the CONNECT request.
func dialViaProxy(ctx context.Context, network, host, port string, proxyURL *url.URL) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyAddr == "" {
		return nil, errors.New("aoni: proxy DNS enabled but proxy address is empty")
	}

	if net.ParseIP(proxyAddr) == nil {
		// proxyAddr may not have a port, default to 1080 for SOCKS5
		if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
			proxyAddr = net.JoinHostPort(proxyAddr, "1080")
		}
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}

	proxyConn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("aoni: dial proxy %s: %w", proxyAddr, err)
	}

	// Set a deadline for the entire handshake phase to prevent goroutine leaks.
	handshakeDeadline := time.Now().Add(30 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}

	if err := proxyConn.SetDeadline(handshakeDeadline); err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: set proxy handshake deadline: %w", err)
	}

	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		if err := socks5Handshake(proxyConn, host, port, proxyURL); err != nil {
			_ = proxyConn.Close()
			return nil, err
		}

		_ = proxyConn.SetDeadline(time.Time{})

		return proxyConn, nil
	}

	// HTTP CONNECT proxy: send CONNECT and let the proxy resolve DNS.
	connectReq := fmt.Sprintf("CONNECT %s:%s HTTP/1.1\r\nHost: %s:%s\r\n\r\n",
		host, port, host, port)
	if _, err := proxyConn.Write([]byte(connectReq)); err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: send CONNECT to proxy: %w", err)
	}

	br := bufio.NewReader(proxyConn)

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: read CONNECT response: %w", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: CONNECT rejected with status %s", resp.Status)
	}

	_ = proxyConn.SetDeadline(time.Time{})

	// If bufio.Reader buffered data beyond the HTTP response, wrap the
	// connection so the leftover bytes are returned before real network data.
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: proxyConn, r: br}, nil
	}

	return proxyConn, nil
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that leftover bytes
// buffered during HTTP response parsing are returned before real network data.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(b)
	}

	return c.Conn.Read(b)
}

// socks5Handshake performs the SOCKS5 protocol handshake with remote DNS resolution.
func socks5Handshake(conn net.Conn, host, port string, proxyURL *url.URL) error {
	// Step 1: Greeting — offer both NO AUTH and USERNAME/PASSWORD when credentials exist.
	greeting := []byte{0x05, 0x01, 0x00} // VER=5, NMETHODS=1, NO AUTH
	if proxyURL.User != nil {
		greeting = []byte{0x05, 0x02, 0x00, 0x02} // VER=5, NMETHODS=2, [NO AUTH, USERNAME/PASSWORD]
	}

	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("aoni: socks5 greeting: %w", err)
	}

	// Step 2: Server choice
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("aoni: socks5 read choice: %w", err)
	}

	if resp[0] != 0x05 {
		return fmt.Errorf("aoni: socks5 unsupported version: %d", resp[0])
	}

	// Step 3: Authentication
	switch resp[1] {
	case 0x02: // Username/Password
		if proxyURL.User == nil {
			return errors.New("aoni: socks5 server requires auth but no credentials provided")
		}

		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()

		if len(username) > 255 || len(password) > 255 {
			return errors.New("aoni: socks5 auth credentials exceed 255 byte limit")
		}

		auth := make([]byte, 0, 2+len(username)+1+len(password))
		auth = append(auth, 0x01, byte(len(username))) //nolint:gosec
		auth = append(auth, []byte(username)...)
		auth = append(auth, byte(len(password))) //nolint:gosec
		auth = append(auth, []byte(password)...)

		if _, err := conn.Write(auth); err != nil {
			return fmt.Errorf("aoni: socks5 auth write: %w", err)
		}

		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			return fmt.Errorf("aoni: socks5 auth read: %w", err)
		}

		if authResp[1] != 0x00 {
			return fmt.Errorf("aoni: socks5 auth failed: status %d", authResp[1])
		}

	case 0x00: // No auth required
	default:
		return fmt.Errorf("aoni: socks5 unsupported auth method: %d", resp[1])
	}

	// Step 4: Connection request (remote DNS - ATYP=0x03 domain name)
	if len(host) > 255 {
		return fmt.Errorf("aoni: socks5 hostname exceeds 255 bytes: %s", host)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 0 || portNum > 65535 {
		return fmt.Errorf("aoni: socks5 invalid port: %s", port)
	}

	req := make([]byte, 0, 5+len(host)+2)
	req = append(req, 0x05, 0x01, 0x00, 0x03) //nolint:gosec // VER=5, CMD=CONNECT, RSV=0, ATYP=DOMAIN
	req = append(req, byte(len(host)))        //nolint:gosec
	req = append(req, []byte(host)...)
	req = append(req, byte(portNum>>8), byte(portNum)) //nolint:gosec

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("aoni: socks5 connect request: %w", err)
	}

	// Step 5: Read connect reply
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("aoni: socks5 connect reply: %w", err)
	}

	if reply[1] != 0x00 {
		return fmt.Errorf("aoni: socks5 connect failed: code %d", reply[1])
	}

	// Skip the rest of the reply (bind addr + bind port)
	switch reply[3] {
	case 0x01: // IPv4
		_, _ = io.CopyN(io.Discard, conn, 4+2)
	case 0x03: // Domain
		domainLen := make([]byte, 1)
		if _, err := io.ReadFull(conn, domainLen); err != nil {
			return fmt.Errorf("aoni: socks5 read domain length: %w", err)
		}

		_, _ = io.CopyN(io.Discard, conn, int64(domainLen[0])+2)

	case 0x04: // IPv6
		_, _ = io.CopyN(io.Discard, conn, 16+2)
	}

	return nil
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

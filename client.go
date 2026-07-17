// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
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

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
	"github.com/lemon4ksan/aoni/profiles"
)

// ClientHelloSpecProvider defines an interface that returns a uTLS ClientHelloSpec.
// Implementing this interface allows developers to feed custom/dynamic TLS fingerprints
// directly to the client at runtime.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// TrafficInspector defines the interface for capturing and logging request trace history.
type TrafficInspector interface {
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *TraceInfo)
}

// SocketController defines a hook callback interface to directly intercept and configure
// TCP sockets (file descriptors) at the dial phase before the SYN packet is sent.
type SocketController interface {
	Control(fd uintptr, network, address string) error
}

// HTTP2Configurer defines an interface to customize the golang.org/x/net/http2.Transport instance.
// This allows advanced developers to adjust HPACK dynamic table size, enable/disable compression,
// or customize the encoder settings without modifying the core library.
type HTTP2Configurer interface {
	ConfigureHTTP2(t *http2.Transport) error
}

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
	capturerCtxKey                struct{}
	decoderCtxKey                 struct{}
	errorModelCtxKey              struct{}
	downloadProgressCtxKey        struct{}
	hedgingCtxKey                 struct{}
	queryErrorCtxKey              struct{}
	bodyErrorCtxKey               struct{}
	happyEyeballsDelayCtxKey      struct{}
	multiReadCtxKey               struct{}
	multiReadDisableDiskCtxKey    struct{}
	ssrfGuardCtxKey               struct{}
	fallbackCtxKey                struct{}
	debugCtxKey                   struct{}
	orderedHeadersCtxKey          struct{}
	ja4ReportCtxKey               struct{}
	ja4CallbackCtxKey             struct{}
	alpnOverrideCtxKey            struct{}
	p0fSignatureCtxKey            struct{}
	proxyDNSCtxKey                struct{}
	proxyAddrCtxKey               struct{}
	sessionCacheCtxKey            struct{}
	packetPaddingCtxKey           struct{}
	requestTimeoutCancelCtxKey    struct{}
	socketControllerCtxKey        struct{}
	clientHelloSpecProviderCtxKey struct{}
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
// participate in [GetTo] and similar generic request helpers. The
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

// RefererState holds the thread-safe state for the Referer tracking automaton.
type RefererState struct {
	mu      sync.Mutex
	lastURL string
}

// NetworkConfig groups all settings related to the network layer, such as
// proxying, DNS resolution, SSRF protection, IP rotation, connection delays,
// request hedging, connection control hooks, packet fragmentation, and host rewrites.
type NetworkConfig struct {
	// ProxyDNS controls whether DNS resolution is routed through the SOCKS5
	// or HTTP CONNECT proxy to prevent local DNS queries from leaking to the local ISP.
	ProxyDNS bool

	// ProxyAddr is the URL of the proxy server to route all traffic through.
	// Supports http, socks5, and socks5h schemes.
	ProxyAddr *url.URL

	// TransportProxy is the proxy resolution function used by the transport.
	// Typically returns ProxyAddr.
	TransportProxy func(*http.Request) (*url.URL, error)

	// DNSResolver is the custom resolver used to resolve hostnames.
	// If nil, the system default net.Resolver is used.
	DNSResolver DNSResolver

	// SSRFGuard blocks requests that resolve to private or loopback IP addresses.
	SSRFGuard bool

	// HappyEyeballsDelay staggers parallel IPv4/IPv6 dial attempts to minimize latency.
	// A duration <= 0 disables staggering.
	HappyEyeballsDelay time.Duration

	// SourceRotator manages a pool of local IP addresses
	// to bind outgoing connections to in a round-robin fashion.
	SourceRotator *SourceIPRotator

	// HedgingDelay defines the delay before a second,
	// parallel request is dispatched for a slow request.
	// A duration <= 0 disables request hedging.
	HedgingDelay time.Duration

	// DynamicHedging configures dynamic request hedging
	// based on the p95 RTT of recent successful requests.
	DynamicHedging *DynamicHedgingConfig

	// SocketController hook is executed on every raw TCP connection
	// right after it is dialed, before any TLS handshake.
	SocketController SocketController

	// FragmentConfig specifies the configuration for splitting
	// TLS ClientHello packets across TCP segments.
	FragmentConfig *FragmentConfig

	// HostRewrite contains custom DNS rules mapping
	// specific hostnames to target IP addresses.
	HostRewrite *HostRewriteConfig
}

// FingerprintConfig groups all settings related to browser TLS and HTTP/2/3 fingerprint emulation,
// JA4 fingerprint tracking, header order serialization, session caching, and TCP packet padding.
type FingerprintConfig struct {
	// BrowserID selects a pre-configured uTLS ClientHello profile for TLS fingerprint emulation.
	BrowserID BrowserID

	// TLSClientHelloID is a specific, low-level uTLS ClientHello ID to use instead of a generic BrowserID.
	TLSClientHelloID *utls.ClientHelloID

	// TLSClientHelloSpecProvider dynamically provides custom ClientHelloSpecs at handshake time.
	TLSClientHelloSpecProvider ClientHelloSpecProvider

	// H2Configurer allows manual tuning of HTTP/2 settings on the transport level.
	H2Configurer HTTP2Configurer

	// HeaderOrder defines the exact sequence in which HTTP headers should be written to the wire.
	HeaderOrder []string

	// JA4Callback is invoked with computed JA4 reports after a successful TLS handshake.
	JA4Callback func(ja4.Report)

	// P0fSignature spoofs TCP/IP parameters (window size, TTL) to match a specific operating system.
	P0fSignature *p0f.Signature

	// H2Settings overrides the default HTTP/2 SETTINGS frame parameters.
	H2Settings *HTTP2Settings

	// H3Settings overrides the default HTTP/3 (QUIC) configuration settings.
	H3Settings *HTTP3Settings

	// SessionCache is a proxy-aware TLS session ticket cache that prevents session correlation across proxies.
	SessionCache *ProxyAwareSessionCache

	// PacketPadding adjusts MSS and injects random padding headers to confuse DPI length analysis.
	PacketPadding *PaddingConfig
}

// ClientDefaults groups standard HTTP client settings, request/response lifecycle hooks,
// body buffering configs, error handlers, rate-limiting modifiers, WAF solvers, and debugger tools.
type ClientDefaults struct {
	// BaseURL is resolved against relative request paths in Client.Request.
	BaseURL *url.URL

	// Headers is the map of default HTTP headers sent with every request.
	Headers http.Header

	// BaseResponse is a factory function that returns a fresh instance of a custom response wrapper.
	BaseResponse func() BaseResponse

	// BeforeRequest hooks run sequentially on every outgoing request before the middleware chain.
	BeforeRequest []func(req *http.Request)

	// AfterResponse hooks run sequentially after every response (or error) is received.
	AfterResponse []func(resp *http.Response, err error)

	// MaxResponseSize restricts the maximum bytes allowed in a response body. A value <= 0 removes limits.
	MaxResponseSize int64

	// RefererAutomaton tracks and automatically injects Referer headers based on previous request URLs.
	RefererAutomaton bool

	// RefererState is the concurrent-safe state tracking the last visited URL for referer tracking.
	RefererState *RefererState

	// Logger is the diagnostic logger used by the client.
	Logger Logger

	// DefaultMods is a slice of RequestModifiers applied to every request prior to the middleware chain.
	DefaultMods []RequestModifier

	// ChallengeSolver solves JavaScript/WAF challenges (e.g., Cloudflare) on challenge detection.
	ChallengeSolver ChallengeSolver

	// ChallengeDetector determines if a response constitutes a challenge to be solved.
	ChallengeDetector ChallengeDetector

	// Inspector logs and exposes request trace history to a local developer dashboard.
	Inspector TrafficInspector

	// MultiReadThreshold determines the size limit (in bytes) under which response bodies are cached in memory for multiple reads.
	MultiReadThreshold int64

	// MultiReadDisableDisk prevents caching responses exceeding MultiReadThreshold to disk, returning errors instead.
	MultiReadDisableDisk bool

	// HeadersCookieJar is the cookie jar used for tracking cookie headers when running with custom cookie setups.
	HeadersCookieJar http.CookieJar

	// PipelineWrapper allows overriding or wrapping the default middleware pipeline.
	PipelineWrapper func(c *Client, engine HTTPDoer) HTTPDoer
}

// Client is an immutable, concurrency-safe HTTP client built on [HTTPDoer].
// Every With* method returns a new clone, so the original remains usable
// by other goroutines. Use [NewClient] to create the first instance.
type Client struct {
	http        HTTPDoer
	engine      HTTPDoer
	network     NetworkConfig
	fingerprint FingerprintConfig
	defaults    ClientDefaults
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
		engine: httpClient,
		defaults: ClientDefaults{
			BaseURL:         &url.URL{},
			Headers:         make(http.Header),
			MaxResponseSize: 10 * 1024 * 1024,
			RefererState:    &RefererState{},
		},
		network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
	}

	c.applyDialers()
	c.rebuildChain()

	return c.WithUserAgent(DefaultUserAgent)
}

// Clone returns a deep copy of c. The cloned client shares nothing
// mutable with the original - transport, cookie jar, and config
// structs are all independently copied.
func (c *Client) Clone() *Client {
	cloned := &Client{
		network:     c.network,
		fingerprint: c.fingerprint,
		defaults:    c.defaults,
	}

	if c.network.DynamicHedging != nil {
		dhCopy := *c.network.DynamicHedging
		cloned.network.DynamicHedging = &dhCopy
	}

	if c.network.FragmentConfig != nil {
		fragCopy := *c.network.FragmentConfig
		cloned.network.FragmentConfig = &fragCopy
	}

	if c.network.HostRewrite != nil && c.network.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(c.network.HostRewrite.Rules))
		maps.Copy(rulesCopy, c.network.HostRewrite.Rules)
		cloned.network.HostRewrite = &HostRewriteConfig{Rules: rulesCopy}
	}

	if c.fingerprint.TLSClientHelloID != nil {
		idCopy := *c.fingerprint.TLSClientHelloID
		cloned.fingerprint.TLSClientHelloID = &idCopy
	}

	if c.fingerprint.HeaderOrder != nil {
		orderCopy := make([]string, len(c.fingerprint.HeaderOrder))
		copy(orderCopy, c.fingerprint.HeaderOrder)
		cloned.fingerprint.HeaderOrder = orderCopy
	}

	if c.fingerprint.P0fSignature != nil {
		sigCopy := *c.fingerprint.P0fSignature
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

		cloned.fingerprint.P0fSignature = &sigCopy
	}

	if c.fingerprint.H2Settings != nil {
		h2Copy := *c.fingerprint.H2Settings
		cloned.fingerprint.H2Settings = &h2Copy
	}

	if c.fingerprint.H3Settings != nil {
		h3Copy := *c.fingerprint.H3Settings
		cloned.fingerprint.H3Settings = &h3Copy
	}

	if c.fingerprint.PacketPadding != nil {
		padCopy := *c.fingerprint.PacketPadding
		cloned.fingerprint.PacketPadding = &padCopy
	}

	cloned.defaults.Headers = c.defaults.Headers.Clone()

	beforeCopy := make([]func(req *http.Request), len(c.defaults.BeforeRequest))
	copy(beforeCopy, c.defaults.BeforeRequest)
	cloned.defaults.BeforeRequest = beforeCopy

	afterCopy := make([]func(resp *http.Response, err error), len(c.defaults.AfterResponse))
	copy(afterCopy, c.defaults.AfterResponse)
	cloned.defaults.AfterResponse = afterCopy

	if c.defaults.DefaultMods != nil {
		modsCopy := make([]RequestModifier, len(c.defaults.DefaultMods))
		copy(modsCopy, c.defaults.DefaultMods)
		cloned.defaults.DefaultMods = modsCopy
	}

	// Clone http.Client and its transport to avoid race conditions.
	// If the transport is wrapped in cookieJarTransport, unwrap, clone the
	// base transport, and re-wrap to preserve the cookie jar binding.
	cloned.engine = c.engine
	if httpClient, ok := cloned.engine.(*http.Client); ok {
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

		cloned.engine = &clonedHTTP
	}

	cloned.applyDialers()
	cloned.rebuildChain()

	return cloned
}

// Get performs a GET request through the Client and returns the raw http.Response.
func (c *Client) Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return Get(ctx, c, path, mods...)
}

// Post executes a POST request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Post(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Post(ctx, c, path, body, mods...)
}

// Put executes a PUT request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Put(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Put(ctx, c, path, body, mods...)
}

// Patch executes a PATCH request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Patch(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Patch(ctx, c, path, body, mods...)
}

// Delete executes a DELETE request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Delete(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Delete(ctx, c, path, body, mods...)
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
	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("aoni: invalid path: %w", err)
	}

	u := c.defaults.BaseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.defaults.Headers)

	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "zstd, br, gzip")
	}

	generic.ApplyOptions(req, c.defaults.DefaultMods...)
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

	resp, reqErr := c.http.Do(req)
	if reqErr != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", reqErr)
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
	newClient.defaults.Logger = l
	newClient.rebuildChain()

	return newClient
}

// WithModifiers returns a clone of c that applies mods to every
// outgoing request before the middleware chain.
func (c *Client) WithModifiers(mods ...RequestModifier) *Client {
	newClient := c.Clone()
	newClient.defaults.DefaultMods = append(newClient.defaults.DefaultMods, mods...)
	newClient.rebuildChain()

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

// WithMultiReadDisableDisk returns a [RequestModifier] that overrides the
// body caching disk-fallback setting for a single request. If disable is true,
// exceeding the memory threshold returns an error ([ErrBufferLimitExceeded]) instead of creating temporary files.
func WithMultiReadDisableDisk(disable bool) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), multiReadDisableDiskCtxKey{}, disable)
		*req = *req.WithContext(ctx)
	}
}

// WithBaseResponse returns a clone of c that uses provider to create
// [BaseResponse] wrappers for structured decoding. Pass nil to clear.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := c.Clone()
	newClient.defaults.BaseResponse = provider
	newClient.rebuildChain()

	return newClient
}

// WithBaseURL returns a clone of c that resolves relative paths in
// [Client.Request] against raw. An empty string clears the base URL.
// If raw is not a valid URL, the original client is returned unchanged.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		newClient := c.Clone()
		newClient.defaults.BaseURL = &url.URL{}
		newClient.rebuildChain()

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
	newClient.defaults.BaseURL = baseURL
	newClient.rebuildChain()

	return newClient
}

// WithHeader returns a clone of c with key set to value on every
// outgoing request. Overwrites any existing value for key.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := c.Clone()
	newClient.defaults.Headers.Set(key, value)
	newClient.rebuildChain()

	return newClient
}

// WithHeaders returns a clone of c with updated headers on every
// outgoing request. Overwrites any existing values for keys.
func (c *Client) WithHeaders(headers map[string]string) *Client {
	newClient := c.Clone()
	for k, v := range headers {
		newClient.defaults.Headers.Set(k, v)
	}

	newClient.rebuildChain()

	return newClient
}

// WithoutHeaders returns a clone of c with all headers removed.
func (c *Client) WithoutHeaders() *Client {
	newClient := c.Clone()
	newClient.defaults.Headers = make(http.Header)
	newClient.rebuildChain()

	return newClient
}

// WithTimeout returns a clone of c whose requests time out after d.
// Only works when the underlying [HTTPDoer] is an [http.Client].
// A duration <= 0 means no timeout.
func (c *Client) WithTimeout(d time.Duration) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.engine.(*http.Client); ok {
		cloned := *httpClient
		cloned.Timeout = d
		newClient.engine = &cloned
		newClient.rebuildChain()
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
		h3Settings HTTP3Settings
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
		h3Settings = FirefoxHTTP3Settings

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
		h3Settings = ChromeHTTP3Settings
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	}

	newClient = newClient.WithH2FramedTransport(h2Settings)
	newClient.fingerprint.H3Settings = &h3Settings
	newClient.rebuildChain()
	newClient = newClient.WithUserAgent(ua)

	return newClient
}

// WithRedirectLimit returns a clone of c that stops following
// redirects after max. A value of 0 disables redirects entirely.
// A negative value restores Go's default behavior (10 hops).
func (c *Client) WithRedirectLimit(max int) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.engine.(*http.Client); ok {
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

		newClient.engine = &cloned
		newClient.rebuildChain()
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
	newClient.network.HedgingDelay = d
	newClient.rebuildChain()

	return newClient
}

// WithDynamicHedging returns a clone of c that computes the hedging
// delay dynamically from the p95 RTT of recent requests. When config
// is nil, [DefaultDynamicHedgingConfig] values are used.
func (c *Client) WithDynamicHedging(config *DynamicHedgingConfig) *Client {
	newClient := c.Clone()
	if config == nil {
		cfg := DefaultDynamicHedgingConfig()
		newClient.network.DynamicHedging = &cfg
	} else {
		newClient.network.DynamicHedging = config
	}

	newClient.rebuildChain()

	return newClient
}

// WithProxyAwareSessionCache returns a clone of c that resumes TLS
// sessions via a [ProxyAwareSessionCache]. The cache is invalidated
// automatically when the proxy or source IP changes, preventing
// servers from correlating sessions across different exit nodes.
func (c *Client) WithProxyAwareSessionCache() *Client {
	newClient := c.Clone()
	newClient.fingerprint.SessionCache = NewProxyAwareSessionCache()
	newClient.rebuildChain()

	return newClient
}

// WithPacketPadding returns a clone of c that constrains TCP MSS and
// adds random padding headers to disrupt DPI length analysis. See
// [PaddingConfig] for available fields.
func (c *Client) WithPacketPadding(cfg PaddingConfig) *Client {
	newClient := c.Clone()
	newClient.fingerprint.PacketPadding = &cfg
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithMaxResponseSize returns a clone of c that rejects response
// bodies larger than size bytes. A value <= 0 removes the limit.
func (c *Client) WithMaxResponseSize(size int64) *Client {
	newClient := c.Clone()
	newClient.defaults.MaxResponseSize = size
	newClient.rebuildChain()

	return newClient
}

// WithSSRFGuard returns a clone of c that blocks requests resolving
// to private or loopback IP addresses. Returns [ErrSSRFBlocked]
// from [Client.Request] when triggered.
func (c *Client) WithSSRFGuard() *Client {
	newClient := c.Clone()
	newClient.network.SSRFGuard = true
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithHappyEyeballs returns a clone of c that staggers parallel
// connection attempts by delay per address. A duration <= 0
// disables staggering and tries all addresses simultaneously.
func (c *Client) WithHappyEyeballs(delay time.Duration) *Client {
	newClient := c.Clone()
	newClient.network.HappyEyeballsDelay = delay
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithMultiReadBody returns a clone of c that configures multiReadThreshold.
func (c *Client) WithMultiReadBody(threshold int64) *Client {
	newClient := c.Clone()
	newClient.defaults.MultiReadThreshold = threshold
	newClient.rebuildChain()

	return newClient
}

// WithMultiReadDisableDisk returns a clone of c that configures whether
// caching beyond the threshold is allowed to fallback to disk. If true,
// exceeding the memory threshold returns an error ([ErrBufferLimitExceeded]) instead of creating temporary files.
func (c *Client) WithMultiReadDisableDisk(disable bool) *Client {
	newClient := c.Clone()
	newClient.defaults.MultiReadDisableDisk = disable
	newClient.rebuildChain()

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
	newClient.network.SourceRotator = rotator
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithDNSResolver returns a clone of c that resolves hostnames
// through resolver instead of the system resolver.
func (c *Client) WithDNSResolver(resolver DNSResolver) *Client {
	newClient := c.Clone()
	newClient.network.DNSResolver = resolver
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// Inspector returns the configured [TrafficInspector] if enabled.
func (c *Client) Inspector() TrafficInspector {
	return c.defaults.Inspector
}

// WithInspector returns a clone of c with the specified TrafficInspector.
func (c *Client) WithInspector(inspector TrafficInspector) *Client {
	newClient := c.Clone()
	newClient.defaults.Inspector = inspector
	newClient.rebuildChain()

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

// WithChallengeDetector returns a clone of c configured with the specified ChallengeDetector.
func (c *Client) WithChallengeDetector(detector ChallengeDetector) *Client {
	newClient := c.Clone()
	newClient.defaults.ChallengeDetector = detector
	newClient.rebuildChain()

	return newClient
}

// WithBeforeRequest returns a clone of c that calls hook before
// every request. Hooks execute in registration order.
func (c *Client) WithBeforeRequest(hook func(req *http.Request)) *Client {
	newClient := c.Clone()
	newClient.defaults.BeforeRequest = append(newClient.defaults.BeforeRequest, hook)
	newClient.rebuildChain()

	return newClient
}

// WithAfterResponse returns a clone of c that calls hook after every
// request, regardless of success or failure.
func (c *Client) WithAfterResponse(hook func(resp *http.Response, err error)) *Client {
	newClient := c.Clone()
	newClient.defaults.AfterResponse = append(newClient.defaults.AfterResponse, hook) //nolint:bodyclose
	newClient.rebuildChain()

	return newClient
}

// WithUserAgent returns a clone of c that sends ua as the
// User-Agent header on every request.
func (c *Client) WithUserAgent(ua string) *Client {
	return c.WithHeader("User-Agent", ua)
}

// UserAgent returns the User-Agent header configured on c.
func (c *Client) UserAgent() string {
	return c.defaults.Headers.Get("User-Agent")
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
	if httpClient, ok := newClient.engine.(*http.Client); ok {
		cloned := *httpClient
		cloned.Jar = jar
		newClient.engine = &cloned
		newClient.rebuildChain()
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
	newClient.fingerprint.BrowserID = browser

	if transport := newClient.Transport(); transport != nil {
		callback := newClient.fingerprint.JA4Callback
		tlsConfig := transport.TLSClientConfig
		proxyFn := transport.Proxy
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var proxyURL *url.URL
			if proxyFn != nil {
				proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
			}

			if newClient.fingerprint.TLSClientHelloSpecProvider != nil {
				ctx = context.WithValue(
					ctx,
					clientHelloSpecProviderCtxKey{},
					newClient.fingerprint.TLSClientHelloSpecProvider,
				)
			}

			return dialTLSWithUTLS(
				ctx,
				network,
				addr,
				browser,
				newClient.fingerprint.TLSClientHelloID,
				newClient.network.SourceRotator,
				newClient.network.DNSResolver,
				callback,
				tlsConfig,
				proxyURL,
			)
		}
	}

	newClient.rebuildChain()

	return newClient
}

// WithTLSClientHelloSpecProvider returns a clone of c that uses the provider
// to dynamically obtain a uTLS ClientHelloSpec for TLS Handshakes.
func (c *Client) WithTLSClientHelloSpecProvider(provider ClientHelloSpecProvider) *Client {
	newClient := c.Clone()
	newClient.fingerprint.TLSClientHelloSpecProvider = provider

	if transport := newClient.Transport(); transport != nil {
		callback := newClient.fingerprint.JA4Callback
		tlsConfig := transport.TLSClientConfig
		proxyFn := transport.Proxy
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var proxyURL *url.URL
			if proxyFn != nil {
				proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
			}

			if newClient.fingerprint.TLSClientHelloSpecProvider != nil {
				ctx = context.WithValue(
					ctx,
					clientHelloSpecProviderCtxKey{},
					newClient.fingerprint.TLSClientHelloSpecProvider,
				)
			}

			return dialTLSWithUTLS(
				ctx,
				network,
				addr,
				newClient.fingerprint.BrowserID,
				newClient.fingerprint.TLSClientHelloID,
				newClient.network.SourceRotator,
				newClient.network.DNSResolver,
				callback,
				tlsConfig,
				proxyURL,
			)
		}
	}

	newClient.rebuildChain()

	return newClient
}

// WithJA4Callback returns a clone of c that calls fn after each TLS
// handshake with the [ja4.Report]. Requires [Client.WithTLSFingerprint]
// to be enabled.
func (c *Client) WithJA4Callback(fn func(ja4.Report)) *Client {
	newClient := c.Clone()
	newClient.fingerprint.JA4Callback = fn
	newClient.rebuildChain()

	return newClient
}

// WithFragmentation returns a clone of c that splits the TLS
// ClientHello across multiple TCP segments according to cfg. See
// [FragmentConfig].
func (c *Client) WithFragmentation(cfg FragmentConfig) *Client {
	newClient := c.Clone()
	newClient.network.FragmentConfig = &cfg
	newClient.rebuildChain()

	return newClient
}

// WithHostRewrite returns a clone of c that resolves hostnames in
// requests according to rules (host -> ip), while keeping the
// original hostname for TLS SNI.
func (c *Client) WithHostRewrite(rules map[string]string) *Client {
	newClient := c.Clone()
	newClient.network.HostRewrite = &HostRewriteConfig{Rules: rules}
	newClient.rebuildChain()

	return newClient
}

// WithProxyIsolatedCookieJar returns a clone of c that stores
// cookies per proxy URL in jar, preventing cross-proxy session
// leakage. See [ProxyIsolatedCookieJar].
func (c *Client) WithProxyIsolatedCookieJar(jar *ProxyIsolatedCookieJar) *Client {
	newClient := c.Clone()
	newClient.defaults.HeadersCookieJar = jar

	if httpClient, ok := newClient.engine.(*http.Client); ok {
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
		newClient.engine = &clonedHTTP
		newClient.rebuildChain()
	}

	return newClient
}

// WithDNSCache returns a clone of c that caches DNS results for ttl.
// The cache wraps the current DNS resolver.
func (c *Client) WithDNSCache(ttl time.Duration) *Client {
	newClient := c.Clone()
	newClient.network.DNSResolver = NewInMemoryDNSCache(ttl, c.network.DNSResolver)
	newClient.rebuildChain()

	return newClient
}

// WithHTTP2Settings returns a clone of c with custom HTTP/2
// connection parameters. These values are stored on the client
// but only take effect when [Client.WithH2FramedTransport] is also
// configured.
func (c *Client) WithHTTP2Settings(settings HTTP2Settings) *Client {
	newClient := c.Clone()
	newClient.fingerprint.H2Settings = &settings
	newClient.rebuildChain()

	return newClient
}

// WithH2FramedTransport returns a clone of c that injects browser-
// specific SETTINGS and PRIORITY frames into the HTTP/2 connection
// preface. This makes the HTTP/2 fingerprint match the TLS profile
// set by [Client.WithTLSFingerprint].
func (c *Client) WithH2FramedTransport(settings HTTP2Settings) *Client {
	newClient := c.Clone()
	newClient.fingerprint.H2Settings = &settings

	if transport := newClient.Transport(); transport != nil {
		if newClient.fingerprint.H2Configurer != nil {
			t2, err := http2.ConfigureTransports(transport)
			if err == nil && t2 != nil {
				t2.TLSClientConfig = transport.TLSClientConfig
				_ = newClient.fingerprint.H2Configurer.ConfigureHTTP2(t2)
			}
		}

		framed := NewH2FramedTransport(transport, settings)
		if httpClient, ok := newClient.engine.(*http.Client); ok {
			httpClient.Transport = framed

			newClient.rebuildChain()
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
	newClient.fingerprint.P0fSignature = sig
	newClient.rebuildChain()

	return newClient
}

// WithSocketController returns a clone of c that applies the socket controller's
// Control hook to every newly created TCP socket before the connection is established.
func (c *Client) WithSocketController(controller SocketController) *Client {
	newClient := c.Clone()
	newClient.network.SocketController = controller
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithHTTP2Configurer returns a clone of c configured with the provided HTTP2Configurer callback.
// This allows customizing the underlying HTTP/2 transport (e.g. disabling HPACK dynamic tables, etc.).
func (c *Client) WithHTTP2Configurer(configurer HTTP2Configurer) *Client {
	newClient := c.Clone()
	newClient.fingerprint.H2Configurer = configurer
	newClient.applyDialers()

	// If h2Settings is already configured, re-apply H2 transport to ensure configurer runs
	if newClient.fingerprint.H2Settings != nil {
		newClient = newClient.WithH2FramedTransport(*newClient.fingerprint.H2Settings)
	}

	newClient.rebuildChain()

	return newClient
}

// WithProxyDNS returns a clone of c that resolves hostnames through
// the configured SOCKS5 or HTTP CONNECT proxy instead of the local
// system resolver. This prevents the local ISP from observing DNS
// queries.
func (c *Client) WithProxyDNS() *Client {
	newClient := c.Clone()
	newClient.network.ProxyDNS = true
	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithProxy returns a clone of c configured to route requests through proxyURL.
// Supported schemes: http, socks5, socks5h (for remote DNS resolution).
// When proxyURL is nil, proxy routing is disabled.
func (c *Client) WithProxy(proxyURL *url.URL) *Client {
	newClient := c.Clone()
	newClient.network.ProxyAddr = proxyURL

	if proxyURL != nil {
		newClient.network.TransportProxy = http.ProxyURL(proxyURL)
	}

	newClient.applyDialers()
	newClient.rebuildChain()

	return newClient
}

// WithProxyString returns a clone of c configured to route requests through a proxy string.
// If the proxy string lacks a scheme (e.g. "user:pass@1.2.3.4:8080"), the protocol (http or socks5)
// is automatically detected by probing the port or guessing based on the port number.
func (c *Client) WithProxyString(proxyStr string) *Client {
	u, err := ParseAutoProxy(proxyStr)
	if err != nil {
		return c.WithProxy(nil)
	}

	return c.WithProxy(u)
}

// WithHTTP3Settings returns a clone of c configured with custom HTTP/3 (QUIC) settings.
func (c *Client) WithHTTP3Settings(settings HTTP3Settings) *Client {
	newClient := c.Clone()
	newClient.fingerprint.H3Settings = &settings
	newClient.rebuildChain()

	return newClient
}

// WithRefererAutomaton returns a clone of c with automatic Referer tracking enabled.
// Each request will automatically have its "Referer" header set to the URL of the previous request.
func (c *Client) WithRefererAutomaton(enabled bool) *Client {
	newClient := c.Clone()
	newClient.defaults.RefererAutomaton = enabled
	newClient.rebuildChain()

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
	if c.defaults.Logger == nil {
		return &noopLogger{}
	}

	return c.defaults.Logger
}

// BaseResponse returns a new [BaseResponse] wrapper if a provider is configured on the client.
// Returns nil if no provider is set.
func (c *Client) BaseResponse() BaseResponse {
	if c.defaults.BaseResponse == nil {
		return nil
	}

	return c.defaults.BaseResponse()
}

// HTTP returns the fully wrapped HTTPDoer pipeline.
func (c *Client) HTTP() HTTPDoer {
	return c.http
}

// Engine returns the raw underlying HTTPDoer (typically *http.Client) without any middleware wrappers.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// WithEngine returns a clone of c with the raw underlying HTTPDoer replaced by engine.
func (c *Client) WithEngine(engine HTTPDoer) *Client {
	newClient := c.Clone()
	newClient.engine = engine
	newClient.rebuildChain()

	return newClient
}

// WithPipelineWrapper returns a clone of c with the custom pipeline wrapper function.
func (c *Client) WithPipelineWrapper(wrapper func(c *Client, engine HTTPDoer) HTTPDoer) *Client {
	newClient := c.Clone()
	newClient.defaults.PipelineWrapper = wrapper
	newClient.rebuildChain()

	return newClient
}

// Defaults returns the ClientDefaults configured on c.
func (c *Client) Defaults() ClientDefaults {
	return c.defaults
}

// Network returns the NetworkConfig configured on c.
func (c *Client) Network() NetworkConfig {
	return c.network
}

// Fingerprint returns the FingerprintConfig configured on c.
func (c *Client) Fingerprint() FingerprintConfig {
	return c.fingerprint
}

// Transport returns the underlying [http.Transport] of the client.
// Returns nil if the [HTTPDoer] is not an [http.Client] or its transport is not an [http.Transport].
func (c *Client) Transport() *http.Transport {
	if httpClient, ok := c.engine.(*http.Client); ok {
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

		curr := httpClient.Transport
		for curr != nil {
			if transport, ok := curr.(*http.Transport); ok {
				return transport
			}

			if ft, ok := curr.(*H2FramedTransport); ok {
				curr = ft.Transport
				continue
			}

			if cj, ok := curr.(*cookieJarTransport); ok {
				curr = cj.next
				continue
			}

			break
		}
	}

	return nil
}

// CloseIdleConnections closes any idle keep-alive connections maintained by the client.
// This only works if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.engine.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

func (c *Client) rebuildChain() {
	if c.defaults.PipelineWrapper != nil {
		c.http = c.defaults.PipelineWrapper(c, c.engine)
		return
	}

	doer := c.engine

	doer = ResponseSizeLimitMiddleware(c.defaults.MaxResponseSize)(doer)
	doer = DecompressionAndTranscodingMiddleware()(doer)
	doer = MultiReadBodyMiddleware(c.defaults.MultiReadThreshold, c.defaults.MultiReadDisableDisk)(doer)
	doer = FinalizerMiddleware()(doer)
	doer = ResponseValidationMiddleware()(doer)
	doer = RefererAutomatonMiddleware(c.defaults.RefererAutomaton, c.defaults.RefererState)(doer)
	doer = HedgingMiddleware(c.network.HedgingDelay, c.network.DynamicHedging)(doer)
	doer = ChallengeSolverMiddleware(c.defaults.ChallengeSolver, c.defaults.ChallengeDetector)(doer)
	doer = InspectorMiddleware(c.defaults.Inspector)(doer)
	doer = PacketPaddingMiddleware(c.fingerprint.PacketPadding)(doer)
	doer = HooksMiddleware(c.defaults.BeforeRequest, c.defaults.AfterResponse)(doer)
	doer = ContextMiddleware(c)(doer)

	c.http = doer
}

func (c *Client) applyDialers() {
	if transport := c.Transport(); transport != nil {
		if c.fingerprint.H2Configurer != nil {
			t2, err := http2.ConfigureTransports(transport)
			if err == nil && t2 != nil {
				t2.TLSClientConfig = transport.TLSClientConfig
				_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
			}
		}

		// Use determineProxy so that per-request → client-level → env priority
		// is respected consistently, including the http.ProxyFromEnvironment fallback.
		transport.Proxy = c.determineProxy

		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if c.network.SocketController != nil {
				ctx = context.WithValue(ctx, socketControllerCtxKey{}, c.network.SocketController)
			}

			if err := ApplyTCPDelay(ctx); err != nil {
				return nil, err
			}

			return happyEyeballsDial(
				ctx,
				network,
				addr,
				c.network.HappyEyeballsDelay,
				c.network.SSRFGuard,
				c.network.SourceRotator,
				c.network.DNSResolver,
			)
		}

		// Wire DialTLSContext only when uTLS is NOT configured (WithTLSFingerprint
		// sets its own DialTLSContext). This gives plain http.Transport connections
		// the same WithInsecureSkipVerify + WithTCPDelay support that uTLS enjoys.
		if transport.DialTLSContext == nil {
			baseTLSCfg := transport.TLSClientConfig
			transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Honour WithTCPDelay before opening the TCP connection.
				if err := ApplyTCPDelay(ctx); err != nil {
					return nil, err
				}

				if c.network.SocketController != nil {
					ctx = context.WithValue(ctx, socketControllerCtxKey{}, c.network.SocketController)
				}

				host, _, _ := net.SplitHostPort(addr)
				if host == "" {
					host = addr
				}

				// Dial the raw TCP connection.
				rawConn, err := happyEyeballsDial(
					ctx,
					network,
					addr,
					c.network.HappyEyeballsDelay,
					c.network.SSRFGuard,
					c.network.SourceRotator,
					c.network.DNSResolver,
				)
				if err != nil {
					return nil, err
				}

				// Build the effective TLS config, applying any per-request overrides.
				effectiveCfg := TLSConfigWithOverride(ctx, baseTLSCfg)
				if effectiveCfg == nil {
					effectiveCfg = &tls.Config{} //nolint:gosec
				}

				// Ensure SNI is set even when config came from the user without it.
				if effectiveCfg.ServerName == "" {
					cloned := effectiveCfg.Clone()
					cloned.ServerName = host
					effectiveCfg = cloned
				}

				tlsConn := tls.Client(rawConn, effectiveCfg)
				if err := tlsConn.HandshakeContext(ctx); err != nil {
					_ = rawConn.Close()
					return nil, err
				}

				return tlsConn, nil
			}
		}
	}
}

// determineProxy resolves the effective proxy for req using three-tier priority:
//  1. Per-request [WithProxyOverride] stored in the request context.
//  2. Client-level proxy set via [Client.WithProxy].
//  3. System environment variables (HTTP_PROXY / HTTPS_PROXY).
//
// It is the single source of truth for proxy resolution inside the client and
// is used by [ProxyFuncWithOverride] to configure [http.Transport.Proxy].
func (c *Client) determineProxy(req *http.Request) (*url.URL, error) {
	if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
		return url.Parse(raw)
	}

	if c.network.ProxyAddr != nil {
		return c.network.ProxyAddr, nil
	}

	return http.ProxyFromEnvironment(req)
}

func dialTLSWithUTLS(
	ctx context.Context,
	network, addr string,
	browser BrowserID,
	helloID *utls.ClientHelloID,
	sourceRotator *SourceIPRotator,
	dnsResolver DNSResolver,
	ja4Callback func(ja4.Report),
	tlsConfig *tls.Config,
	proxyURL *url.URL,
) (net.Conn, error) {
	// Read callback from context (set by Client.Request) - the closure-captured
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

	// Apply per-request TCP delay (WithTCPDelay) before dialing.
	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	// Check for a per-request proxy override (WithProxyOverride).
	if raw, ok := GetProxyOverride(ctx).Value(); ok && raw != "" {
		if parsed, parseErr := url.Parse(raw); parseErr == nil {
			proxyURL = parsed
		}
	}

	// Route through proxy if configured - prevents direct IP leak.
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
	if helloID != nil {
		spec = *helloID
	} else {
		switch browser {
		case BrowserFirefox:
			spec = utls.HelloFirefox_Auto
		case BrowserSafari:
			spec = utls.HelloSafari_Auto
		default:
			spec = utls.HelloChrome_Auto
		}
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

	// Per-request InsecureSkipVerify override (WithInsecureSkipVerify).
	if GetInsecureSkipVerify(ctx) {
		uConfig.InsecureSkipVerify = true //nolint:gosec
	}

	// Use proxy-aware session cache if available in context.
	if cache, ok := ctx.Value(sessionCacheCtxKey{}).(*ProxyAwareSessionCache); ok && cache != nil {
		uConfig.ClientSessionCache = cache
	}

	if alpn, ok := ctx.Value(alpnOverrideCtxKey{}).([]string); ok && len(alpn) > 0 {
		uConfig.NextProtos = alpn
	}

	var customSpec *utls.ClientHelloSpec
	if provider, ok := ctx.Value(clientHelloSpecProviderCtxKey{}).(ClientHelloSpecProvider); ok && provider != nil {
		var err error

		customSpec, err = provider.ClientHelloSpec()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni tls: failed to get custom client hello spec: %w", err)
		}
	}

	var uConn *utls.UConn
	if customSpec != nil {
		uConn = utls.UClient(conn, uConfig, utls.HelloCustom)
		if err := uConn.ApplyPreset(customSpec); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni tls: failed to apply custom client hello spec: %w", err)
		}
	} else {
		uConn = utls.UClient(conn, uConfig, spec)
	}

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	alpnProtos := []string{"http/1.1"}
	if alpn, ok := ctx.Value(alpnOverrideCtxKey{}).([]string); ok && len(alpn) > 0 {
		alpnProtos = alpn
	}

	uConn.Extensions = forceALPN(uConn.Extensions, alpnProtos)

	report := extractJA4FromUConn(uConn, host)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

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

	if resp.Request != nil {
		if cancel, ok := resp.Request.Context().Value(requestTimeoutCancelCtxKey{}).(context.CancelFunc); ok {
			cancel()
		}
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

func makeDialerControl(ctx context.Context) func(network, address string, rc syscall.RawConn) error {
	var spoofer *p0f.Spoofer
	if cfg, ok := ctx.Value(p0fSignatureCtxKey{}).(*p0f.Signature); ok && cfg != nil {
		spoofer = p0f.NewSpoofer(cfg)
	}

	controller, _ := ctx.Value(socketControllerCtxKey{}).(SocketController)

	if spoofer == nil && controller == nil {
		return nil
	}

	return func(network, address string, rc syscall.RawConn) error {
		if controller != nil {
			var controlErr error

			err := rc.Control(func(fd uintptr) {
				controlErr = controller.Control(fd, network, address)
			})
			if err != nil {
				return err
			}

			if controlErr != nil {
				return controlErr
			}
		}

		if spoofer != nil {
			return spoofer.ApplyToRawConn(rc)
		}

		return nil
	}
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

		dialer.Control = makeDialerControl(ctx)

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
			dialer.Control = makeDialerControl(dialCtx)

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
func dialViaProxy(ctx context.Context, _, host, port string, proxyURL *url.URL) (net.Conn, error) {
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
	// Step 1: Greeting - offer both NO AUTH and USERNAME/PASSWORD when credentials exist.
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

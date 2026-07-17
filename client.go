// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
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

type requestConfigKey struct{}

// RequestConfig aggregates all request-scoped options and transport overrides.
//
// Note: It is stored in the request's context under [requestConfigKey] and is
// reusable across middleware and transport levels.
type RequestConfig struct {
	// Decoder overrides the response [Decoder] for this request.
	// - SeeAlso: [WithDecoder]
	Decoder any

	// ErrorModel is the target struct/map where non-2xx response bodies will be decoded.
	// - Important: Must be a pointer to a struct or a map.
	// - SeeAlso: [WithErrorModel]
	ErrorModel any

	// DownloadProgress triggers during reads from the response body.
	// - Parameter total: represents Content-Length (or -1 if unknown).
	// - SeeAlso: [WithDownloadProgress]
	DownloadProgress ProgressFunc

	// Capturer holds a pointer to a response reference to capture the raw response.
	// - SeeAlso: [WithCaptureResponse]
	Capturer any

	// BodyError holds any serialization/validation error occurring during request body setup.
	// - Note: Verified by [Client.Request] before dispatching the request.
	BodyError error

	// QueryError holds any validation/serialization error occurring during URL query parameter setup.
	// - Note: Verified by [Client.Request] before dispatching the request.
	QueryError error

	// MultiReadThreshold is the size limit in bytes below which response bodies are cached in memory.
	// - Note: Set to <= 0 to disable body caching.
	// - SeeAlso: [WithMultiReadBody]
	MultiReadThreshold int64

	// MultiReadDisableDisk disables disk caching when the multi-read threshold is exceeded.
	// - SeeAlso: [WithMultiReadDisableDisk]
	MultiReadDisableDisk bool

	// OrderedHeaders defines the exact order in which HTTP/1.1 request headers must be serialized.
	// - Important: Only applies to HTTP/1.1 connections.
	// - SeeAlso: [WithOrderedHeaders]
	OrderedHeaders []string

	// ALPNOverride configures the exact Application-Layer Protocol Negotiation list for TLS.
	// - Example: []string{"h2", "http/1.1"}
	// - SeeAlso: [WithForceHTTP1], [WithForceHTTP2], [WithALPN]
	ALPNOverride []string

	// JA4ReportStore holds the temporary report reference for TLS/HTTP JA4 fingerprinting.
	// - SeeAlso: [TraceJA4]
	JA4ReportStore *ja4ReportStore

	// Debug enables verbose debug logging for this single request.
	// - SeeAlso: [WithDebug]
	Debug bool

	// Fallback defines the custom fallback logic to invoke when the request fails.
	// - SeeAlso: [WithFallback], [FallbackMiddleware]
	Fallback FallbackFunc

	// RequestTimeoutCancel cancels the request-specific deadline context upon response body close.
	// - SeeAlso: [WithTimeout]
	RequestTimeoutCancel context.CancelFunc

	// HedgingDelayOverride sets a custom delay for request hedging.
	// - Note: Set to a non-positive value to disable hedging.
	// - SeeAlso: [WithHedging], [HedgingMiddleware]
	HedgingDelayOverride *time.Duration

	// ProxyAddr is the effective proxy URL for the TCP dial.
	// - SeeAlso: [WithProxyOverride]
	ProxyAddr *url.URL

	// InsecureSkipVerify disables TLS certificate verification.
	// - Warning: Setting this to true exposes the connection to man-in-the-middle attacks.
	// - SeeAlso: [WithInsecureSkipVerify]
	InsecureSkipVerify bool

	// TCPDelay introduces a random timing delay before initiating the TCP handshake.
	// - SeeAlso: [WithTCPDelay]
	TCPDelay TCPDelayRange

	// ResponseValidator verifies the response immediately after the HTTP round-trip.
	// - SeeAlso: [WithResponseValidator]
	ResponseValidator func(resp *http.Response) error

	// CacheTTL specifies the caching time-to-live for the response.
	// - SeeAlso: [WithCacheTTL]
	CacheTTL time.Duration

	// RetryPolicy defines the per-request retry override logic.
	// - SeeAlso: [WithRetryPolicy]
	RetryPolicy *RetryOverride

	// SSRFGuard enforces DNS resolution restrictions, blocking private and loopback IPs.
	SSRFGuard bool

	// HappyEyeballsDelay sets the fallback delay between IPv4 and IPv6 connection attempts.
	HappyEyeballsDelay time.Duration

	// ProxyDNS resolves the host name through the proxy to prevent local DNS leakage.
	ProxyDNS bool

	// P0fSignature spoofs the TCP/IP network stack fingerprint to emulate specific operating systems.
	// - SeeAlso: [WithP0fSignature]
	P0fSignature *p0f.Signature

	// SessionCache is the TLS session ticket cache used to resume TLS handshakes.
	SessionCache *ProxyAwareSessionCache

	// PacketPadding configuration to obscure TLS segment boundaries.
	PacketPadding *PaddingConfig

	// SocketController intercepts the socket file descriptor for low-level socket modifications.
	SocketController SocketController

	// ClientHelloSpecProvider provides custom uTLS ClientHelloSpecs.
	ClientHelloSpecProvider ClientHelloSpecProvider

	// JA4Callback is invoked once the TLS handshake is complete with the computed JA4 fingerprint report.
	JA4Callback func(ja4.Report)

	// Metadata stores arbitrary user-defined metadata values associated with the request connection.
	// - SeeAlso: [WithConnMetadata]
	Metadata map[string]any
}

// GetRequestConfig retrieves the [RequestConfig] associated with the context.
func GetRequestConfig(ctx context.Context) *RequestConfig {
	cfg, _ := ctx.Value(requestConfigKey{}).(*RequestConfig)
	return cfg
}

func getOrInitRequestConfig(req *http.Request) *RequestConfig {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		*req = *req.WithContext(ctx)
	}

	return cfg
}

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
func NewClient(httpClient HTTPDoer, opts ...ClientOption) *Client {
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

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	generic.ApplyOptions(c, opts...)

	c.rebuildChain()

	// Default to user agent if not set
	if c.defaults.Headers.Get("User-Agent") == "" {
		c.defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}

	return c
}

// With returns a clone of c with the specified functional options applied.
func (c *Client) With(opts ...ClientOption) *Client {
	cloned := c.Clone()
	for _, opt := range opts {
		opt(cloned)
	}

	cloned.applyDialers()
	cloned.rebuildChain()

	return cloned
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

// InitRequestConfig initializes the request configuration for the given request.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		req = req.WithContext(ctx)
	}

	if !cfg.SSRFGuard {
		cfg.SSRFGuard = c.network.SSRFGuard
	}

	if cfg.HappyEyeballsDelay == 0 {
		cfg.HappyEyeballsDelay = c.network.HappyEyeballsDelay
	}

	if !cfg.ProxyDNS {
		cfg.ProxyDNS = c.network.ProxyDNS
	}

	if cfg.ProxyAddr == nil {
		cfg.ProxyAddr = c.network.ProxyAddr
	}

	if cfg.P0fSignature == nil {
		cfg.P0fSignature = c.fingerprint.P0fSignature
	}

	if cfg.SessionCache == nil {
		cfg.SessionCache = c.fingerprint.SessionCache
	}

	if cfg.PacketPadding == nil {
		cfg.PacketPadding = c.fingerprint.PacketPadding
	}

	if cfg.SocketController == nil {
		cfg.SocketController = c.network.SocketController
	}

	if cfg.ClientHelloSpecProvider == nil {
		cfg.ClientHelloSpecProvider = c.fingerprint.TLSClientHelloSpecProvider
	}

	if cfg.JA4Callback == nil {
		cfg.JA4Callback = c.fingerprint.JA4Callback
	}

	if cfg.MultiReadThreshold == 0 {
		cfg.MultiReadThreshold = c.defaults.MultiReadThreshold
	}

	if !cfg.MultiReadDisableDisk {
		cfg.MultiReadDisableDisk = c.defaults.MultiReadDisableDisk
	}

	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	return req
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

	req = c.InitRequestConfig(req)

	generic.ApplyOptions(req, c.defaults.DefaultMods...)
	generic.ApplyOptions(req, mods...)

	if cfg := GetRequestConfig(req.Context()); cfg != nil {
		if cfg.BodyError != nil {
			return nil, fmt.Errorf("aoni: body encoding failed: %w", cfg.BodyError)
		}

		if cfg.QueryError != nil {
			return nil, fmt.Errorf("aoni: query encoding failed: %w", cfg.QueryError)
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

// WithMultiReadBody returns a [RequestModifier] that overrides the
// body caching threshold for a single request. Responses smaller
// than threshold are buffered in memory so the body can be read
// multiple times. A value <= 0 disables caching for the request.
func WithMultiReadBody(threshold int64) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk returns a [RequestModifier] that overrides the
// body caching disk-fallback setting for a single request. If disable is true,
// exceeding the memory threshold returns an error ([ErrBufferLimitExceeded]) instead of creating temporary files.
func WithMultiReadDisableDisk(disable bool) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).MultiReadDisableDisk = disable
	}
}

// UserAgent returns the User-Agent header configured on c.
func (c *Client) UserAgent() string {
	return c.defaults.Headers.Get("User-Agent")
}

// Inspector returns the configured [TrafficInspector] if enabled.
func (c *Client) Inspector() TrafficInspector {
	return c.defaults.Inspector
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
			// SocketController is retrieved from RequestConfig inside makeDialerControl.
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
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.JA4Callback != nil {
			ja4Callback = cfg.JA4Callback
		}
	}

	ssrfGuard := false

	delay := 300 * time.Millisecond
	if cfg != nil {
		ssrfGuard = cfg.SSRFGuard
		delay = cfg.HappyEyeballsDelay
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
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
	if cfg != nil && cfg.SessionCache != nil {
		uConfig.ClientSessionCache = cfg.SessionCache
	}

	if cfg != nil && len(cfg.ALPNOverride) > 0 {
		uConfig.NextProtos = cfg.ALPNOverride
	}

	var customSpec *utls.ClientHelloSpec
	if cfg != nil && cfg.ClientHelloSpecProvider != nil {
		var err error

		customSpec, err = cfg.ClientHelloSpecProvider.ClientHelloSpec()
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
	if cfg != nil && len(cfg.ALPNOverride) > 0 {
		alpnProtos = cfg.ALPNOverride
	}

	uConn.Extensions = forceALPN(uConn.Extensions, alpnProtos)

	report := extractJA4FromUConn(uConn, host)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Write JA4 report to the store in the request context (set by TraceJA4).
	// The request context flows through to DialTLSContext.
	if cfg != nil && cfg.JA4ReportStore != nil {
		cfg.JA4ReportStore.report = &report
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
		if cfg := GetRequestConfig(resp.Request.Context()); cfg != nil && cfg.RequestTimeoutCancel != nil {
			cfg.RequestTimeoutCancel()
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

func wrapConn(ctx context.Context, conn net.Conn) net.Conn {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.PacketPadding != nil && cfg.PacketPadding.MaxSegmentSize > 0 {
			conn = wrapWithMSSLimit(conn, cfg.PacketPadding.MaxSegmentSize)
		}

		if len(cfg.OrderedHeaders) > 0 {
			conn = &headerOrderingConn{Conn: conn, orderedKeys: cfg.OrderedHeaders}
		}
	}

	if fCfg, ok := ctx.Value(fragmentCtxKey{}).(FragmentConfig); ok && fCfg.ChunkSize > 0 {
		conn = wrapWithFragmentation(conn, fCfg)
	}

	return conn
}

func makeDialerControl(ctx context.Context) func(network, address string, rc syscall.RawConn) error {
	var (
		spoofer    *p0f.Spoofer
		controller SocketController
	)

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.P0fSignature != nil {
			spoofer = p0f.NewSpoofer(cfg.P0fSignature)
		}

		controller = cfg.SocketController
	}

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
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.ProxyDNS {
		if cfg.ProxyAddr != nil && net.ParseIP(host) == nil {
			return dialViaProxy(ctx, network, host, port, cfg.ProxyAddr)
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

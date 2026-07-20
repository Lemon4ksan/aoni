// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/h3"
	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
)

// Config aggregates all client settings for easy serialization and bootstrapping.
type Config struct {
	Network     NetworkConfig
	Fingerprint FingerprintConfig
	Defaults    ClientDefaults
}

// QueryEncoder defines the function signature for marshalling structures into url.Values.
type QueryEncoder func(any) (url.Values, error)

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

// BrowserID selects a uTLS ClientHello profile for JA3 fingerprint
// emulation. Pass to [WithClientTLSFingerprint].
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

// DefaultUserAgent is the default User-Agent string used for HTTP requests.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// DefaultBrowserProfiles provides a list of realistic, modern Chrome browser profiles.
var DefaultBrowserProfiles = []BrowserProfile{
	{
		UserAgent: DefaultUserAgent,
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": `"Windows"`,
		},
	},
	{
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": `"macOS"`,
		},
	},
	{
		UserAgent: "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?1",
			"Sec-CH-UA-Platform": `"Android"`,
		},
	},
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

	// MultipartBoundary is a custom boundary string for multipart requests.
	MultipartBoundary string

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
	JA4ReportStore *JA4ReportStore

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

	// TraceInfo holds timing tracking metrics.
	TraceInfo *TraceInfo

	// HostRewrite rules.
	HostRewrite *HostRewriteConfig

	// Pipeline config overrides.
	Pipeline *PipelineConfig

	// Fragment configures packet fragmentation.
	Fragment *FragmentConfig

	// TimeoutOverride overrides the request timeout.
	TimeoutOverride time.Duration

	// Redact configures the sensitive header redaction rules.
	Redact *RedactConfig

	// CertificatePins maps domains to their pinned SHA-256 public key hashes.
	CertificatePins map[string][]string

	// Modifiers holds RequestModifiers passed via context.
	Modifiers []RequestModifier

	// QueryEncoder allows overriding the query encoder for a specific request.
	QueryEncoder QueryEncoder
}

// ApplyDefaults merges client-level defaults into the request config
// if they are not already set.
func (cfg *RequestConfig) ApplyDefaults(c *Client) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	if cfg.CertificatePins == nil {
		cfg.CertificatePins = make(map[string][]string)
	}

	if !cfg.SSRFGuard {
		cfg.SSRFGuard = c.network.SSRFGuard
	}

	if !cfg.ProxyDNS {
		cfg.ProxyDNS = c.network.ProxyDNS
	}

	if !cfg.MultiReadDisableDisk {
		cfg.MultiReadDisableDisk = c.defaults.MultiReadDisableDisk
	}

	cfg.HappyEyeballsDelay = generic.Coalesce(cfg.HappyEyeballsDelay, c.network.HappyEyeballsDelay)
	cfg.MultiReadThreshold = generic.Coalesce(c.defaults.MultiReadThreshold, cfg.MultiReadThreshold)

	cfg.ProxyAddr = generic.CoalesceNil(cfg.ProxyAddr, c.network.ProxyAddr)
	cfg.P0fSignature = generic.CoalesceNil(cfg.P0fSignature, c.fingerprint.P0fSignature)
	cfg.SessionCache = generic.CoalesceNil(cfg.SessionCache, c.fingerprint.SessionCache)
	cfg.PacketPadding = generic.CoalesceNil(cfg.PacketPadding, c.fingerprint.PacketPadding)
	cfg.SocketController = generic.CoalesceNil(cfg.SocketController, c.network.SocketController)
	cfg.ClientHelloSpecProvider = generic.CoalesceNil(
		cfg.ClientHelloSpecProvider,
		c.fingerprint.TLSClientHelloSpecProvider,
	)
	cfg.JA4Callback = generic.CoalesceNil(cfg.JA4Callback, c.fingerprint.JA4Callback)
	cfg.QueryEncoder = generic.CoalesceNil(cfg.QueryEncoder, c.defaults.QueryEncoder)

	for domain, hashes := range c.fingerprint.CertificatePins {
		for _, h := range hashes {
			if !slices.Contains(cfg.CertificatePins[domain], h) {
				cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], h)
			}
		}
	}
}

type requestConfigKey struct{}

// GetRequestConfig retrieves the [RequestConfig] associated with the context.
func GetRequestConfig(ctx context.Context) *RequestConfig {
	cfg, _ := ctx.Value(requestConfigKey{}).(*RequestConfig)
	return cfg
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

// Clone returns the deep copy of the NetworkConfig.
func (n NetworkConfig) Clone() NetworkConfig {
	cloned := n

	if n.DynamicHedging != nil {
		dhCopy := *n.DynamicHedging
		cloned.DynamicHedging = &dhCopy
	}

	if n.FragmentConfig != nil {
		fragCopy := *n.FragmentConfig
		cloned.FragmentConfig = &fragCopy
	}

	if n.HostRewrite != nil && n.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(n.HostRewrite.Rules))
		maps.Copy(rulesCopy, n.HostRewrite.Rules)
		cloned.HostRewrite = &HostRewriteConfig{Rules: rulesCopy}
	}

	return cloned
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
	H2Settings *h2.Settings

	// H3Settings overrides the default HTTP/3 (QUIC) configuration settings.
	H3Settings *h3.Settings

	// SessionCache is a proxy-aware TLS session ticket cache that prevents session correlation across proxies.
	SessionCache *ProxyAwareSessionCache

	// PacketPadding adjusts MSS and injects random padding headers to confuse DPI length analysis.
	PacketPadding *PaddingConfig

	// CertificatePins maps domains to their pinned SHA-256 public key hashes globally.
	CertificatePins map[string][]string
}

// Clone returns the deep copy of the FingerprintConfig.
func (f FingerprintConfig) Clone() FingerprintConfig {
	cloned := f

	if f.TLSClientHelloID != nil {
		idCopy := *f.TLSClientHelloID
		cloned.TLSClientHelloID = &idCopy
	}

	if f.HeaderOrder != nil {
		orderCopy := make([]string, len(f.HeaderOrder))
		copy(orderCopy, f.HeaderOrder)
		cloned.HeaderOrder = orderCopy
	}

	if f.P0fSignature != nil {
		sigCopy := *f.P0fSignature
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

		cloned.P0fSignature = &sigCopy
	}

	if f.H2Settings != nil {
		h2Copy := *f.H2Settings
		cloned.H2Settings = &h2Copy
	}

	if f.H3Settings != nil {
		h3Copy := *f.H3Settings
		cloned.H3Settings = &h3Copy
	}

	if f.PacketPadding != nil {
		padCopy := *f.PacketPadding
		cloned.PacketPadding = &padCopy
	}

	return cloned
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

	// Pipeline configures the client-level default pipeline settings.
	Pipeline PipelineConfig

	// QueryEncoder is the default encoder for marshalling structures into url.Values.
	QueryEncoder QueryEncoder

	// ResponseValidator is the client-level default response validator called on every request.
	ResponseValidator func(*http.Response) error

	// UARotationProfiles defines the list of browser profiles for User-Agent rotation.
	UARotationProfiles []BrowserProfile
}

// Clone returns the deep copy of the ClientDefaults.
func (d ClientDefaults) Clone() ClientDefaults {
	cloned := d

	cloned.Headers = d.Headers.Clone()

	beforeCopy := make([]func(req *http.Request), len(d.BeforeRequest))
	copy(beforeCopy, d.BeforeRequest)
	cloned.BeforeRequest = beforeCopy

	afterCopy := make([]func(resp *http.Response, err error), len(d.AfterResponse))
	copy(afterCopy, d.AfterResponse)
	cloned.AfterResponse = afterCopy

	if d.DefaultMods != nil {
		modsCopy := make([]RequestModifier, len(d.DefaultMods))
		copy(modsCopy, d.DefaultMods)
		cloned.DefaultMods = modsCopy
	}

	cloned.Pipeline = d.Pipeline
	if d.Pipeline.DPIJitter != nil {
		dj := *d.Pipeline.DPIJitter
		cloned.Pipeline.DPIJitter = &dj
	}

	if d.Pipeline.ProxyFailover != nil {
		pf := *d.Pipeline.ProxyFailover
		proxiesCopy := make([]string, len(pf.Proxies))
		copy(proxiesCopy, pf.Proxies)
		pf.Proxies = proxiesCopy
		cloned.Pipeline.ProxyFailover = &pf
	}

	if d.Pipeline.Hedging != nil {
		h := *d.Pipeline.Hedging
		if h.DynamicHedging != nil {
			dhCopy := *h.DynamicHedging
			h.DynamicHedging = &dhCopy
		}

		cloned.Pipeline.Hedging = &h
	}

	if d.Pipeline.Cache != nil {
		cc := *d.Pipeline.Cache
		cloned.Pipeline.Cache = &cc
	}

	if d.Pipeline.HAR != nil {
		har := *d.Pipeline.HAR
		cloned.Pipeline.HAR = &har
	}

	if d.Pipeline.Redact != nil {
		r := *d.Pipeline.Redact
		if r.Headers != nil {
			headersCopy := make(map[string]struct{}, len(r.Headers))
			maps.Copy(headersCopy, r.Headers)

			r.Headers = headersCopy
		}

		cloned.Pipeline.Redact = &r
	}

	if d.UARotationProfiles != nil {
		profilesCopy := make([]BrowserProfile, len(d.UARotationProfiles))
		for i, prof := range d.UARotationProfiles {
			hintsCopy := make(map[string]string, len(prof.ClientHints))
			maps.Copy(hintsCopy, prof.ClientHints)

			profilesCopy[i] = BrowserProfile{
				UserAgent:   prof.UserAgent,
				ClientHints: hintsCopy,
			}
		}

		cloned.UARotationProfiles = profilesCopy
	}

	return cloned
}

// ConnectionPoolConfig tunes the [http.Transport] connection pool.
// Apply it with [WithClientConnectionPool].
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

// PipelineConfig configures request-response execution phases.
type PipelineConfig struct {
	// RotateUA enables automatic rotation of the User-Agent header and its matching
	// Sec-CH-UA-* Client Hints for every request.
	RotateUA bool

	// DPIJitter configures a randomized delay (jitter) introduced between writing
	// request headers and body to confuse timing analysis of Deep Packet Inspection (DPI) systems.
	DPIJitter *DPIJitterConfig

	// ProxyFailover configures automatic fallback to alternative proxies in the pool
	// if the current proxy fails with connection errors or returns 502/503 status codes.
	ProxyFailover *ProxyFailoverConfig

	// Hedging configures parallel request dispatching (hedging) to mitigate tail latency
	// by firing a backup request if the primary request stalls beyond the configured delay.
	Hedging *HedgingConfig

	// Cache configures RFC-7234 compliant local caching of GET responses.
	Cache *CacheConfig

	// HAR configures capturing full request-response exchanges (including headers, bodies,
	// and timing metrics) and recording them into an HTTP Archive (HAR) log.
	HAR *HARConfig

	// Redact configures automatic redaction of sensitive request/response headers
	// (like Authorization and Cookie) by replacing their values with [REDACTED].
	Redact *RedactConfig

	// Inspect enables raw request/response mirroring to the traffic inspector for debugging and replay.
	Inspect bool

	// SizeLimit specifies the maximum allowed size of the response body in bytes.
	// Responses exceeding this limit are aborted early to protect against response bomb attacks.
	SizeLimit int64

	// Decompress enables transparent decompression of brotli, zstd, gzip, or deflate response bodies.
	Decompress bool

	// Validate configures response validation using registered validation rules (like checking status codes).
	Validate bool

	// Challenge enables automatic WAF/JS/DDoS challenge page detection (e.g. Cloudflare) before returning the response.
	Challenge bool
}

// WithPipeline returns a RequestModifier that overrides the pipeline configuration for a single request.
func WithPipeline(pipe PipelineConfig) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).Pipeline = &pipe
	}
}

// GetPipeline retrieves the request-specific PipelineConfig from context.
func GetPipeline(ctx context.Context) (PipelineConfig, bool) {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.Pipeline != nil {
		return *cfg.Pipeline, true
	}

	return PipelineConfig{}, false
}

// DPIJitterConfig configures the randomized delay before sending headers or body.
type DPIJitterConfig struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

// ProxyFailoverConfig configures automatic proxy switching on connection or gateway errors.
type ProxyFailoverConfig struct {
	Proxies    []string
	RetryLimit int
}

// HedgingConfig configures the request hedging delay and dynamic tracker.
type HedgingConfig struct {
	DefaultDelay   time.Duration
	DynamicHedging *DynamicHedgingConfig
}

// CacheStore defines an interface for response caching.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
}

// CacheConfig configures response caching store and TTL.
type CacheConfig struct {
	Store      CacheStore
	DefaultTTL time.Duration
}

// HARConfig configures completed session logging to a HAR Generator.
type HARConfig struct {
	Generator *HARGenerator
}

// RedactConfigCtxKey is the context key used to store the RedactConfig in the request context.
type RedactConfigCtxKey struct{}

// RedactConfig holds the configuration for redacting sensitive headers.
type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

// BrowserProfile represents a user agent aligned with its modern client hints.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// JA4ReportStore is a shared pointer that allows dialTLSWithUTLS to write the JA4 report
// and Client.Request to copy it to the target TraceInfo after the request completes.
type JA4ReportStore struct {
	Report *ja4.Report
	Target *TraceInfo
}

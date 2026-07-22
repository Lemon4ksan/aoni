// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/telemetry"
)

const (
	// AlpnH3 is the official Application-Layer Protocol Negotiation (ALPN) token
	// used to negotiate HTTP/3 over QUIC during the connection handshake.
	AlpnH3 = "h3"

	// AlpnH2 is the ALPN token used to negotiate HTTP/2 over TLS.
	AlpnH2 = "h2"

	// AlpnHTTP is the ALPN token used to negotiate classic HTTP/1.1 over TLS.
	AlpnHTTP = "http/1.1"
)

// BrowserID represents a pre-defined uTLS ClientHello profile used to emulate
// specific browser TLS handshake fingerprints.
type BrowserID int

const (
	// BrowserNone disables TLS fingerprint emulation, falling back to standard Go TLS.
	BrowserNone BrowserID = iota

	// BrowserChrome emulates Google Chrome TLS handshake fingerprints.
	BrowserChrome

	// BrowserFirefox emulates Mozilla Firefox TLS handshake fingerprints.
	BrowserFirefox

	// BrowserSafari emulates Apple Safari TLS handshake fingerprints.
	BrowserSafari
)

// DefaultUserAgent is the standard User-Agent header value applied to outbound requests
// if no custom User-Agent is declared.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// DefaultBrowserProfiles provides a list of realistic, modern Chrome browser profiles with
// matching User-Agent strings and Client Hints to pass anti-bot heuristics.
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

// DefaultSensitiveHeaders lists headers that must be scrubbed from outgoing requests
// during cross-origin redirects to prevent credential and session token leakage.
var DefaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// Config aggregates all isolated configuration layers required to bootstrap,
// serialize, or deep-clone a [Client] instance safely.
type Config struct {
	// Network configures the network transport layer, proxies, and DNS resolution.
	Network NetworkConfig

	// Fingerprint configures TLS/JA4 evasion, HTTP/2 setting frames, and header ordering.
	Fingerprint FingerprintConfig

	// Defaults configures standard request defaults, hooks, and body buffer limits.
	Defaults ClientDefaults

	// Engine configures settings applied directly to the underlying HTTPDoer engine.
	Engine EngineConfig
}

// EngineConfig configures parameters applied directly to the underlying [HTTPDoer]
// engine (typically [*http.Client]) rather than the modular network/fingerprint layers.
type EngineConfig struct {
	// Timeout sets the maximum duration allowed for the end-to-end request transaction.
	Timeout time.Duration

	// RedirectLimit controls the maximum number of redirects followed.
	//   -2: Unset (leaves engine default behavior).
	//   -1: Unlimited redirects.
	//    0: Disables and blocks all redirects.
	//  > 0: Explicit cap on followed redirects.
	RedirectLimit int

	// CookieJar overrides the engine's default cookie storage.
	CookieJar http.CookieJar

	// InsecureSkipVerify disables TLS certificate verification globally on the transport.
	// Warning: Enabling this exposes connections to man-in-the-middle attacks.
	InsecureSkipVerify bool

	// ConnectionPool configures idle connection limits and keepalive timeouts.
	ConnectionPool *ConnectionPoolConfig

	// CustomEngine replaces the default *http.Client engine entirely.
	CustomEngine HTTPDoer

	// HTTP2Config configures low-level HTTP/2 connection and ping parameters.
	HTTP2Config *HTTP2Config
}

const redirectLimitUnset = -2

// ConnectionPoolConfig configures keep-alive connection pool boundaries
// on the underlying [http.Transport] instance.
type ConnectionPoolConfig struct {
	// MaxIdleConns is the maximum number of idle (keep-alive) connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost is the maximum number of idle (keep-alive) connections kept per host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost is the maximum total number of concurrent connections allowed per host.
	MaxConnsPerHost int

	// IdleConnTimeout is the maximum duration an idle connection is kept open.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the maximum duration to wait for reading response headers.
	ResponseHeaderTimeout time.Duration
}

// HTTP2Config configures low-level HTTP/2 connection parameters.
type HTTP2Config struct {
	// ReadIdleTimeout is the connection idle ping timeout.
	// Sends a PING frame if the connection is idle for this duration.
	ReadIdleTimeout time.Duration

	// PingTimeout is the duration to wait for a PONG response before closing the connection.
	PingTimeout time.Duration

	// AllowHTTP allows HTTP/2 over plain, unencrypted TCP connections (h2c / Prior Knowledge).
	AllowHTTP bool
}

// QUICMigrationConfig controls the parameters for QUIC Connection Migration over HTTP/3.
type QUICMigrationConfig struct {
	// EnableMigration enables QUIC Connection Migration.
	EnableMigration bool

	// KeepAlivePeriod sends periodic keepalive packets to maintain the connection.
	KeepAlivePeriod time.Duration

	// MaxIdleTimeout is the maximum duration without network activity before connection close.
	MaxIdleTimeout time.Duration

	// DisablePathMTUDiscovery disables Path MTU Discovery during migration.
	DisablePathMTUDiscovery bool

	// InitialPacketSize sets the initial QUIC packet size.
	InitialPacketSize uint16
}

// DefaultQUICMigrationConfig returns a [QUICMigrationConfig] populated with stable,
// production-ready default values.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

// staticSpecProvider wraps a static *utls.ClientHelloSpec as a ClientHelloSpecProvider.
type staticSpecProvider struct {
	Spec *utls.ClientHelloSpec
}

// ClientHelloSpec returns the underlying static spec.
func (s staticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.Spec, nil
}

// NetworkConfig configures the network transport layer, proxies, DNS resolution,
// SSRF safeguards, IP rotation, and socket controllers.
type NetworkConfig struct {
	// ProxyDNS routes DNS lookup requests through the SOCKS5 or HTTP CONNECT proxy
	// to prevent local DNS queries from leaking to the local ISP.
	ProxyDNS bool

	// ProxyAddr is the URL of the proxy server to route all traffic through.
	// Supports http, socks5, and socks5h schemes.
	ProxyAddr *url.URL

	// TransportProxy is the proxy resolution function used by the transport.
	TransportProxy func(*http.Request) (*url.URL, error)

	// DNSResolver is the custom resolver used to resolve hostnames.
	DNSResolver DNSResolver

	// SSRFGuard blocks requests that resolve to private or loopback IP addresses.
	SSRFGuard bool

	// HappyEyeballsDelay staggers parallel IPv4/IPv6 dial attempts to minimize latency.
	HappyEyeballsDelay time.Duration

	// SourceRotator manages a pool of local IP addresses to bind outgoing connections to in a round-robin fashion.
	SourceRotator *ip.SourceIPRotator

	// HedgingDelay defines the delay before a second, parallel request is dispatched for a slow request.
	HedgingDelay time.Duration

	// DynamicHedging configures dynamic request hedging based on the percentile RTT of recent successful requests.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// SocketController hook is executed on every raw TCP connection right after it is dialed, before any TLS handshake.
	SocketController SocketController

	// FragmentConfig specifies the configuration for splitting TLS ClientHello packets across TCP segments.
	FragmentConfig *FragmentConfig

	// HostRewrite contains custom DNS rules mapping specific hostnames to target IP addresses.
	HostRewrite *HostRewriteConfig
}

// FragmentConfig specifies the chunk size and inter-chunk delay for connection fragmentation.
type FragmentConfig struct {
	ChunkSize int

	// LimitBytes specifies the maximum number of bytes to subject to fragmentation.
	// Once LimitBytes is exceeded, subsequent writes pass through seamlessly.
	// Set to -1 to fragment the entire stream.
	LimitBytes int64

	MinChunkSize int
	MaxChunkSize int

	MaxDelay time.Duration
	MinDelay time.Duration
}

// NewFragmentedConn wraps a net.Conn with fragmentation and delay settings.
func NewFragmentedConn(conn net.Conn, cfg *FragmentConfig) net.Conn {
	var limit int64
	switch cfg.LimitBytes {
	case -1:
		// -1 means infinite fragmentation (no limit)
		limit = 0
	case 0:
		// Default safe limit to cover heavy/post-quantum TLS ClientHello (4 KB)
		limit = 4096
	default:
		// User-defined custom limit
		limit = cfg.LimitBytes
	}

	return &fragment.FragmentedConn{
		Conn:         conn,
		ChunkSize:    cfg.ChunkSize,
		MaxDelay:     cfg.MaxDelay,
		MinDelay:     cfg.MinDelay,
		MaxChunkSize: cfg.MaxChunkSize,
		MinChunkSize: cfg.MinChunkSize,
		LimitBytes:   limit,
	}
}

// Clone returns a deep copy of [NetworkConfig].
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

// HostRewriteConfig stores custom hostname-to-IP remapping rules for DNS override.
type HostRewriteConfig struct {
	Rules map[string]string
}

// DNSResolver defines the hostname-to-IP lookup resolution interface.
//
// This allows custom DNS resolution (such as DNS-over-HTTPS or DNS-over-TLS)
// to bypass the system's default networking resolver cleanly.
type DNSResolver interface {
	// LookupIPAddr resolves the host domain into a slice of IP addresses under the given context.
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// FingerprintConfig groups settings related to browser TLS/JA4 evasion,
// HTTP/2 setting frames, header order serialization, and TCP packet padding.
type FingerprintConfig struct {
	// BrowserID selects a pre-configured uTLS ClientHello profile for TLS fingerprint emulation.
	BrowserID BrowserID

	// TLSClientHelloID is a specific, low-level uTLS ClientHello ID to use instead of a generic BrowserID.
	TLSClientHelloID *utls.ClientHelloID

	// TLSClientHelloSpecProvider dynamically provides custom ClientHelloSpecs at handshake time.
	TLSClientHelloSpecProvider ClientHelloSpecProvider

	// TLSQUICClientHelloSpec is the QUIC-specific ClientHelloSpec to use for TLS fingerprint emulation.
	TLSQUICClientHelloSpec *utls.ClientHelloSpec

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
	SessionCache SessionCache

	// PacketPadding adjusts MSS and injects random padding headers to confuse DPI length analysis.
	PacketPadding *fingerprint.PaddingConfig

	// CertificatePins maps domains to their pinned SHA-256 public key hashes globally.
	CertificatePins map[string][]string
}

// Clone returns a deep copy of [FingerprintConfig].
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

// SessionCache extends the standard uTLS session caching contract to support proxy-isolated connection tickets.
//
// Proxy-isolated session ticket managers prevent tracking clients across multiple proxies/exit IPs
// by invalidating active handshakes whenever the target proxy changes.
type SessionCache interface {
	utls.ClientSessionCache

	// SetProxyKey invalidates all cached session tickets and initializes a fresh session cache pool
	// for the specified proxy key (such as the proxy address or source IP) to prevent cross-proxy tracking.
	SetProxyKey(key string)
}

// JA4ReportStore serves as a thread-safe shared carrier used by low-level TLS dialers
// to record computed JA4 reports during handshakes and propagate them back to the active request context.
type JA4ReportStore struct {
	// Report points to the captured raw JA4 signature computed on the wire.
	Report *ja4.Report

	// Target points to the request's telemetry trace container where the final report is compiled.
	Target *telemetry.TraceInfo
}

// CacheStore defines the contract for response caching backends.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
}

// CacheConfig configures HTTP response caching behavior and default time-to-live.
type CacheConfig struct {
	Store      CacheStore
	DefaultTTL time.Duration
}

// CachedResponse holds a serialized HTTP response stored in cache backends.
type CachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	BodyBase64 string              `json:"body_base64"`
}

// PipelineConfig configures request-response execution phases,
// including User-Agent rotation, DPI jittering, HAR logging, and size limits.
type PipelineConfig struct {
	// RotateUA enables automatic rotation of the User-Agent header and its matching Client Hints.
	RotateUA bool

	// DPIJitter configures a randomized delay introduced between writing request headers and body.
	DPIJitter *DPIJitterConfig

	// ProxyFailover configures automatic fallback to alternative proxies in the pool if the current proxy fails.
	ProxyFailover *ProxyFailoverConfig

	// Hedging configures parallel request dispatching (hedging) to mitigate tail latency.
	Hedging *HedgingConfig

	// Cache configures RFC-7234 compliant local caching of GET responses.
	Cache *CacheConfig

	// HAR configures capturing full request-response exchanges into an HTTP Archive (HAR) log.
	HAR *HARConfig

	// Redact configures automatic redaction of sensitive request/response headers.
	Redact *RedactConfig

	// Inspect enables raw request/response mirroring to the traffic inspector.
	Inspect bool

	// SizeLimit specifies the maximum allowed size of the response body in bytes.
	SizeLimit int64

	// Decompress enables transparent decompression of brotli, zstd, gzip, or deflate response bodies.
	Decompress bool

	// Validate configures response validation using registered validation rules.
	Validate bool

	// Challenge enables automatic WAF/JS/DDoS challenge page detection (e.g. Cloudflare).
	Challenge bool
}

// GetPipeline retrieves the request-specific [PipelineConfig] from context.
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
	DynamicHedging *telemetry.DynamicHedgingConfig
}

// HARConfig configures capturing completed session logging to a HAR Tracker.
type HARConfig struct {
	Tracker HARTracker
}

// RedactConfig holds the configuration for redacting sensitive headers.
type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

// RedactConfigCtxKey is the context key used to store the [RedactConfig] in the request context.
type RedactConfigCtxKey struct{}

// ChallengeSolver delegates WAF challenge resolution (e.g. JavaScript challenges, CAPTCHAs)
// to an automated external driver such as Playwright, Selenium, or FlareSolverr.
type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector decides whether an incoming HTTP response constitutes a WAF challenge.
type ChallengeDetector func(resp *http.Response) (bool, error)

// ClientDefaults configures standard request defaults, hooks, WAF solvers,
// body buffering configs, and debuggers.
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

// Clone returns a deep copy of [ClientDefaults].
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

// BrowserProfile represents a user agent aligned with its modern client hints.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// RefererState holds the thread-safe state for the Referer tracking automaton.
type RefererState struct {
	mu      sync.Mutex
	lastURL string
}

// ProgressFunc represents a callback signature triggered periodically during data streams
// (such as uploading request payloads or reading response bodies) to monitor transfer progress.
type ProgressFunc = io.ProgressFunc

// RequestConfig aggregates all request-scoped options and transport overrides.
type RequestConfig struct {
	// Decoder overrides the response [Decoder] for this request.
	Decoder any

	// ErrorModel is the target struct/map where non-2xx response bodies will be decoded.
	ErrorModel any

	// UploadProgress triggers during reads from the response body.
	UploadProgress ProgressFunc

	// DownloadProgress triggers during reads from the response body.
	DownloadProgress ProgressFunc

	// Capturer holds a pointer to a response reference to capture the raw response.
	Capturer any

	// BodyError holds any serialization/validation error occurring during request body setup.
	BodyError error

	// QueryError holds any validation/serialization error occurring during URL query parameter setup.
	QueryError error

	// MultipartBoundary is a custom boundary string for multipart requests.
	MultipartBoundary string

	// MultiReadThreshold is the size limit in bytes below which response bodies are cached in memory.
	MultiReadThreshold int64

	// MultiReadDisableDisk disables disk caching when the multi-read threshold is exceeded.
	MultiReadDisableDisk bool

	// OrderedHeaders defines the exact order in which HTTP/1.1 request headers must be serialized.
	OrderedHeaders []string

	// ALPNOverride configures the exact Application-Layer Protocol Negotiation list for TLS.
	ALPNOverride []string

	// JA4ReportStore holds the temporary report reference for TLS/HTTP JA4 fingerprinting.
	JA4ReportStore *JA4ReportStore

	// Debug enables verbose debug logging for this single request.
	Debug bool

	// Fallback defines the custom fallback logic to invoke when the request fails.
	Fallback FallbackFunc

	// RequestTimeoutCancel cancels the request-specific deadline context upon response body close.
	RequestTimeoutCancel context.CancelFunc

	// HedgingDelayOverride sets a custom delay for request hedging.
	HedgingDelayOverride *time.Duration

	// ProxyAddr is the effective proxy URL for the TCP dial.
	ProxyAddr *url.URL

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// TCPDelay introduces a random timing delay before initiating the TCP handshake.
	TCPDelay TCPDelayRange

	// ResponseValidator verifies the response immediately after the HTTP round-trip.
	ResponseValidator func(resp *http.Response) error

	// CacheTTL specifies the caching time-to-live for the response.
	CacheTTL time.Duration

	// RetryPolicy defines the per-request retry override logic.
	RetryPolicy *RetryOverride

	// SSRFGuard enforces DNS resolution restrictions, blocking private and loopback IPs.
	SSRFGuard bool

	// HappyEyeballsDelay sets the fallback delay between IPv4 and IPv6 connection attempts.
	HappyEyeballsDelay time.Duration

	// ProxyDNS resolves the host name through the proxy to prevent local DNS leakage.
	ProxyDNS bool

	// P0fSignature spoofs the TCP/IP network stack fingerprint to emulate specific operating systems.
	P0fSignature *p0f.Signature

	// SessionCache is the TLS session ticket cache used to resume TLS handshakes.
	SessionCache SessionCache

	// PacketPadding configuration to obscure TLS segment boundaries.
	PacketPadding *fingerprint.PaddingConfig

	// SocketController intercepts the socket file descriptor for low-level socket modifications.
	SocketController SocketController

	// ClientHelloSpecProvider provides custom uTLS ClientHelloSpecs.
	ClientHelloSpecProvider ClientHelloSpecProvider

	// JA4Callback is invoked once the TLS handshake is complete with the computed JA4 fingerprint report.
	JA4Callback func(ja4.Report)

	// Metadata stores arbitrary user-defined metadata values associated with the request connection.
	Metadata map[string]any

	// TraceInfo holds timing tracking metrics.
	TraceInfo *telemetry.TraceInfo

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

	c.mergeCertificatePins(cfg)
}

func (c *Client) mergeCertificatePins(cfg *RequestConfig) {
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

// QueryEncoder defines the function signature for marshalling structures into url.Values.
type QueryEncoder func(any) (url.Values, error)

// ClientHelloSpecProvider defines an interface that returns a uTLS ClientHelloSpec.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// TrafficInspector defines the interface for capturing and logging request trace history.
type TrafficInspector interface {
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *telemetry.TraceInfo)
}

// SocketController defines a hook callback interface to directly intercept and configure
// TCP sockets at the dial phase before the SYN packet is sent.
type SocketController interface {
	Control(fd uintptr, network, address string) error
}

// HTTP2Configurer defines an interface to customize the golang.org/x/net/http2.Transport instance.
type HTTP2Configurer interface {
	ConfigureHTTP2(t *http2.Transport) error
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

// BaseResponseProvider optionally provides a [BaseResponse] for structured decoding.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// BaseResponse is implemented by user-defined response wrappers.
type BaseResponse interface {
	// IsSuccess reports whether the response indicates a successful operation.
	IsSuccess() bool
	// Error returns an error representation if IsSuccess returns false.
	Error() error
	// SetData sets the data into the response.
	SetData(data any)
}

// HARTracker defines the interface for recording HTTP archive logs.
type HARTracker interface {
	Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
}

// DefaultRedirectPolicy returns a function suitable for [http.Client.CheckRedirect].
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

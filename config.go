// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
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
	// CookieJar overrides the engine's default cookie storage.
	CookieJar http.CookieJar

	// CustomEngine replaces the default *http.Client engine entirely.
	CustomEngine HTTPDoer

	// ConnectionPool configures idle connection limits and keepalive timeouts.
	ConnectionPool *ConnectionPoolConfig

	// HTTP2Config configures low-level HTTP/2 connection and ping parameters.
	HTTP2Config *HTTP2Config

	// Timeout sets the maximum duration allowed for the end-to-end request transaction.
	Timeout time.Duration

	// RedirectLimit controls the maximum number of redirects followed.
	//   -2: Unset (leaves engine default behavior).
	//   -1: Unlimited redirects.
	//    0: Disables and blocks all redirects.
	//  > 0: Explicit cap on followed redirects.
	RedirectLimit int

	// InsecureSkipVerify disables TLS certificate verification globally on the transport.
	// Warning: Enabling this exposes connections to man-in-the-middle attacks.
	InsecureSkipVerify bool

	// CheckRedirect defines a custom redirect policy function.
	CheckRedirect func(req *http.Request, via []*http.Request) error
}

const redirectLimitUnset = -2

// ConnectionPoolConfig configures keep-alive connection pool boundaries
// on the underlying [http.Transport] instance.
type ConnectionPoolConfig struct {
	// IdleConnTimeout is the maximum duration an idle connection is kept open.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the maximum duration to wait for reading response headers.
	ResponseHeaderTimeout time.Duration

	// MaxIdleConns is the maximum number of idle (keep-alive) connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost is the maximum number of idle (keep-alive) connections kept per host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost is the maximum total number of concurrent connections allowed per host.
	MaxConnsPerHost int

	// ReadBufferSize specifies the size of the read buffer in bytes for I/O operations (e.g. 64 KB).
	ReadBufferSize int

	// WriteBufferSize specifies the size of the write buffer in bytes for I/O operations (e.g. 64 KB).
	WriteBufferSize int
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
	// KeepAlivePeriod sends periodic keepalive packets to maintain the connection.
	KeepAlivePeriod time.Duration

	// MaxIdleTimeout is the maximum duration without network activity before connection close.
	MaxIdleTimeout time.Duration

	// InitialPacketSize sets the initial QUIC packet size.
	InitialPacketSize uint16

	// EnableMigration enables QUIC Connection Migration.
	EnableMigration bool

	// DisablePathMTUDiscovery disables Path MTU Discovery during migration.
	DisablePathMTUDiscovery bool
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
	// ProxyAddr is the URL of the proxy server to route all traffic through.
	// Supports http, socks5, and socks5h schemes.
	ProxyAddr *url.URL

	// TransportProxy is the proxy resolution function used by the transport.
	TransportProxy func(*http.Request) (*url.URL, error)

	// DNSResolver is the custom resolver used to resolve hostnames.
	DNSResolver DNSResolver

	// SourceRotator manages a pool of local IP addresses to bind outgoing connections to in a round-robin fashion.
	SourceRotator *ip.SourceIPRotator

	// DynamicHedging configures dynamic request hedging based on the percentile RTT of recent successful requests.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// SocketController hook is executed on every raw TCP connection right after it is dialed, before any TLS handshake.
	SocketController SocketController

	// FragmentConfig specifies the configuration for splitting TLS ClientHello packets across TCP segments.
	FragmentConfig *FragmentConfig

	// HostRewrite contains custom DNS rules mapping specific hostnames to target IP addresses.
	HostRewrite *HostRewriteConfig

	// HappyEyeballsDelay staggers parallel IPv4/IPv6 dial attempts to minimize latency.
	HappyEyeballsDelay time.Duration

	// HedgingDelay defines the delay before a second, parallel request is dispatched for a slow request.
	HedgingDelay time.Duration

	// ProxyDNS routes DNS lookup requests through the SOCKS5 or HTTP CONNECT proxy
	// to prevent local DNS queries from leaking to the local ISP.
	ProxyDNS bool

	// SSRFGuard blocks requests that resolve to private or loopback IP addresses.
	SSRFGuard bool
}

// FragmentConfig specifies the chunk size and inter-chunk delay for connection fragmentation.
type FragmentConfig struct {
	// LimitBytes specifies the maximum number of bytes to subject to fragmentation.
	// Once LimitBytes is exceeded, subsequent writes pass through seamlessly.
	// Set to -1 to fragment the entire stream.
	LimitBytes int64

	MaxDelay time.Duration
	MinDelay time.Duration

	ChunkSize    int
	MinChunkSize int
	MaxChunkSize int
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

	// BrowserID selects a pre-configured uTLS ClientHello profile for TLS fingerprint emulation.
	BrowserID BrowserID
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

// CacheKey uniquely identifies a cached HTTP request without string concatenation.
type CacheKey struct {
	Method string
	URL    string
}

func (k CacheKey) String() string {
	return k.Method + ":" + k.URL
}

// CacheStore defines the contract for response caching backends.
type CacheStore interface {
	Get(ctx context.Context, key any) ([]byte, error)
	Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
}

// CacheConfig configures HTTP response caching behavior and default time-to-live.
type CacheConfig struct {
	Store      CacheStore
	DefaultTTL time.Duration
}

// CachedResponse holds a serialized HTTP response stored in cache backends.
type CachedResponse struct {
	Header     map[string][]string `json:"header"`
	BodyBase64 string              `json:"body_base64"`
	StatusCode int                 `json:"status_code"`
}

// PipelineConfig configures request-response execution phases,
// including User-Agent rotation, DPI jittering, HAR logging, and size limits.
type PipelineConfig struct {
	DPIJitter     *DPIJitterConfig
	ProxyFailover *ProxyFailoverConfig
	Hedging       *HedgingConfig
	Cache         *CacheConfig
	HAR           *HARConfig
	Redact        *RedactConfig
	SizeLimit     int64
	RotateUA      bool
	Inspect       bool
	Decompress    bool
	Validate      bool
	Challenge     bool
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

// HedgingConfig configures the request hedging delay, rate limits, and idempotency rules.
type HedgingConfig struct {
	DynamicHedging       *telemetry.DynamicHedgingConfig
	DefaultDelay         time.Duration
	MaxRequestsPerSecond int
	AllowNonReadOnly     bool
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
	BaseURL              *url.URL
	Headers              http.Header
	BaseResponse         func() BaseResponse
	BeforeRequest        []func(req *http.Request)
	AfterResponse        []func(resp *http.Response, err error)
	RefererState         *RefererState
	Logger               Logger
	DefaultMods          []RequestModifier
	ChallengeSolver      ChallengeSolver
	ChallengeDetector    ChallengeDetector
	Inspector            TrafficInspector
	HeadersCookieJar     http.CookieJar
	QueryEncoder         QueryEncoder
	ResponseValidator    func(*http.Response) error
	OnPanic              func(ctx context.Context, err any, stack []byte)
	UARotationProfiles   []BrowserProfile
	Pipeline             PipelineConfig
	MaxResponseSize      int64
	MultiReadThreshold   int64
	RefererAutomaton     bool
	MultiReadDisableDisk bool
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
	Decoder                 any
	ErrorModel              any
	ForceContentType        string
	Label                   string
	UploadProgress          ProgressFunc
	DownloadProgress        ProgressFunc
	Capturer                any
	BodyError               error
	QueryError              error
	MultipartBoundary       string
	OrderedHeaders          []string
	ALPNOverride            []string
	JA4ReportStore          *JA4ReportStore
	Fallback                FallbackFunc
	RequestTimeoutCancel    context.CancelFunc
	HedgingDelayOverride    *time.Duration
	ProxyAddr               *url.URL
	ResponseValidator       func(resp *http.Response) error
	RetryPolicy             *RetryOverride
	P0fSignature            *p0f.Signature
	SessionCache            SessionCache
	PacketPadding           *fingerprint.PaddingConfig
	SocketController        SocketController
	ClientHelloSpecProvider ClientHelloSpecProvider
	JA4Callback             func(ja4.Report)
	Metadata                map[string]any
	TraceInfo               *telemetry.TraceInfo
	HostRewrite             *HostRewriteConfig
	Pipeline                *PipelineConfig
	Fragment                *FragmentConfig
	Redact                  *RedactConfig
	CertificatePins         map[string][]string
	Modifiers               []RequestModifier
	QueryEncoder            QueryEncoder

	MultiReadThreshold int64
	TimeoutOverride    time.Duration
	CacheTTL           time.Duration
	HappyEyeballsDelay time.Duration
	TCPDelay           TCPDelayRange

	MultiReadDisableDisk    bool
	AllowNonReadOnlyHedging bool
	Debug                   bool
	InsecureSkipVerify      bool
	SSRFGuard               bool
	ProxyDNS                bool
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

var ErrRedirectDomainForbidden = errors.New("aoni: redirect domain not allowed")

// AllowedDomainsRedirectPolicy returns a CheckRedirect function that restricts HTTP redirects to a list of allowed domains.
// Supports exact domain matches ("example.com") and wildcard subdomain matches ("*.example.com").
func AllowedDomainsRedirectPolicy(allowedDomains ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}

		if req.URL == nil {
			return nil
		}

		host := strings.ToLower(req.URL.Hostname())
		allowed := false

		for _, domain := range allowedDomains {
			d := strings.ToLower(domain)
			if strings.HasPrefix(d, "*.") {
				suffix := d[1:]
				if strings.HasSuffix(host, suffix) || host == d[2:] {
					allowed = true
					break
				}
			} else if host == d {
				allowed = true
				break
			}
		}

		if !allowed {
			return fmt.Errorf("%w: %s", ErrRedirectDomainForbidden, host)
		}

		return nil
	}
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
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
	// AlpnH3 is the official Application-Layer Protocol Negotiation token
	// used to negotiate HTTP/3 over QUIC during the connection handshake.
	AlpnH3 = "h3"

	// AlpnH2 is the ALPN token used to negotiate HTTP/2 over TLS.
	AlpnH2 = "h2"

	// AlpnHTTP is the ALPN token used to negotiate classic HTTP/1.1 over TLS.
	AlpnHTTP = "http/1.1"
)

// BrowserID represents a pre-defined uTLS ClientHello profile identifier used to emulate
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

// DefaultUserAgent specifies the standard User-Agent header value applied to outbound requests
// when no custom User-Agent is declared.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// DefaultBrowserProfiles provides realistic, modern Chrome browser profiles with
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

// DefaultSensitiveHeaders lists headers scrubbed from outgoing requests during cross-origin
// redirects to prevent credential and session token leakage.
var DefaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// Config aggregates isolated configuration layers required to bootstrap,
// serialize, or deep-clone a [Client] instance safely.
type Config struct {
	Network     NetworkConfig
	Fingerprint FingerprintConfig
	Defaults    ClientDefaults
	Engine      EngineConfig
}

// EngineConfig configures settings applied directly to the underlying [HTTPDoer]
// engine (typically [*http.Client]) rather than modular transport layers.
type EngineConfig struct {
	CookieJar          http.CookieJar
	CustomEngine       HTTPDoer
	ConnectionPool     *ConnectionPoolConfig
	HTTP2Config        *HTTP2Config
	Timeout            time.Duration
	RedirectLimit      int
	InsecureSkipVerify bool
	CheckRedirect      func(req *http.Request, via []*http.Request) error
	Protocols          map[string]http.RoundTripper // Custom scheme handlers (e.g., "file", "ftp")
}

const redirectLimitUnset = -2

// ConnectionPoolConfig configures keep-alive connection boundaries
// on the underlying [http.Transport] instance.
type ConnectionPoolConfig struct {
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	ReadBufferSize        int
	WriteBufferSize       int
}

// HTTP2Config controls low-level HTTP/2 protocol parameters.
type HTTP2Config struct {
	ReadIdleTimeout time.Duration
	PingTimeout     time.Duration
	AllowHTTP       bool
}

// QUICMigrationConfig controls parameters for QUIC Connection Migration over HTTP/3.
type QUICMigrationConfig struct {
	KeepAlivePeriod         time.Duration
	MaxIdleTimeout          time.Duration
	InitialPacketSize       uint16
	EnableMigration         bool
	DisablePathMTUDiscovery bool
}

// DefaultQUICMigrationConfig returns a QUICMigrationConfig populated with stable default values.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

type staticSpecProvider struct {
	Spec *utls.ClientHelloSpec
}

func (s staticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.Spec, nil
}

// NetworkConfig configures the network transport layer, proxies, DNS resolution,
// SSRF safeguards, IP rotation, and socket controllers.
type NetworkConfig struct {
	ProxyAddr          *url.URL
	TransportProxy     func(*http.Request) (*url.URL, error)
	DNSResolver        DNSResolver
	SourceRotator      *ip.SourceIPRotator
	DynamicHedging     *telemetry.DynamicHedgingConfig
	SocketController   SocketController
	FragmentConfig     *fragment.Config
	HostRewrite        *HostRewriteConfig
	HappyEyeballsDelay time.Duration
	HedgingDelay       time.Duration
	ProxyDNS           bool
	SSRFGuard          bool
}

// Clone creates an independent deep copy of [NetworkConfig].
func (n NetworkConfig) Clone() NetworkConfig {
	cloned := n

	cloned.DynamicHedging = clonePtr(n.DynamicHedging)
	cloned.FragmentConfig = clonePtr(n.FragmentConfig)

	if n.HostRewrite != nil && n.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(n.HostRewrite.Rules))
		maps.Copy(rulesCopy, n.HostRewrite.Rules)
		cloned.HostRewrite = &HostRewriteConfig{Rules: rulesCopy}
	}

	return cloned
}

// HostRewriteConfig stores custom hostname-to-IP remapping rules for DNS overrides.
type HostRewriteConfig struct {
	Rules map[string]string
}

// DNSResolver defines the hostname-to-IP lookup resolution contract.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// FingerprintConfig groups settings related to browser TLS/JA4 evasion,
// HTTP/2 frame SETTINGS, header order serialization, and TCP packet padding.
type FingerprintConfig struct {
	TLSClientHelloID           *utls.ClientHelloID
	TLSClientHelloSpecProvider ClientHelloSpecProvider
	TLSQUICClientHelloSpec     *utls.ClientHelloSpec
	H2Configurer               HTTP2Configurer
	HeaderOrder                []string
	JA4Callback                func(ja4.Report)
	P0fSignature               *p0f.Signature
	H2Settings                 *h2.Settings
	H3Settings                 *h3.Settings
	SessionCache               SessionCache
	PacketPadding              *fingerprint.PaddingConfig
	CertificatePins            map[string][]string
	BrowserID                  BrowserID
}

// Clone creates an independent deep copy of [FingerprintConfig].
func (f FingerprintConfig) Clone() FingerprintConfig {
	cloned := f

	cloned.TLSClientHelloID = clonePtr(f.TLSClientHelloID)
	cloned.H2Settings = clonePtr(f.H2Settings)
	cloned.H3Settings = clonePtr(f.H3Settings)
	cloned.PacketPadding = clonePtr(f.PacketPadding)

	if len(f.HeaderOrder) > 0 {
		cloned.HeaderOrder = slices.Clone(f.HeaderOrder)
	}

	if len(f.CertificatePins) > 0 {
		pinsCopy := make(map[string][]string, len(f.CertificatePins))
		for k, v := range f.CertificatePins {
			pinsCopy[k] = slices.Clone(v)
		}

		cloned.CertificatePins = pinsCopy
	}

	return cloned
}

// SessionCache extends the uTLS session caching contract to support proxy-isolated TLS tickets.
type SessionCache interface {
	utls.ClientSessionCache
	SetProxyKey(key string)
}

// JA4ReportStore acts as a shared carrier used by low-level TLS dialers to record
// computed JA4 signatures and propagate them to the active telemetry context.
type JA4ReportStore struct {
	Report *ja4.Report
	Target *telemetry.TraceInfo
}

// CacheKey uniquely identifies a cached HTTP request without string concatenations.
type CacheKey struct {
	Method string
	URL    string
}

func (k CacheKey) String() string {
	return k.Method + ":" + k.URL
}

// CacheStore defines the persistence contract for response caching backends.
type CacheStore interface {
	Get(ctx context.Context, key any) ([]byte, error)
	Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
}

// CacheConfig configures HTTP response caching behavior and default TTL.
type CacheConfig struct {
	Store      CacheStore
	DefaultTTL time.Duration
}

// CachedResponse holds a serialized HTTP response stored in cache backends.
type CachedResponse struct {
	Header      map[string][]string `json:"header"`
	VaryHeaders map[string]string   `json:"vary_headers,omitempty"`
	BodyBase64  string              `json:"body_base64"`
	StatusCode  int                 `json:"status_code"`
}

// PipelineConfig configures request-response execution phases,
// including User-Agent rotation, DPI jittering, HAR logging, and body size limits.
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

// GetPipeline retrieves the request-specific PipelineConfig from context.
func GetPipeline(ctx context.Context) (PipelineConfig, bool) {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.Pipeline != nil {
		return *cfg.Pipeline, true
	}

	return PipelineConfig{}, false
}

// DPIJitterConfig configures randomized delays before sending headers or body.
type DPIJitterConfig struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

// ProxyFailoverConfig configures automatic proxy switching on connection or gateway errors.
type ProxyFailoverConfig struct {
	Proxies    []string
	RetryLimit int
}

// HedgingConfig configures request hedging delays, rate limits, and idempotency rules.
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

// RedactConfig holds the configuration for redacting sensitive headers and payload keys.
type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

// RedactConfigCtxKey is the context key used to store RedactConfig in the request context.
type RedactConfigCtxKey struct{}

// ChallengeSolver delegates WAF challenge resolution (e.g. Cloudflare JavaScript challenges)
// to an automated external driver.
type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector decides whether an incoming HTTP response represents a WAF challenge.
type ChallengeDetector func(resp *http.Response) (bool, error)

// ClientDefaults configures standard request defaults, hooks, WAF solvers, and body buffer limits.
type ClientDefaults struct {
	BaseURL              *url.URL
	BaseURLString        string
	BaseURLTrimmedString string
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
	Decoders             map[string]ResponseDecoder
	ResponseValidator    func(*http.Response) error
	OnPanic              func(ctx context.Context, err any, stack []byte)
	UARotationProfiles   []BrowserProfile
	Pipeline             PipelineConfig
	MaxResponseSize      int64
	MultiReadThreshold   int64
	RefererAutomaton     bool
	MultiReadDisableDisk bool
}

// Clone creates an independent deep copy of [ClientDefaults].
func (d ClientDefaults) Clone() ClientDefaults {
	cloned := d

	if d.Headers != nil {
		cloned.Headers = d.Headers.Clone()
	}

	if len(d.BeforeRequest) > 0 {
		cloned.BeforeRequest = slices.Clone(d.BeforeRequest)
	}

	if len(d.AfterResponse) > 0 {
		cloned.AfterResponse = slices.Clone(d.AfterResponse) //nolint:bodyclose
	}

	if len(d.DefaultMods) > 0 {
		cloned.DefaultMods = slices.Clone(d.DefaultMods)
	}

	if d.Decoders != nil {
		cloned.Decoders = make(map[string]ResponseDecoder, len(d.Decoders))
		maps.Copy(cloned.Decoders, d.Decoders)
	}

	cloned.Pipeline = d.Pipeline
	cloned.Pipeline.DPIJitter = clonePtr(d.Pipeline.DPIJitter)

	if d.Pipeline.ProxyFailover != nil {
		pf := *d.Pipeline.ProxyFailover
		pf.Proxies = slices.Clone(pf.Proxies)
		cloned.Pipeline.ProxyFailover = &pf
	}

	if d.Pipeline.Hedging != nil {
		h := *d.Pipeline.Hedging
		h.DynamicHedging = clonePtr(h.DynamicHedging)
		cloned.Pipeline.Hedging = &h
	}

	cloned.Pipeline.Cache = clonePtr(d.Pipeline.Cache)
	cloned.Pipeline.HAR = clonePtr(d.Pipeline.HAR)

	if d.Pipeline.Redact != nil {
		r := *d.Pipeline.Redact
		if r.Headers != nil {
			headersCopy := make(map[string]struct{}, len(r.Headers))
			maps.Copy(headersCopy, r.Headers)
			r.Headers = headersCopy
		}

		cloned.Pipeline.Redact = &r
	}

	if len(d.UARotationProfiles) > 0 {
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

// BrowserProfile aligns a User-Agent string with modern browser Client Hints.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// RefererState maintains thread-safe state for automatic Referer tracking.
type RefererState struct {
	mu      sync.Mutex
	lastURL string
}

// ProgressFunc represents a callback triggered periodically to monitor stream transfer progress.
type ProgressFunc = io.ProgressFunc

// RequestConfig aggregates request-scoped options and transport overrides.
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
	Fragment                *fragment.Config
	Redact                  *RedactConfig
	CertificatePins         map[string][]string
	Modifiers               []RequestModifier
	QueryEncoder            QueryEncoder
	Decoders                map[string]ResponseDecoder

	MultiReadThreshold int64
	TimeoutOverride    time.Duration
	CacheTTL           time.Duration
	HappyEyeballsDelay time.Duration
	TCPDelay           TCPDelayRange

	MultiReadDisableDisk      bool
	AllowNonReadOnlyHedging   bool
	HasExplicitAcceptEncoding bool
	Debug                     bool
	InsecureSkipVerify        bool
	SSRFGuard                 bool
	ProxyDNS                  bool
}

// ApplyDefaults merges client-level defaults into uninitialized fields of [RequestConfig].
func (cfg *RequestConfig) ApplyDefaults(c *Client) {
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

	if len(c.defaults.Decoders) > 0 {
		if cfg.Decoders == nil {
			cfg.Decoders = make(map[string]ResponseDecoder, len(c.defaults.Decoders))
		}

		for k, v := range c.defaults.Decoders {
			if _, ok := cfg.Decoders[k]; !ok {
				cfg.Decoders[k] = v
			}
		}
	}

	if len(c.fingerprint.CertificatePins) > 0 {
		c.mergeCertificatePins(cfg)
	}
}

// LookupDecoder resolves a registered [ResponseDecoder] for contentType using request-level decoders or client defaults.
func (cfg *RequestConfig) LookupDecoder(contentType string) ResponseDecoder {
	mediaType, _, _ := strings.Cut(contentType, ";")

	norm := strings.ToLower(strings.TrimSpace(mediaType))
	if norm != "" && cfg.Decoders != nil {
		if d, ok := cfg.Decoders[norm]; ok {
			return d
		}
	}

	return nil
}

func (c *Client) mergeCertificatePins(cfg *RequestConfig) {
	for domain, hashes := range c.fingerprint.CertificatePins {
		for _, h := range hashes {
			if cfg.CertificatePins == nil {
				cfg.CertificatePins = make(map[string][]string)
			}

			if !slices.Contains(cfg.CertificatePins[domain], h) {
				cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], h)
			}
		}
	}
}

type requestConfigKey struct{}

// GetRequestConfig retrieves the RequestConfig instance attached to the context.
func GetRequestConfig(ctx context.Context) *RequestConfig {
	cfg, _ := ctx.Value(requestConfigKey{}).(*RequestConfig)
	return cfg
}

// QueryEncoder marshals arbitrary structures or maps into [url.Values].
type QueryEncoder func(any) (url.Values, error)

// ClientHelloSpecProvider generates or retrieves a uTLS ClientHelloSpec dynamically.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// TrafficInspector captures and records request traces and headers for diagnostics.
type TrafficInspector interface {
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *telemetry.TraceInfo)
}

// SocketController directly configures underlying TCP sockets before SYN packets are written.
type SocketController interface {
	Control(fd uintptr, network, address string) error
}

// HTTP2Configurer customizes the [golang.org/x/net/http2.Transport] instance.
type HTTP2Configurer interface {
	ConfigureHTTP2(t *http2.Transport) error
}

// Logger specifies the structured diagnostic logging interface.
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

// BaseResponseProvider provides a [BaseResponse] model for structured unwrapping.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// LoggerProvider provides access to the diagnostic Logger instance.
type LoggerProvider interface {
	Logger() Logger
}

// BaseResponse is implemented by user-defined response wrappers for unified API response parsing.
type BaseResponse interface {
	IsSuccess() bool
	Error() error
	SetData(data any)
}

// HARTracker records HTTP transactions into HAR session logs.
type HARTracker interface {
	Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
}

// AllowedDomainsRedirectPolicy restricts HTTP redirects strictly to allowed domain patterns.
// Supports exact domain matches ("example.com") and wildcard subdomains ("*.example.com").
func AllowedDomainsRedirectPolicy(allowedDomains ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if req.URL == nil {
			return nil
		}

		host := strings.ToLower(req.URL.Hostname())
		for _, domainPattern := range allowedDomains {
			if matchDomainPattern(host, domainPattern) {
				return nil
			}
		}

		return &Error{Op: "redirect", Target: host, Err: ErrRedirectDomainForbidden}
	}
}

func matchDomainPattern(host, pattern string) bool {
	p := strings.ToLower(pattern)
	if !strings.HasPrefix(p, "*.") {
		return host == p
	}

	suffix := p[1:]

	return strings.HasSuffix(host, suffix) || host == p[2:]
}

// DefaultRedirectPolicy creates an http.Client.CheckRedirect function enforcing redirect limits
// and scrubbing sensitive authentication headers during cross-origin redirects.
func DefaultRedirectPolicy(
	maxRedirects int,
	sensitiveHeaders ...string,
) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if maxRedirects >= 0 && len(via) >= maxRedirects {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if len(via) == 0 {
			return nil
		}

		headersToScrub := sensitiveHeaders
		if len(headersToScrub) == 0 {
			headersToScrub = DefaultSensitiveHeaders
		}

		if isCrossOrigin(req.URL, via[0].URL) {
			for _, h := range headersToScrub {
				req.Header.Del(h)
			}
		}

		return nil
	}
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}

	val := *p

	return &val
}

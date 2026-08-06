// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

type (
	PipelineConfig      = pipeline.PipelineConfig
	DPIJitterConfig     = pipeline.DPIJitterConfig
	ProxyFailoverConfig = pipeline.ProxyFailoverConfig
	HedgingConfig       = pipeline.HedgingConfig
	HARConfig           = pipeline.HARConfig
	RedactConfig        = pipeline.RedactConfig
	CacheConfig         = pipeline.CacheConfig
	HostRewriteConfig   = pipeline.HostRewriteConfig
	BrowserProfile      = pipeline.BrowserProfile
	RefererState        = pipeline.RefererState
)

const (
	// AlpnH3 is the official Application-Layer Protocol Negotiation token
	// used to negotiate HTTP/3 over QUIC during the connection handshake.
	AlpnH3 = "h3"

	// AlpnH2 is the ALPN token used to negotiate HTTP/2 over TLS.
	AlpnH2 = "h2"

	// AlpnHTTP is the ALPN token used to negotiate classic HTTP/1.1 over TLS.
	AlpnHTTP = "http/1.1"

	// DefaultUserAgent specifies the standard User-Agent header value applied to outbound requests
	// when no custom User-Agent is declared.
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	redirectLimitUnset = -2
)

// BrowserID represents a pre-defined uTLS ClientHello profile identifier used to emulate
// modern web browsers during TLS handshake negotiations.
type BrowserID int

const (
	// BrowserNone disables TLS fingerprint emulation, falling back to standard Go TLS.
	BrowserNone BrowserID = iota
	BrowserChrome
	BrowserFirefox
	BrowserSafari
)

var DefaultAcceptEncoding = []string{"zstd, br, gzip"}

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

func (c Config) Clone() Config {
	return Config{
		Network:     c.Network.Clone(),
		Fingerprint: c.Fingerprint.Clone(),
		Defaults:    c.Defaults.Clone(),
		Engine:      c.Engine,
	}
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
	Protocols          map[string]http.RoundTripper
}

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

// NetworkConfig configures the network transport layer, proxies, DNS resolution,
// SSRF safeguards, IP rotation, and socket controllers.
type NetworkConfig struct {
	ProxyAddr          *url.URL
	TransportProxy     func(*http.Request) (*url.URL, error)
	DNSResolver        DNSResolver
	StackDriver        netdial.RawStackDriver
	L2Device           netdial.L2Device
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
	CertCompression            []cert.CompressionAlgorithm
	BrowserID                  BrowserID
}

func (f FingerprintConfig) Clone() FingerprintConfig {
	cloned := f
	cloned.TLSClientHelloID = clonePtr(f.TLSClientHelloID)
	cloned.H2Settings = clonePtr(f.H2Settings)
	cloned.H3Settings = clonePtr(f.H3Settings)
	cloned.PacketPadding = clonePtr(f.PacketPadding)

	if len(f.HeaderOrder) > 0 {
		cloned.HeaderOrder = slices.Clone(f.HeaderOrder)
	}

	if len(f.CertCompression) > 0 {
		cloned.CertCompression = slices.Clone(f.CertCompression)
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

func (d ClientDefaults) Clone() ClientDefaults {
	cloned := d

	if d.RefererState != nil {
		d.RefererState.Mu.Lock()
		lastURL := d.RefererState.LastURL
		d.RefererState.Mu.Unlock()

		cloned.RefererState = &RefererState{LastURL: lastURL}
	} else {
		cloned.RefererState = &RefererState{}
	}

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

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}

	val := *p

	return &val
}

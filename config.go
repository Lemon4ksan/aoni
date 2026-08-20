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
	"time"

	"github.com/lemon4ksan/foundation/generic"
	fip "github.com/lemon4ksan/foundation/net/ip"
	furl "github.com/lemon4ksan/foundation/net/url"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
)

const (
	// AlpnH3 specifies the Application-Layer Protocol Negotiation (ALPN) token
	// for negotiating HTTP/3 over QUIC transport during TLS 1.3 handshakes (RFC 9114).
	AlpnH3 = "h3"

	// AlpnH2 specifies the ALPN token for negotiating HTTP/2 over TLS 1.2+ (RFC 7540/9113).
	AlpnH2 = "h2"

	// AlpnHTTP specifies the ALPN token for negotiating classic HTTP/1.1 over TLS.
	AlpnHTTP = "http/1.1"

	// DefaultUserAgent defines the fallback Chrome/Windows User-Agent header used
	// when no browser persona or custom User-Agent is declared.
	DefaultUserAgent = fingerprint.DefaultUserAgent

	// RedirectLimitDefault is the default redirect limit, which is 10.
	RedirectLimitDefault = -1

	// RedirectLimitUnset is a special value that indicates bypassing redirect policy check.
	RedirectLimitUnset = -2
)

// Network represents an L4 transport or IPC socket network protocol (e.g. "tcp", "unix").
type Network string

const (
	// NetworkTCP represents Transmission Control Protocol over IPv4 or IPv6 ("tcp").
	NetworkTCP Network = "tcp"

	// NetworkTCP4 represents Transmission Control Protocol restricted to IPv4 ("tcp4").
	NetworkTCP4 Network = "tcp4"

	// NetworkTCP6 represents Transmission Control Protocol restricted to IPv6 ("tcp6").
	NetworkTCP6 Network = "tcp6"

	// NetworkUDP represents User Datagram Protocol over IPv4 or IPv6 ("udp").
	NetworkUDP Network = "udp"

	// NetworkUDP4 represents User Datagram Protocol restricted to IPv4 ("udp4").
	NetworkUDP4 Network = "udp4"

	// NetworkUDP6 represents User Datagram Protocol restricted to IPv6 ("udp6").
	NetworkUDP6 Network = "udp6"

	// NetworkIP represents raw IP protocol over IPv4 or IPv6 ("ip").
	NetworkIP Network = "ip"

	// NetworkIP4 represents raw IP protocol restricted to IPv4 ("ip4").
	NetworkIP4 Network = "ip4"

	// NetworkIP6 represents raw IP protocol restricted to IPv6 ("ip6").
	NetworkIP6 Network = "ip6"

	// NetworkUnix represents Unix domain stream socket ("unix").
	NetworkUnix Network = "unix"

	// NetworkUnixGram represents Unix domain datagram socket ("unixgram").
	NetworkUnixGram Network = "unixgram"

	// NetworkUnixPacket represents Unix domain sequenced packet socket ("unixpacket").
	NetworkUnixPacket Network = "unixpacket"
)

// String returns the network protocol string value.
func (n Network) String() string {
	return string(n)
}

// IsTCP reports whether the network is a TCP variant ("tcp", "tcp4", "tcp6").
func (n Network) IsTCP() bool {
	return n == NetworkTCP || n == NetworkTCP4 || n == NetworkTCP6
}

// IsUDP reports whether the network is a UDP variant ("udp", "udp4", "udp6").
func (n Network) IsUDP() bool {
	return n == NetworkUDP || n == NetworkUDP4 || n == NetworkUDP6
}

// IsUnix reports whether the network is a Unix domain socket variant ("unix", "unixgram", "unixpacket").
func (n Network) IsUnix() bool {
	return n == NetworkUnix || n == NetworkUnixGram || n == NetworkUnixPacket
}

// IsIP reports whether the network is a raw IP socket variant ("ip", "ip4", "ip6").
func (n Network) IsIP() bool {
	return n == NetworkIP || n == NetworkIP4 || n == NetworkIP6
}

// ConnFilter defines a stream transformation codec evaluated during socket dialing.
// See [transport.ConnFilter].
type ConnFilter = transport.ConnFilter

// DialConfig is an alias for [transport.DialConfig].
type DialConfig = transport.DialConfig

// DefaultSensitiveHeaders defines sensitive credential and session headers
// automatically scrubbed during cross-origin HTTP redirects (RFC 9110 §15.4)
// to prevent security token leakage to untrusted third-party origins.
var DefaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// Config is the root Data Transfer Object (DTO) aggregating all client settings.
// It is 100% pure data with no dynamic runtime state or active mutex locks.
//
// Thread Safety & Mutability:
// Config objects have value semantics and are immutable once passed to NewClient.
// Calling Clone() creates a deep copy of all slices, maps, and nested pointers,
// allowing safe concurrent reuse across client instances or threads.
type Config struct {
	// Network configures L3/L4 socket dialing, proxies, DNS, and IP routing.
	Network NetworkConfig

	// Fingerprint controls TLS ClientHello, HTTP/2/3 framing, and TCP/IP evasion.
	Fingerprint FingerprintConfig

	// Defaults configures default request headers, hooks, limits, and pipeline rules.
	Defaults ClientDefaults

	// Engine configures low-level HTTP doer engines, connection pools, and redirects.
	Engine EngineConfig
}

// Clone creates a deep copy of Config, allocating fresh memory for all nested
// maps, slices, and pointer fields to guarantee strict memory isolation.
func (c Config) Clone() Config {
	return Config{
		Network:     c.Network.Clone(),
		Fingerprint: c.Fingerprint.Clone(),
		Defaults:    c.Defaults.Clone(),
		Engine:      c.Engine.Clone(),
	}
}

// EngineConfig configures settings applied directly to the underlying HTTP execution engine
// (typically standard *http.Client or fast.Client) rather than transport layers.
type EngineConfig struct {
	// CookieJar handles HTTP cookie persistence. If nil, cookies are ignored.
	// For proxy-isolated cookie storage, use [cookie.ProxyIsolatedJar].
	CookieJar http.CookieJar

	// CustomEngine overrides default engine creation with a custom HTTPDoer implementation.
	CustomEngine HTTPDoer

	// ConnectionPool configures keep-alive connection boundaries and socket buffer limits.
	ConnectionPool *ConnectionPoolConfig

	// HTTP2Config configures low-level HTTP/2 protocol timeouts and HTTP cleartext fallback.
	HTTP2Config *HTTP2Config

	// Timeout sets the maximum end-to-end execution time for an entire HTTP transaction,
	// including dial, TLS handshake, request write, server processing, and body read.
	// Set to 0 for no timeout.
	Timeout time.Duration

	// RedirectLimit sets the maximum number of HTTP redirects followed automatically.
	//   - Default (> 0): Follows up to N redirects with cross-origin header scrubbing.
	//   - 0: Disables automatic redirect following (returns 3xx response directly).
	//   - [RedirectLimitDefault]: Uses default limit (10 redirects).
	//   - [RedirectLimitUnset]: Fully bypasses redirect policy check.
	//     All redirects will be followed without any header scrubbing or security policy enforcement.
	RedirectLimit int

	// InsecureSkipVerify bypasses TLS server certificate and hostname verification.
	// WARNING: Enabling this exposes outgoing connections to Man-in-the-Middle (MitM) attacks.
	InsecureSkipVerify bool

	// CheckRedirect overrides default redirect handling logic with a custom policy.
	// If set, takes precedence over RedirectLimit.
	CheckRedirect func(req *http.Request, via []*http.Request) error

	// Protocols maps non-HTTP URL schemes (e.g. "file", "ftp", "s3", "blob") to custom RoundTrippers.
	Protocols ProtocolMap
}

// ProtocolMap maps non-HTTP URL schemes (e.g. "file", "ftp", "s3", "blob") to custom [http.RoundTripper] handlers.
type ProtocolMap map[string]http.RoundTripper

// Clone creates a memory-isolated copy of the protocol handler map.
func (p ProtocolMap) Clone() ProtocolMap {
	if p == nil {
		return nil
	}

	cloned := make(ProtocolMap, len(p))
	maps.Copy(cloned, p)

	return cloned
}

// Clone creates a deep copy of EngineConfig and its nested maps and pointers.
func (e EngineConfig) Clone() EngineConfig {
	cloned := e
	cloned.Protocols = e.Protocols.Clone()
	cloned.ConnectionPool = clonePtr(e.ConnectionPool)
	cloned.HTTP2Config = clonePtr(e.HTTP2Config)

	return cloned
}

// ConnectionPoolConfig configures HTTP/1.1 and HTTP/2 keep-alive connection boundaries,
// host limits, and socket I/O memory buffer allocations.
type ConnectionPoolConfig struct {
	// IdleConnTimeout is the maximum time an idle keep-alive connection remains open.
	// Default: 90 seconds.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the maximum time to wait for a server's response headers
	// after writing the complete request payload.
	ResponseHeaderTimeout time.Duration

	// MaxIdleConns sets the maximum number of idle keep-alive connections across all hosts.
	// Default: 100.
	MaxIdleConns int

	// MaxIdleConnsPerHost sets the maximum number of idle keep-alive connections per host.
	// Default: 2 (stdlib) or higher for high-throughput clients.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost limits the total active (busy + idle) connections per host.
	// Set to 0 for unlimited.
	MaxConnsPerHost int

	// ReadBufferSize sets the size of the read buffer allocated per connection socket (bytes).
	ReadBufferSize int

	// WriteBufferSize sets the size of the write buffer allocated per connection socket (bytes).
	WriteBufferSize int
}

// HTTP2Config configures low-level HTTP/2 protocol health checks and transport options.
type HTTP2Config struct {
	// ReadIdleTimeout is the duration of client inactivity before sending an HTTP/2 PING frame.
	ReadIdleTimeout time.Duration

	// PingTimeout is the time to wait for an HTTP/2 PING ACK before closing the connection.
	PingTimeout time.Duration

	// AllowHTTP enables unencrypted HTTP/2 over cleartext TCP (h2c / Prior Knowledge).
	AllowHTTP bool
}

// QUICMigrationConfig controls QUIC Connection Migration parameters (RFC 9000 §9).
// Connection migration allows HTTP/3 streams to survive client IP/interface changes
// (e.g. switching from Wi-Fi to Cellular) without breaking active transfers.
type QUICMigrationConfig struct {
	// KeepAlivePeriod is the interval for sending QUIC PING frames to maintain NAT bindings.
	KeepAlivePeriod time.Duration

	// MaxIdleTimeout is the duration of QUIC inactivity before closing the connection.
	MaxIdleTimeout time.Duration

	// InitialPacketSize sets the initial QUIC UDP packet size in bytes (RFC 9000 §14.1 requires >= 1200).
	InitialPacketSize uint16

	// EnableMigration toggles QUIC connection migration across network path changes.
	EnableMigration bool

	// DisablePathMTUDiscovery disables Path MTU Discovery (PMTUD) over QUIC UDP sockets.
	DisablePathMTUDiscovery bool
}

// DefaultQUICMigrationConfig returns a QUICMigrationConfig initialized with production defaults.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

// ExperimentalFlag defines bitwise feature flags for opt-in hardware and OS optimizations.
type ExperimentalFlag uint64

const (
	// ExpKernelBypass enables io_uring / RIO kernel ring buffer I/O.
	ExpKernelBypass ExperimentalFlag = 1 << iota

	// ExpSIMD enables AVX2 / AVX-512 hardware vector acceleration.
	ExpSIMD

	// ExpZeroCopy enables Linux splice / sendfile zero-copy socket transfers.
	ExpZeroCopy

	// ExpRIO enables Windows Winsock Registered I/O extensions.
	ExpRIO

	// ExpTCPFastOpen enables 0-RTT TCP FastOpen socket connection tuning (RFC 7413).
	ExpTCPFastOpen

	// ExpBusyPoll enables low-latency kernel socket driver polling (SO_BUSY_POLL).
	ExpBusyPoll
)

// NetworkConfig configures L3/L4 transport parameters, proxy routing, DNS resolution,
// dual-stack IPv4/IPv6 racing, SSRF guards, and socket controllers.
type NetworkConfig struct {
	// Network specifies the default network protocol used for dialing (e.g. NetworkTCP, NetworkTCP4, NetworkTCP6, NetworkUnix).
	// Defaults to NetworkTCP if unset.
	Network Network

	// ProxyAddr specifies a single static proxy endpoint (HTTP, HTTPS, SOCKS5, SOCKS5h).
	// Takes precedence if TransportProxy is nil.
	ProxyAddr *url.URL

	// TransportProxy is a dynamic proxy resolution function evaluated per request.
	// Takes precedence over ProxyAddr if both are defined.
	TransportProxy func(*http.Request) (*url.URL, error)

	// DNSResolver overrides system DNS resolution with custom DoH, DoT, DoQ, or static resolvers.
	DNSResolver netutil.DNSResolver

	// StackDriver provides an optional custom user-space L3/L4 network stack driver.
	StackDriver netdial.RawStackDriver

	// L2Device provides an optional Data Link Layer (Ethernet) device interface for raw frame I/O.
	L2Device netdial.L2Device

	// SourceRotator manages round-robin rotation across multiple local egress IP addresses.
	SourceRotator *fip.SourceIPRotator

	// DynamicHedging configures RTT percentile-based speculative request hedging to eliminate tail latency.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// SocketController provides a low-level callback to manipulate socket file descriptors (fd)
	// before TCP SYN packets are transmitted.
	SocketController netutil.SocketController

	// FragmentConfig configures TCP payload write chunking to evade DPI rate/pattern inspection.
	FragmentConfig *fragment.Config

	// HostRewrite configures static hostname-to-IP/host remapping rules.
	HostRewrite *netutil.HostRewriteConfig

	// ConnFilters registers custom stream codec filters evaluated during socket dialing.
	ConnFilters []ConnFilter

	// HappyEyeballsDelay sets the IPv4/IPv6 connection racing delay (RFC 8305).
	// Default: 300ms.
	HappyEyeballsDelay time.Duration

	// HedgingDelay sets the fixed delay before launching a speculative secondary request.
	HedgingDelay time.Duration

	// InterfaceName binds outgoing sockets to a specific network interface (e.g. "eth0", "wg0").
	InterfaceName string

	// SocketMark sets a Linux netfilter socket mark (SO_MARK) for policy-based routing.
	SocketMark uint32

	// ProxyDNS forces DNS resolution to occur remotely on the proxy server (SOCKS5h / HTTP CONNECT).
	ProxyDNS bool

	// SSRFGuard blocks requests targeting private, loopback, link-local, or CGNAT IP ranges.
	SSRFGuard bool

	// TCPQuickACK enables TCP quick ACK socket option.
	TCPQuickACK bool

	// EnablePowerManagement monitors OS sleep/resume transitions and purges zombie connections.
	EnablePowerManagement bool

	// ExperimentalFlags consolidates all opt-in hardware and OS experimental features under a single bitmask.
	ExperimentalFlags ExperimentalFlag

	// CPUAffinityCores locks worker OS threads to designated CPU core indices.
	CPUAffinityCores []int
}

// HasExperimental returns true if the specified experimental flag is enabled.
func (n NetworkConfig) HasExperimental(flag ExperimentalFlag) bool {
	return (n.ExperimentalFlags & flag) != 0
}

// Clone creates a deep copy of NetworkConfig and its nested pointer structures.
func (n NetworkConfig) Clone() NetworkConfig {
	cloned := n
	cloned.DynamicHedging = clonePtr(n.DynamicHedging)
	cloned.FragmentConfig = clonePtr(n.FragmentConfig)
	cloned.CPUAffinityCores = slices.Clone(n.CPUAffinityCores)

	if n.HostRewrite != nil && n.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(n.HostRewrite.Rules))
		maps.Copy(rulesCopy, n.HostRewrite.Rules)
		cloned.HostRewrite = &netutil.HostRewriteConfig{Rules: rulesCopy}
	}

	return cloned
}

// HostRewriteConfig configures static DNS and Host header remapping rules.
type HostRewriteConfig struct {
	// Rules maps source hostnames (e.g. "api.example.com") to target addresses (e.g. "1.2.3.4:443").
	Rules map[string]string
}

// BrowserID identifies predefined browser TLS handshake emulation targets.
type BrowserID int

const (
	// BrowserNone disables TLS fingerprint emulation, falling back to standard Go TLS.
	BrowserNone BrowserID = iota
	// BrowserChrome emulates Google Chrome TLS 1.3 ClientHello fingerprints.
	BrowserChrome
	// BrowserFirefox emulates Mozilla Firefox TLS 1.3 ClientHello fingerprints.
	BrowserFirefox
	// BrowserSafari emulates Apple Safari / WebKit TLS 1.3 ClientHello fingerprints.
	BrowserSafari
)

// String returns the human-readable identifier of BrowserID.
func (b BrowserID) String() string {
	switch b {
	case BrowserChrome:
		return "Chrome"
	case BrowserFirefox:
		return "Firefox"
	case BrowserSafari:
		return "Safari"
	default:
		return "None"
	}
}

// BrowserProfile holds user-agent strings and Client Hints headers for profile rotation.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// Clone creates a deep copy of BrowserProfile and its ClientHints map.
func (b BrowserProfile) Clone() BrowserProfile {
	cloned := b
	if len(b.ClientHints) > 0 {
		cloned.ClientHints = make(map[string]string, len(b.ClientHints))
		maps.Copy(cloned.ClientHints, b.ClientHints)
	}

	return cloned
}

// FingerprintConfig controls TLS ClientHello emulation, HTTP/2 SETTINGS frames,
// header order serialization, p0f OS stack spoofing, and ECH/0-RTT features.
//
// TLS Emulation Precedence:
// When multiple TLS fingerprint options are set, the engine evaluates them in order:
//  1. TLSClientHelloSpecProvider (explicit uTLS ClientHelloSpec builder)
//  2. TLSClientHelloID (specific uTLS ClientHelloID preset)
//  3. BrowserID (predefined high-level browser profile)
type FingerprintConfig struct {
	// BrowserID specifies a predefined browser fingerprint profile.
	BrowserID BrowserID

	// TLSClientHelloID overrides BrowserID with a specific uTLS ClientHelloID preset.
	TLSClientHelloID *utls.ClientHelloID

	// TLSClientHelloSpecProvider overrides BrowserID and TLSClientHelloID
	// with a dynamically generated uTLS ClientHelloSpec.
	TLSClientHelloSpecProvider fingerprint.ClientHelloSpecProvider

	// TLSQUICClientHelloSpec configures cipher suites for HTTP/3 QUIC handshakes.
	TLSQUICClientHelloSpec *utls.ClientHelloSpec

	// HeaderOrder defines the exact HTTP/1.1 or HTTP/2 header key serialization sequence.
	HeaderOrder []string

	// H2Settings overrides default HTTP/2 SETTINGS and PRIORITY frame parameters.
	H2Settings *h2.Settings

	// H3Settings overrides default QUIC/HTTP/3 flow control receive window limits.
	H3Settings *h3.Settings

	// P0fSignature spoofs TCP/IP stack parameters (TTL, Window Size, MSS, SYN options).
	P0fSignature *p0f.Signature

	// PacketPadding adds random-length padding headers to disrupt DPI packet length analysis.
	PacketPadding *fingerprint.PaddingConfig

	// CertificatePins maps domain patterns to expected SHA-256 SPKI fingerprint hashes.
	CertificatePins map[string][]string

	// CertCompression specifies RFC 8879 certificate compression algorithms (Brotli, Zstd, Zlib).
	CertCompression []cert.CompressionAlgorithm

	// ECHConfigList contains raw RFC 9484 Encrypted Client Hello configuration bytes.
	ECHConfigList []byte

	// SessionCache handles proxy-isolated TLS session ticket resumption.
	SessionCache fingerprint.SessionCache

	// JA4Callback receives calculated JA4 fingerprint reports after TLS handshakes.
	JA4Callback func(ja4.Report)

	// H2Configurer customizes x/net/http2 transport settings.
	H2Configurer fingerprint.HTTP2Configurer

	// AutoECH automatically resolves ECH keys via DNS HTTPS (Type 65) records.
	AutoECH bool

	// Enable0RTT enables TLS 1.3 / QUIC Early Data session resumption (RFC 8446 / RFC 9001).
	// WARNING: 0-RTT data is susceptible to replay attacks; use primarily for idempotent requests.
	Enable0RTT bool
}

// Clone creates a deep copy of FingerprintConfig and its nested maps, slices, and pointers.
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

	if len(f.ECHConfigList) > 0 {
		cloned.ECHConfigList = slices.Clone(f.ECHConfigList)
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

// ToPipelineFingerprint extracts pipeline-specific fingerprint parameters.
func (f FingerprintConfig) ToPipelineFingerprint() pipeline.ClientFingerprint {
	return pipeline.ClientFingerprint{
		PacketPadding: f.PacketPadding,
	}
}

// ClientDefaults configures default headers, hooks, limits, decoders, and pipeline policies.
type ClientDefaults struct {
	// BaseURL is the default root endpoint used to resolve relative request paths (RFC 3986).
	BaseURL *url.URL

	// Headers are default HTTP headers sent with every request unless overridden.
	Headers http.Header

	// MaxResponseSize caps response body reads in bytes to prevent OOM errors.
	// Set to <= 0 for unlimited. Default: 10MB.
	MaxResponseSize int64

	// MultiReadThreshold sets the RAM buffering limit before spilling over to temporary disk files.
	MultiReadThreshold int64

	// MultiReadDisableDisk forces memory-only buffering, failing if MultiReadThreshold is exceeded.
	MultiReadDisableDisk bool

	// RefererAutomaton automatically tracks and attaches Referer headers across sequential requests.
	RefererAutomaton bool

	// Pipeline holds transaction execution policies (Retry, Cache, Hedging, WAF, Jitter).
	Pipeline PipelineConfig

	// BeforeRequest hooks run sequentially before dispatching an HTTP request.
	BeforeRequest []func(req *http.Request)

	// AfterResponse hooks run sequentially after receiving an HTTP response or error.
	AfterResponse []func(resp *http.Response, err error)

	// ResponseValidator validates response status codes and headers before body unmarshaling.
	ResponseValidator func(*http.Response) error

	// SoftErrorDetectors sniffs initial body bytes for application-level soft errors without draining streams.
	SoftErrorDetectors []SoftErrorDetector

	// OnPanic handles panics occurring inside request execution pipelines.
	OnPanic func(ctx context.Context, err any, stack []byte)

	// BaseResponse provides an envelope factory function for structured API response unwrapping.
	BaseResponse func() BaseResponse

	// ChallengeSolver delegates WAF/DDoS challenge solving (e.g. Cloudflare) to external drivers.
	ChallengeSolver challenge.Solver

	// ChallengeDetector determines whether an HTTP response represents a WAF/DDoS challenge.
	ChallengeDetector challenge.Detector

	// Inspector captures and records request traces for real-time diagnostic inspection.
	Inspector telemetry.TrafficInspector

	// HeadersCookieJar provides a fallback cookie jar implementation.
	HeadersCookieJar http.CookieJar

	// QueryEncoder marshals structs or maps into URL query parameters.
	QueryEncoder QueryEncoder

	// Decoders maps MIME content types (e.g. "application/json") to response body decoders.
	Decoders map[string]ResponseDecoder

	// Logger receives structured diagnostic log events.
	Logger core.Logger

	// DefaultMods holds default functional request modifiers applied to every request.
	DefaultMods []RequestModifier

	// UARotationProfiles holds user agents and Client Hints for automatic User-Agent rotation.
	UARotationProfiles []BrowserProfile
}

// Clone creates a deep copy of ClientDefaults and all nested structures.
func (d ClientDefaults) Clone() ClientDefaults {
	cloned := d

	if d.BaseURL != nil {
		cloned.BaseURL = furl.CloneURL(d.BaseURL)
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

	if len(d.UARotationProfiles) > 0 {
		cloned.UARotationProfiles = make([]BrowserProfile, len(d.UARotationProfiles))
		for i, p := range d.UARotationProfiles {
			cloned.UARotationProfiles[i] = p.Clone()
		}
	}

	cloned.Pipeline = d.Pipeline.Clone()

	return cloned
}

// PipelineConfig configures pipeline execution rules and resilience policies.
type PipelineConfig struct {
	// DPIJitter configures packet write delay jitter to bypass DPI rate inspection.
	DPIJitter *DPIJitterConfig

	// ProxyFailover configures proxy rotation and automatic retry targets.
	ProxyFailover *ProxyFailoverConfig

	// Hedging configures speculative secondary requests to cut tail latency.
	Hedging *HedgingConfig

	// Cache configures HTTP response caching parameters.
	Cache *CacheConfig

	// HAR configures HAR 1.2 transaction logging.
	HAR *HARConfig

	// Redact configures sensitive header and payload key sanitization for logging.
	Redact *RedactConfig

	// SizeLimit sets maximum allowable response body size in bytes.
	SizeLimit int64

	// MultiReadThreshold sets RAM buffering threshold for replayable stream reads.
	MultiReadThreshold int64

	// RotateUA enables automatic User-Agent and Client Hints rotation.
	RotateUA bool

	// Inspect enables real-time traffic inspection and dashboard telemetry.
	Inspect bool

	// Decompress enables transparent response body decompression (Gzip, Brotli, Zstd).
	Decompress bool

	// Validate enables automatic response validation checking.
	Validate bool

	// Challenge enables automatic WAF/DDoS challenge detection and solving.
	Challenge bool
}

// Clone creates a deep copy of PipelineConfig and its sub-configurations.
func (p PipelineConfig) Clone() PipelineConfig {
	cloned := p
	cloned.DPIJitter = clonePtr(p.DPIJitter)

	if p.ProxyFailover != nil {
		pf := *p.ProxyFailover
		pf.Proxies = slices.Clone(pf.Proxies)
		cloned.ProxyFailover = &pf
	}

	if p.Hedging != nil {
		h := *p.Hedging
		h.DynamicHedging = clonePtr(h.DynamicHedging)
		cloned.Hedging = &h
	}

	if p.Cache != nil {
		c := p.Cache.Clone()
		cloned.Cache = &c
	}

	cloned.HAR = clonePtr(p.HAR)

	if p.Redact != nil {
		r := *p.Redact
		if r.Headers != nil {
			headersCopy := make(map[string]struct{}, len(r.Headers))
			maps.Copy(headersCopy, r.Headers)
			r.Headers = headersCopy
		}

		if len(r.HeadersToRedact) > 0 {
			r.HeadersToRedact = slices.Clone(r.HeadersToRedact)
		}

		if len(r.JSONKeysToRedact) > 0 {
			r.JSONKeysToRedact = slices.Clone(r.JSONKeysToRedact)
		}

		cloned.Redact = &r
	}

	return cloned
}

// DPIJitterConfig configures randomized delay bounds applied between socket writes
// to confuse Deep Packet Inspection (DPI) rate and timing analysis.
type DPIJitterConfig struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

// ProxyFailoverConfig configures proxy pool rotation and automatic failover
// when a proxy node fails or returns gateway errors.
type ProxyFailoverConfig struct {
	Proxies    []string
	RetryLimit int
}

// HedgingConfig configures speculative secondary requests dispatched after a delay
// if the initial request has not completed, drastically reducing p99 tail latency.
type HedgingConfig struct {
	// DynamicHedging enables adaptive percentile RTT hedging delay calculation.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// DefaultDelay is the fixed delay before launching a secondary request.
	DefaultDelay time.Duration

	// MaxRequestsPerSecond caps total hedged requests per second.
	MaxRequestsPerSecond int

	// AllowNonReadOnly permits request hedging for non-idempotent HTTP methods (POST/PUT/DELETE).
	// WARNING: Enabling this may cause duplicate mutations on the server.
	AllowNonReadOnly bool
}

// HARConfig configures HAR 1.2 transaction recording for session exports.
type HARConfig struct {
	Tracker telemetry.HARTracker
}

// RedactConfig configures sensitive header and JSON payload key sanitization
// to prevent credential leakage in log files or traffic dumps.
type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

// CacheConfig configures HTTP response caching using RFC 9211 No-Vary-Search normalization.
type CacheConfig struct {
	// Store provides the persistence backend for cached response payloads.
	Store cache.Store

	// DefaultTTL sets the fallback cache expiration duration if no Cache-Control header is present.
	DefaultTTL time.Duration

	// NoVarySearch configures URL query parameter stripping for cache key normalization (RFC 9211).
	NoVarySearch *NoVarySearchConfig

	// CookieIndices specifies cookie names hashed into the cache key for cookie-aware caching.
	CookieIndices []string
}

// Clone creates a deep copy of CacheConfig and its nested structures.
func (c CacheConfig) Clone() CacheConfig {
	cloned := c
	if c.NoVarySearch != nil {
		nv := c.NoVarySearch.Clone()
		cloned.NoVarySearch = &nv
	}

	if len(c.CookieIndices) > 0 {
		cloned.CookieIndices = slices.Clone(c.CookieIndices)
	}

	return cloned
}

// NoVarySearchConfig configures RFC 9211 No-Vary-Search URL query parameter normalization.
// Normalization strips marketing/tracking parameters (e.g. utm_source, gclid) to maximize cache hit rates.
type NoVarySearchConfig struct {
	VaryByHeaders   []string
	IgnoreParams    []string
	ExceptParams    []string
	IgnoreAllParams bool
}

// Clone creates a deep copy of NoVarySearchConfig and its header/param slices.
func (n NoVarySearchConfig) Clone() NoVarySearchConfig {
	cloned := n
	if len(n.VaryByHeaders) > 0 {
		cloned.VaryByHeaders = slices.Clone(n.VaryByHeaders)
	}

	if len(n.IgnoreParams) > 0 {
		cloned.IgnoreParams = slices.Clone(n.IgnoreParams)
	}

	if len(n.ExceptParams) > 0 {
		cloned.ExceptParams = slices.Clone(n.ExceptParams)
	}

	return cloned
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}

	return generic.Ptr(*p)
}

// RequestConfig aggregates request-scoped execution options, transport overrides,
// tracing carriers, and custom metadata attached to an in-flight HTTP transaction.
type RequestConfig = pipeline.RequestConfig

// GetRequestConfig retrieves the RequestConfig instance attached to the context.
var GetRequestConfig = pipeline.GetRequestConfig

// DrainAndClose drains unread response body payload to preserve Keep-Alive connections,
// closes the response body, and recycles request context resources.
func DrainAndClose(resp *http.Response) {
	pipeline.CloseResponse(resp)
}

// CloseResponse drains unread response body payload to preserve Keep-Alive connections,
// closes the response body stream, and recycles request context resources.
// It is an alias for [DrainAndClose].
func CloseResponse(resp *http.Response) {
	DrainAndClose(resp)
}

func (c *Client) applyRequestConfigDefaults(cfg *RequestConfig) {
	if !cfg.SSRFGuard {
		cfg.SSRFGuard = c.cfg.Network.SSRFGuard
	}

	if !cfg.ProxyDNS {
		cfg.ProxyDNS = c.cfg.Network.ProxyDNS
	}

	if !cfg.MultiReadDisableDisk {
		cfg.MultiReadDisableDisk = c.cfg.Defaults.MultiReadDisableDisk
	}

	if cfg.HappyEyeballsDelay == 0 {
		cfg.HappyEyeballsDelay = c.cfg.Network.HappyEyeballsDelay
	}

	if cfg.MultiReadThreshold == 0 {
		cfg.MultiReadThreshold = c.cfg.Defaults.MultiReadThreshold
	}

	if cfg.ProxyAddr == nil {
		cfg.ProxyAddr = c.cfg.Network.ProxyAddr
	}

	if cfg.P0fSignature == nil {
		cfg.P0fSignature = c.cfg.Fingerprint.P0fSignature
	}

	if cfg.SessionCache == nil {
		cfg.SessionCache = c.cfg.Fingerprint.SessionCache
	}

	if cfg.PacketPadding == nil {
		cfg.PacketPadding = c.cfg.Fingerprint.PacketPadding
	}

	if cfg.SocketController == nil {
		cfg.SocketController = c.cfg.Network.SocketController
	}

	if cfg.ClientHelloSpecProvider == nil {
		cfg.ClientHelloSpecProvider = c.cfg.Fingerprint.TLSClientHelloSpecProvider
	}

	if cfg.JA4Callback == nil {
		cfg.JA4Callback = c.cfg.Fingerprint.JA4Callback
	}

	if cfg.QueryEncoder == nil && c.cfg.Defaults.QueryEncoder != nil {
		cfg.QueryEncoder = c.cfg.Defaults.QueryEncoder
	}

	if len(c.cfg.Defaults.Decoders) > 0 {
		if cfg.Decoders == nil {
			cfg.Decoders = make(map[string]core.ResponseDecoder, len(c.cfg.Defaults.Decoders))
		}

		for k, v := range c.cfg.Defaults.Decoders {
			if _, ok := cfg.Decoders[k]; !ok {
				cfg.Decoders[k] = v
			}
		}
	}

	if len(c.cfg.Fingerprint.CertificatePins) > 0 {
		c.mergeCertificatePins(cfg)
	}
}

// mergeCertificatePins merges client-level SHA-256 certificate pins into a per-request config.
func (c *Client) mergeCertificatePins(cfg *RequestConfig) {
	for domain, hashes := range c.cfg.Fingerprint.CertificatePins {
		if len(hashes) == 0 {
			continue
		}

		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string, len(c.cfg.Fingerprint.CertificatePins))
		}

		existing := cfg.CertificatePins[domain]
		if len(existing) == 0 {
			cfg.CertificatePins[domain] = slices.Clone(hashes)
			continue
		}

		for _, h := range hashes {
			if !slices.Contains(existing, h) {
				existing = append(existing, h)
			}
		}

		cfg.CertificatePins[domain] = existing
	}
}

// resolvePipeline computes the active PipelineConfig for an outgoing HTTP request context.
func (c *Client) resolvePipeline(req *http.Request) PipelineConfig {
	if p, ok := pipeline.GetPipeline(req.Context()); ok {
		return pipelineToAoniConfig(p)
	}

	pipe := c.cfg.Defaults.Pipeline
	if !pipe.RotateUA && len(c.cfg.Defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.cfg.Defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.cfg.Defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.cfg.Network.HedgingDelay > 0 || c.cfg.Network.DynamicHedging != nil) {
		pipe.Hedging = &HedgingConfig{
			DefaultDelay:   c.cfg.Network.HedgingDelay,
			DynamicHedging: c.cfg.Network.DynamicHedging,
		}
	}

	return pipe
}

// toPipelineDefaults maps ClientDefaults into internal pipeline.ClientDefaults DTOs.
//
//nolint:bodyclose // SoftErrorDetectors and ResponseValidator inspect responses without taking ownership of response lifecycle.
func (c *Client) toPipelineDefaults() pipeline.ClientDefaults {
	return pipeline.ClientDefaults{
		Headers:              c.cfg.Defaults.Headers,
		BeforeRequest:        c.cfg.Defaults.BeforeRequest,
		AfterResponse:        c.cfg.Defaults.AfterResponse,
		Inspector:            c.cfg.Defaults.Inspector,
		ResponseValidator:    c.cfg.Defaults.ResponseValidator,
		SoftErrorDetectors:   c.cfg.Defaults.toInternalSoftErrorDetectors(),
		ChallengeDetector:    c.cfg.Defaults.ChallengeDetector,
		ChallengeSolver:      c.cfg.Defaults.ChallengeSolver,
		UARotationProfiles:   c.cfg.Defaults.toInternalProfiles(),
		RefererState:         c.referer,
		MaxResponseSize:      c.cfg.Defaults.MaxResponseSize,
		MultiReadThreshold:   c.cfg.Defaults.MultiReadThreshold,
		MultiReadDisableDisk: c.cfg.Defaults.MultiReadDisableDisk,
		RefererAutomaton:     c.cfg.Defaults.RefererAutomaton,
	}
}

//nolint:bodyclose // Soft error detectors inspect responses without taking ownership of response lifecycle.
func (d ClientDefaults) toInternalSoftErrorDetectors() []func(*http.Response, []byte) error {
	if len(d.SoftErrorDetectors) == 0 {
		return nil
	}

	res := make([]func(*http.Response, []byte) error, len(d.SoftErrorDetectors))
	for i, det := range d.SoftErrorDetectors {
		res[i] = det
	}

	return res
}

// toInternalProfiles translates public BrowserProfile slices to internal pipeline DTOs.
func (d ClientDefaults) toInternalProfiles() []pipeline.BrowserProfile {
	if len(d.UARotationProfiles) == 0 {
		return nil
	}

	res := make([]pipeline.BrowserProfile, len(d.UARotationProfiles))
	for i, p := range d.UARotationProfiles {
		res[i] = pipeline.BrowserProfile{
			UserAgent:   p.UserAgent,
			ClientHints: p.ClientHints,
		}
	}

	return res
}

// ToInternal translates PipelineConfig into internal [pipeline.PipelineConfig] DTOs.
func (p PipelineConfig) ToInternal() pipeline.PipelineConfig {
	return p.toInternal()
}

// toInternal translates PipelineConfig into internal pipeline.PipelineConfig DTOs.
func (p PipelineConfig) toInternal() pipeline.PipelineConfig {
	res := pipeline.PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &pipeline.DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}

	if p.ProxyFailover != nil {
		res.ProxyFailover = &pipeline.ProxyFailoverConfig{
			Proxies:    p.ProxyFailover.Proxies,
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}

	if p.Hedging != nil {
		res.Hedging = &pipeline.HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}

	if p.Cache != nil {
		var nvs *pipeline.NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}

		res.Cache = &pipeline.CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}

	if p.HAR != nil {
		res.HAR = &pipeline.HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}

	if p.Redact != nil {
		res.Redact = &pipeline.RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}

	res.BuildFlags()

	return res
}

// pipelineToAoniConfig translates an internal pipeline.PipelineConfig DTO back into a public PipelineConfig structure.
func pipelineToAoniConfig(p pipeline.PipelineConfig) PipelineConfig {
	res := PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}

	if p.ProxyFailover != nil {
		res.ProxyFailover = &ProxyFailoverConfig{
			Proxies:    slices.Clone(p.ProxyFailover.Proxies),
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}

	if p.Hedging != nil {
		res.Hedging = &HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}

	if p.Cache != nil {
		var nvs *NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}

		res.Cache = &CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}

	if p.HAR != nil {
		res.HAR = &HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}

	if p.Redact != nil {
		res.Redact = &RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}

	return res
}

// AllowedDomainsRedirectPolicy constructs an [http.Client.CheckRedirect] policy function
// restricting HTTP redirects strictly to allowed domain patterns (e.g., "*.example.com").
// The returned policy function is stateless and safe for concurrent use.
func AllowedDomainsRedirectPolicy(allowedDomains ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if req.URL == nil {
			return nil
		}

		host := strings.ToLower(strings.TrimSuffix(req.URL.Hostname(), "."))
		for _, domainPattern := range allowedDomains {
			if furl.MatchDomainPattern(host, domainPattern) {
				return nil
			}
		}

		return &Error{Op: "redirect", Target: host, Err: ErrRedirectDomainForbidden}
	}
}

// BlockPathRedirectPolicy constructs an [http.Client.CheckRedirect] policy function
// that immediately halts and fails fast if the redirect URL matches any blocked substring or pattern.
func BlockPathRedirectPolicy(blockedPatterns ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if req.URL == nil {
			return nil
		}

		rawURL := req.URL.String()
		for _, pattern := range blockedPatterns {
			if strings.Contains(rawURL, pattern) {
				return &Error{Op: "redirect", Target: rawURL, Err: ErrRedirectBlocked}
			}
		}

		return DefaultRedirectPolicy(10)(req, via)
	}
}

// DefaultRedirectPolicy constructs an [http.Client.CheckRedirect] policy function enforcing
// redirect chain length limits and scrubbing sensitive authentication headers during cross-origin
// or HTTPS-to-HTTP downgrade redirects (RFC 9110 §15.4 / RFC 7231 §6.4).
// If maxRedirects is negative, defaults to 10 redirects.
// The returned policy function is stateless and safe for concurrent use.
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

		if furl.IsCrossOrigin(req.URL, via[0].URL) {
			for _, h := range headersToScrub {
				req.Header.Del(h)
			}
		}

		return nil
	}
}

// applyRedirectPolicy applies redirect policies to standard http.Client instances.
func applyRedirectPolicy(httpClient *http.Client, eng EngineConfig) {
	if eng.CheckRedirect != nil {
		httpClient.CheckRedirect = eng.CheckRedirect
		return
	}

	switch eng.RedirectLimit {
	case 0:
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case -1:
		httpClient.CheckRedirect = DefaultRedirectPolicy(10)
	case RedirectLimitUnset:
		return
	default:
		httpClient.CheckRedirect = DefaultRedirectPolicy(eng.RedirectLimit)
	}
}

// applyMSSLimit applies maximum segment size boundaries to TCP socket streams.
func applyMSSLimit(conn net.Conn, mss int) net.Conn {
	return transport.ApplyMSSLimit(conn, mss)
}

// BuildDialConfig converts the [Config] into a self-contained [transport.DialConfig] DTO for socket dialing.
// Read-only and safe for concurrent use.
func (c *Config) BuildDialConfig(ctx context.Context) transport.DialConfig {
	if c == nil {
		return transport.DialConfig{}
	}

	netProto := c.Network.Network.String()
	if netProto == "" {
		netProto = NetworkTCP.String()
	}

	return transport.DialConfig{
		Network:            netProto,
		DNSResolver:        c.Network.DNSResolver,
		StackDriver:        c.Network.StackDriver,
		L2Device:           c.Network.L2Device,
		SourceRotator:      c.Network.SourceRotator,
		HappyEyeballs:      c.Network.HappyEyeballsDelay,
		SSRFGuard:          c.Network.SSRFGuard,
		ProxyDNS:           c.Network.ProxyDNS,
		P0fSignature:       c.Fingerprint.P0fSignature,
		SocketController:   c.Network.SocketController,
		FragmentConfig:     c.Network.FragmentConfig,
		ProxyURL:           c.Network.ProxyAddr,
		InsecureSkipVerify: GetInsecureSkipVerify(ctx) || c.Engine.InsecureSkipVerify,
		SpecProvider:       c.Fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       c.Fingerprint.SessionCache,
		CertificatePins:    c.Fingerprint.CertificatePins,
		CertCompression:    c.Fingerprint.CertCompression,
		HeaderOrder:        c.Fingerprint.HeaderOrder,
		JA4Callback:        c.Fingerprint.JA4Callback,
		AutoECH:            c.Fingerprint.AutoECH,
		Enable0RTT:         c.Fingerprint.Enable0RTT,
		ECHConfigList:      c.Fingerprint.ECHConfigList,
		ConnFilters:        c.Network.ConnFilters,
		TCPQuickACK:        c.Network.TCPQuickACK,
		RegisteredIO:       c.Network.HasExperimental(ExpRIO),
	}
}

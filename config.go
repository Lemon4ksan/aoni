// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/fragment"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/net/ip"
	"github.com/lemon4ksan/foundation/net/urlkit"
	coreh3 "github.com/lemon4ksan/mach/proto/h3"

	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/pipeline"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/x/telemetry"
)

// ============================================================================
// Section 1: Protocols, Networks & Fundamental Constants
// ============================================================================

const (
	// AlpnH3 specifies the Application-Layer Protocol Negotiation (ALPN) token
	// for negotiating HTTP/3 over QUIC transport during TLS 1.3 handshakes (RFC 9114).
	AlpnH3 = "h3"

	// AlpnH2 specifies the ALPN token for negotiating HTTP/2 over TLS 1.2+ (RFC 9113 §3.1 & §3.2).
	AlpnH2 = "h2"

	// AlpnHTTP specifies the ALPN token for negotiating classic HTTP/1.1 over TLS.
	AlpnHTTP = "http/1.1"

	// DefaultUserAgent defines the fallback Chrome/Windows User-Agent header used
	// when no browser persona or custom User-Agent is declared.
	DefaultUserAgent = "aoni/1.0"

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

// Protocol represents a URL scheme protocol (e.g. ProtocolHTTP, ProtocolHTTPS, ProtocolFile, ProtocolFTP, ProtocolS3, ProtocolWS).
type Protocol string

const (
	// ProtocolHTTP represents unencrypted Hypertext Transfer Protocol ("http").
	ProtocolHTTP Protocol = "http"

	// ProtocolHTTPS represents encrypted Hypertext Transfer Protocol over TLS ("https").
	ProtocolHTTPS Protocol = "https"

	// ProtocolFile represents local filesystem URI scheme ("file").
	ProtocolFile Protocol = "file"

	// ProtocolFTP represents File Transfer Protocol ("ftp").
	ProtocolFTP Protocol = "ftp"

	// ProtocolS3 represents Amazon Simple Storage Service scheme ("s3").
	ProtocolS3 Protocol = "s3"

	// ProtocolBlob represents Azure Blob or browser Blob scheme ("blob").
	ProtocolBlob Protocol = "blob"

	// ProtocolIPFS represents InterPlanetary File System scheme ("ipfs").
	ProtocolIPFS Protocol = "ipfs"

	// ProtocolWS represents RFC 6455 unencrypted WebSocket scheme ("ws").
	ProtocolWS Protocol = "ws"

	// ProtocolWSS represents RFC 6455 encrypted WebSocket over TLS scheme ("wss").
	ProtocolWSS Protocol = "wss"
)

// String returns the string representation of Protocol.
func (p Protocol) String() string {
	return string(p)
}

// IsHTTP reports whether the protocol is standard HTTP ("http") or HTTPS ("https").
func (p Protocol) IsHTTP() bool {
	return p == ProtocolHTTP || p == ProtocolHTTPS
}

// IsSecure reports whether the protocol is an encrypted scheme ("https", "wss").
func (p Protocol) IsSecure() bool {
	return p == ProtocolHTTPS || p == ProtocolWSS
}

// IsWebSocket reports whether the protocol is WebSocket ("ws") or Secure WebSocket ("wss").
func (p Protocol) IsWebSocket() bool {
	return p == ProtocolWS || p == ProtocolWSS
}

// IsStandardHTTP reports whether the protocol belongs to standard Web HTTP/WS traffic.
func (p Protocol) IsStandardHTTP() bool {
	return p.IsHTTP() || p.IsWebSocket()
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
	header.Authorization,
	header.Cookie,
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

// ============================================================================
// Section 2: Root Config (Pure DTO)
// ============================================================================

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

	// Defaults configures default request headers, hooks, limits, and pipeline rules.
	Defaults ClientDefaults

	// Engine configures low-level HTTP doer engines, connection pools, and redirects.
	Engine EngineConfig

	// Ext provides hooks for external extension modules.
	Ext ExtensionConfig

	// Middlewares holds client-level cross-cutting interceptors registered via [option.WithMiddleware] or [Client.Use].
	Middlewares []Middleware
}

// Clone creates a deep copy of Config, allocating fresh memory for all nested
// maps, slices, and pointer fields to guarantee strict memory isolation.
func (c Config) Clone() Config {
	return Config{
		Network:     c.Network.Clone(),
		Defaults:    c.Defaults.Clone(),
		Engine:      c.Engine.Clone(),
		Ext:         c.Ext.Clone(),
		Middlewares: slices.Clone(c.Middlewares),
	}
}

// RequiresRequestContext reports whether any subsystem configuration requires attaching RequestConfig to request contexts.
func (c Config) RequiresRequestContext() bool {
	return c.Network.RequiresRequestContext() ||
		c.Defaults.RequiresRequestContext()
}

// IsBaremetalEligible reports whether the entire client configuration permits fast 0-alloc baremetal execution.
func (c Config) IsBaremetalEligible() bool {
	return len(c.Middlewares) == 0 &&
		c.Engine.CustomEngine == nil &&
		!c.RequiresRequestContext() &&
		c.Defaults.IsBaremetalEligible()
}

// BuildDialConfig converts the [Config] into a self-contained [transport.DialConfig] DTO for socket dialing.
// Read-only and safe for concurrent use.
func (c Config) BuildDialConfig(ctx context.Context) transport.DialConfig {
	netProto := c.Network.Network.String()
	if netProto == "" {
		netProto = NetworkTCP.String()
	}

	return transport.DialConfig{
		Network:            netProto,
		BaseTLSConfig:      c.Network.TLSConfig,
		DialTLSContext:     c.Ext.DialTLSContext,
		WrapTLSClient:      c.Ext.WrapTLSClient,
		DNSResolver:        c.Network.DNSResolver,
		StackDriver:        c.Network.StackDriver,
		L2Device:           c.Network.L2Device,
		SourceRotator:      c.Network.SourceRotator,
		HappyEyeballs:      c.Network.HappyEyeballsDelay,
		SSRFGuard:          c.Network.SSRFGuard,
		ProxyDNS:           c.Network.ProxyDNS,
		SocketController:   c.Network.SocketController,
		FragmentConfig:     c.Network.FragmentConfig,
		ProxyURL:           c.Network.ProxyAddr,
		InsecureSkipVerify: GetInsecureSkipVerify(ctx) || c.Engine.InsecureSkipVerify,
		ConnFilters:        c.Network.ConnFilters,
		TCPQuickACK:        c.Network.TCPQuickACK,
		RegisteredIO:       c.Network.HasExperimental(ExpRIO),
	}
}

// ============================================================================
// Section 3: Engine & Connection Pool Configuration
// ============================================================================

// EngineConfig governs low-level HTTP execution engine parameters, connection pool boundaries,
// socket I/O memory buffers, protocol-specific keep-alive probes, and redirect policies.
//
// These settings apply directly to the underlying HTTP execution handler (standard [*http.Client]
// or [fast.Client]) rather than dial-level transport layers.
type EngineConfig struct {
	// CookieJar manages HTTP cookie persistence and RFC 6265 lifecycle policies across transactions.
	//
	// Security & Proxy Isolation:
	// If nil, cookies received in Set-Cookie response headers are discarded immediately.
	// For multi-tenant or rotating proxy architectures, use [cookie.ProxyIsolatedJar] to prevent
	// session identification and cross-proxy cookie leakage.
	CookieJar CookieJar

	// CustomEngine overrides default engine instantiation with a custom [HTTPDoer] execution handler.
	// Useful for dependency injection, recorded replay fixtures, or specialized in-memory engines.
	CustomEngine HTTPDoer

	// ConnectionPool configures keep-alive socket boundaries, host limits, and I/O buffer allocations.
	// If nil, standard library transport defaults (MaxIdleConns=100, IdleConnTimeout=90s) are applied.
	ConnectionPool *ConnectionPoolConfig

	// HTTP2Config configures low-level HTTP/2 protocol timeouts, PING keep-alives, and cleartext h2c.
	// If nil, default HTTP/2 protocol parameters are inherited from Go runtime transport.
	HTTP2Config *HTTP2Config

	// Timeout specifies the maximum end-to-end execution time for an entire HTTP transaction,
	// spanning DNS lookup, TCP dial, TLS handshake, request serialization, server processing, and body read.
	//
	// Invariant:
	// A timeout of 0 disables client-level timeouts, leaving deadline control exclusively to [context.Context].
	Timeout time.Duration

	// RedirectLimit enforces the maximum allowable HTTP redirect hops followed automatically (RFC 9110 §15.4).
	//
	// Operational Modes:
	//   - Default (> 0): Automatically follows up to N redirects with cross-origin credential scrubbing.
	//   - 0: Disables automatic redirect following, returning 3xx responses directly to the caller.
	//   - [RedirectLimitDefault] (-1): Applies the standard browser limit (10 redirects).
	//   - [RedirectLimitUnset] (-2): Fully disables redirect limits, following redirects unconditionally.
	RedirectLimit int

	// InsecureSkipVerify controls whether the client verifies the server's certificate chain and host name.
	//
	// CAUTION: Man-in-the-Middle (MitM) Vulnerability:
	// When true, crypto/tls accepts any certificate presented by the server and any host name in that certificate.
	// This should ONLY be used in controlled development, local proxy sniffing, or self-signed staging environments.
	InsecureSkipVerify bool

	// CheckRedirect overrides default redirect handling logic with a custom policy function.
	//
	// Precedence Invariant:
	// If non-nil, CheckRedirect takes precedence over [EngineConfig.RedirectLimit].
	CheckRedirect func(req *http.Request, via []*http.Request) error

	// Protocols maps non-HTTP URL schemes (e.g. ProtocolFile, ProtocolFTP, ProtocolS3, ProtocolBlob) to custom RoundTrippers.
	Protocols ProtocolMap

	// DigestAuth configures RFC 7616 HTTP Digest Access Authentication credentials for automatic 401 challenge resolution.
	DigestAuth *DigestAuthConfig

	// EnableH2 enables and forces HTTP/2 transport.
	EnableH2 bool

	// EnableH3 enables and forces HTTP/3 QUIC transport.
	EnableH3 bool

	// H3Settings configures low-level HTTP/3 protocol parameters.
	H3Settings *coreh3.Settings
}

// DigestAuthConfig holds RFC 7616 HTTP Digest Access Authentication credentials.
type DigestAuthConfig struct {
	// Username is the authentication principal identity.
	Username string

	// Password is the shared secret for Digest challenge response calculation.
	Password string
}

// ProtocolMap maps URL protocol schemes to custom [http.RoundTripper] handlers.
type ProtocolMap map[Protocol]http.RoundTripper

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
	cloned.DigestAuth = clonePtr(e.DigestAuth)

	if e.H3Settings != nil {
		sCopy := *e.H3Settings
		if e.H3Settings.Other != nil {
			sCopy.Other = make(map[uint64]uint64, len(e.H3Settings.Other))
			maps.Copy(sCopy.Other, e.H3Settings.Other)
		}

		cloned.H3Settings = &sCopy
	}

	return cloned
}

// ConnectionPoolConfig configures HTTP/1.1 and HTTP/2 keep-alive connection boundaries,
// host limits, and socket I/O memory buffer allocations.
type ConnectionPoolConfig struct {
	// IdleConnTimeout defines the maximum idle duration an unused keep-alive socket remains cached in the pool.
	//
	// Pool Janitor Mechanics:
	// Once an idle connection exceeds this threshold, background pool janitors close the socket
	// to prevent holding open stale kernel file descriptors. Default: 90 seconds.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout sets the maximum time allowed to wait for a server's response headers
	// after writing the complete request payload.
	//
	// Stalled Connection Detection:
	// Prevents goroutines from hanging indefinitely when backend servers accept TCP requests but fail to reply.
	// Set to 0 for unlimited.
	ResponseHeaderTimeout time.Duration

	// MaxIdleConns sets the maximum number of idle keep-alive connections across all target hosts.
	// Default: 100.
	MaxIdleConns int

	// MaxIdleConnsPerHost sets the maximum number of idle keep-alive connections maintained per host.
	// Default: 2 (stdlib standard) or higher for high-throughput clients.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost bounds the total active (busy + idle) connections permitted per target host.
	//
	// Rate Throttling & Resource Protection:
	// When active connections reach this limit, subsequent dials block until existing sockets return to the pool.
	// Set to 0 for unlimited.
	MaxConnsPerHost int

	// ReadBufferSize sets the size of the OS read buffer allocated per connection socket (bytes).
	// Default: 4KB (4096 bytes). Larger buffers (e.g. 64KB) optimize high-throughput payload streaming.
	ReadBufferSize int

	// WriteBufferSize sets the size of the OS write buffer allocated per connection socket (bytes).
	// Default: 4KB (4096 bytes). Larger buffers reduce write syscall frequency during large multipart uploads.
	WriteBufferSize int
}

// HTTP2Config configures low-level HTTP/2 framing, PING keep-alive health probes, and cleartext h2c.
type HTTP2Config struct {
	// ReadIdleTimeout specifies client inactivity duration before transmitting an HTTP/2 PING frame.
	//
	// Keep-Alive Liveness Probing:
	// Periodically tests whether the remote edge node is responsive, detecting half-open TCP connections.
	ReadIdleTimeout time.Duration

	// PingTimeout defines the duration to wait for an HTTP/2 PING ACK before terminating the connection.
	PingTimeout time.Duration

	// AllowHTTP enables unencrypted HTTP/2 over cleartext TCP (Starting HTTP/2 with Prior Knowledge, RFC 9113 §3.3).
	// When true, allows HTTP/2 framing without TLS handshakes on trusted internal VPCs or local microservices.
	AllowHTTP bool
}

// QUICMigrationConfig controls QUIC Connection Migration parameters (RFC 9000 §9).
//
// Zero-Disruption Network Switching:
// Connection migration allows HTTP/3 streams to survive client IP and network interface changes
// (e.g. switching from Wi-Fi to Cellular or roaming across base stations) without breaking active transfers.
type QUICMigrationConfig struct {
	// KeepAlivePeriod defines the interval for transmitting QUIC PING frames to preserve NAT traversal bindings.
	KeepAlivePeriod time.Duration

	// MaxIdleTimeout sets the duration of QUIC inactivity before gracefully closing the connection.
	MaxIdleTimeout time.Duration

	// InitialPacketSize sets the initial QUIC UDP datagram size in bytes (RFC 9000 §14.1 requires >= 1200).
	InitialPacketSize uint16

	// EnableMigration toggles QUIC connection migration across network path and IP changes.
	EnableMigration bool

	// DisablePathMTUDiscovery disables dynamic Path MTU Discovery (PMTUD) over QUIC UDP sockets.
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

// ============================================================================
// Section 4: L3/L4 Network Layer Configuration
// ============================================================================

// NetworkConfig configures L3/L4 transport parameters, proxy routing, custom DNS resolution,
// dual-stack IPv4/IPv6 racing (Happy Eyeballs v2 / RFC 8305), SSRF guards, and kernel socket options.
type NetworkConfig struct {
	// Network specifies the default network protocol used for socket dialing.
	// Supported options: [NetworkTCP], [NetworkTCP4], [NetworkTCP6], [NetworkUnix].
	// Defaults to [NetworkTCP] if unset.
	Network Network

	// ProxyAddr specifies a static proxy endpoint URL (HTTP, HTTPS, SOCKS5, SOCKS5h).
	// Takes precedence if TransportProxy is nil.
	ProxyAddr *url.URL

	// TransportProxy is a dynamic proxy resolution function evaluated per request.
	// Takes precedence over ProxyAddr if both are defined, enabling dynamic proxy rotation.
	TransportProxy func(*http.Request) (*url.URL, error)

	// DNSResolver overrides the host operating system's DNS resolver with high-performance
	// custom DNS over HTTPS (DoH / RFC 8484), DNS over TLS (DoT / RFC 7858), DNS over QUIC (DoQ / RFC 9250),
	// or static in-memory DNS tables.
	DNSResolver netutil.DNSResolver

	// StackDriver provides an optional custom user-space L3/L4 network stack driver.
	StackDriver netdial.RawStackDriver

	// L2Device provides an optional Data Link Layer (Ethernet) device interface for raw frame I/O.
	L2Device netdial.L2Device

	// SourceRotator manages round-robin or least-used rotation across multiple local egress IP addresses.
	SourceRotator *ip.SourceIPRotator

	// DynamicHedging configures real-time EWMA latency-based speculative request hedging to eliminate tail latency.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// SocketController provides a low-level callback invoked immediately after socket creation,
	// allowing arbitrary manipulation of raw file descriptors (fd) via setsockopt before TCP SYN transmission.
	SocketController netutil.SocketController

	// FragmentConfig configures TCP payload write-chunking to evade Deep Packet Inspection (DPI) pattern signatures.
	FragmentConfig *fragment.Config

	// HostRewrite configures static hostname-to-IP/host remapping rules, overriding DNS resolution for specific domains.
	HostRewrite *netutil.HostRewriteConfig

	// ConnFilters registers custom stream codec filters evaluated during socket dialing.
	ConnFilters []ConnFilter

	// HappyEyeballsDelay defines the head-start delay between IPv6 and IPv4 connection attempts (RFC 8305 §5).
	// The client attempts IPv6 first, launching a concurrent IPv4 dial after this delay if IPv6 has not connected.
	// Evaluated in [transport.UniversalDialer] during dual-stack racing.
	// Default: 300ms.
	HappyEyeballsDelay time.Duration

	// HedgingDelay sets the fixed fallback duration before launching a secondary speculative request.
	//
	// Precedence & Pipeline Interaction:
	// Serves as default dial hedging delay. If [PipelineConfig.Hedging] is nil, HedgingDelay and
	// [NetworkConfig.DynamicHedging] are automatically synthesized into a [HedgingConfig] during request execution.
	HedgingDelay time.Duration

	// InterfaceName binds outgoing sockets to a designated network interface (e.g. "eth0", "wlan0", "wg0").
	InterfaceName string

	// SocketMark sets a Linux netfilter socket mark (SO_MARK) for kernel policy-based routing tables.
	SocketMark uint32

	// ProxyDNS forces hostname resolution to execute remotely on the proxy server (SOCKS5h / HTTP CONNECT).
	// Prevents local DNS leaks when operating through privacy tunnels.
	ProxyDNS bool

	// SSRFGuard actively inspects resolved IP addresses, blocking outgoing requests to private (RFC 1918),
	// loopback (127.0.0.0/8), link-local (169.254.0.0/16), and Carrier-Grade NAT (100.64.0.0/10) subnets.
	// Evaluated in [transport.UniversalDialer] before socket connection.
	SSRFGuard bool

	// TCPQuickACK enables the TCP_QUICKACK socket option on Linux, disabling delayed ACKs for lower latency.
	TCPQuickACK bool

	// EnablePowerManagement attaches an OS power lifecycle watcher ([netutil/power.Watcher]) that purges stale keep-alive connections
	// upon laptop sleep/wake transitions, preventing silent 15-second write timeouts on dead sockets.
	EnablePowerManagement bool

	// ExperimentalFlags consolidates opt-in hardware and OS experimental accelerations (io_uring, SIMD, RIO, TCP Fast Open).
	ExperimentalFlags ExperimentalFlag

	// TLSConfig specifies the default TLS client configuration.
	TLSConfig *tls.Config
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

	if n.HostRewrite != nil && n.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(n.HostRewrite.Rules))
		maps.Copy(rulesCopy, n.HostRewrite.Rules)
		cloned.HostRewrite = &netutil.HostRewriteConfig{Rules: rulesCopy}
	}

	if n.TLSConfig != nil {
		cloned.TLSConfig = n.TLSConfig.Clone()
	}

	return cloned
}

// RequiresRequestContext reports whether network configurations require attaching RequestConfig to request contexts.
func (n NetworkConfig) RequiresRequestContext() bool {
	return n.SocketController != nil || n.SSRFGuard || n.ProxyAddr != nil
}

// HostRewriteConfig configures static DNS and Host header remapping rules.
type HostRewriteConfig struct {
	// Rules maps source hostnames (e.g. "api.example.com") to target addresses (e.g. "1.2.3.4:443").
	Rules map[string]string
}

// DecoderMap maps MIME content types (e.g. "application/json", "application/xml") to response body decoders.
type DecoderMap map[string]ResponseDecoder

// Clone creates a memory-isolated copy of the decoder map.
func (d DecoderMap) Clone() DecoderMap {
	if d == nil {
		return nil
	}

	cloned := make(DecoderMap, len(d))
	maps.Copy(cloned, d)

	return cloned
}

// ============================================================================
// Section 6: Client Defaults Configuration (L7 Application Defaults)
// ============================================================================

// ClientDefaults configures default headers, interceptor hooks, resource limits, decoders, and pipeline policies.
type ClientDefaults struct {
	// BaseURL is the default root endpoint used to resolve relative request paths (RFC 3986).
	// Relative subpaths (e.g. "/users/1") are resolved against BaseURL with zero allocations.
	BaseURL *url.URL

	// Headers are default HTTP headers sent with every outgoing request unless explicitly overridden.
	Headers http.Header

	// MaxResponseSize caps response body reads in bytes to prevent Out-Of-Memory (OOM) crashes.
	// Set to <= 0 for unlimited. Default: 10MB (10 * 1024 * 1024 bytes).
	//
	// Precedence:
	// Acts as global default. Overridden if [PipelineConfig.SizeLimit] is set to a non-zero value.
	MaxResponseSize int64

	// MultiReadThreshold sets the RAM buffering boundary (in bytes) before spilling over to temporary disk files.
	// Enables replayable/rewindable stream reading ([io.Seeker]) without unbounded heap growth.
	//
	// Precedence:
	// Acts as global default. Overridden if [PipelineConfig.MultiReadThreshold] is set to a non-zero value.
	MultiReadThreshold int64

	// MultiReadDisableDisk forces in-memory-only buffering, failing if MultiReadThreshold is exceeded.
	MultiReadDisableDisk bool

	// RefererAutomaton automatically tracks and attaches realistic browser Referer headers across sequential requests.
	RefererAutomaton bool

	// Pipeline holds transaction execution policies (Retry, Cache, Hedging, WAF, Jitter).
	Pipeline PipelineConfig

	// BeforeRequest hooks execute sequentially immediately before an HTTP request is dispatched on the wire.
	BeforeRequest []func(req *http.Request)

	// AfterResponse hooks execute sequentially immediately after receiving an HTTP response or transport error.
	AfterResponse []func(resp *http.Response, err error)

	// ResponseValidator validates response status codes and headers before structured unmarshaling begins.
	ResponseValidators []func(*http.Response) error

	// SoftErrorDetectors inspects initial body bytes non-destructively for application-level soft errors
	// (e.g. HTTP 200 OK responses containing HTML login pages or JSON business error payloads).
	SoftErrorDetectors []SoftErrorDetector

	// OnPanic handles panics occurring inside request execution pipelines or middleware chains.
	OnPanic func(ctx context.Context, err any, stack []byte)

	// BaseResponse provides an envelope factory function used for structured API response unwrapping.
	BaseResponse func() BaseResponse

	// Inspector captures and records request traces for real-time diagnostic telemetry inspection.
	Inspector telemetry.TrafficInspector

	// HeadersCookieJar provides a fallback cookie jar implementation.
	HeadersCookieJar CookieJar

	// QueryEncoder marshals structs or maps into URL query parameters.
	QueryEncoder QueryEncoder

	// Decoders maps MIME content types (e.g. "application/json", "application/protobuf") to response body decoders.
	Decoders DecoderMap

	// Logger receives structured diagnostic log events.
	Logger Logger

	// DefaultMods holds default functional request modifiers applied to every outgoing request.
	DefaultMods []RequestModifier

	// DictionaryStore caches HTTP compression dictionaries conforming to RFC 9842.
	DictionaryStore *dict.Store

	// DisableDictionaryCompression disables RFC 9842 compression dictionary discovery and negotiation.
	DisableDictionaryCompression bool
}

// Clone creates a deep copy of ClientDefaults and all nested structures.
func (d ClientDefaults) Clone() ClientDefaults {
	cloned := d

	if d.BaseURL != nil {
		cloned.BaseURL = urlkit.CloneURL(d.BaseURL)
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

	cloned.Decoders = d.Decoders.Clone()

	cloned.Pipeline = d.Pipeline.Clone()

	return cloned
}

// RequiresRequestContext reports whether default configurations require attaching RequestConfig to request contexts.
func (d ClientDefaults) RequiresRequestContext() bool {
	return d.QueryEncoder != nil || len(d.Decoders) > 0 || d.MultiReadThreshold > 0
}

// IsBaremetalEligible reports whether default configurations permit bypassing the pipeline.
func (d ClientDefaults) IsBaremetalEligible() bool {
	return len(d.DefaultMods) == 0 &&
		!d.RequiresRequestContext() &&
		d.Inspector == nil &&
		len(d.BeforeRequest) == 0 &&
		len(d.AfterResponse) == 0 &&
		!d.RefererAutomaton &&
		!d.Pipeline.IsActive()
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

// ============================================================================
// Section 7: Pipeline Configuration (5-Stage Execution Engine)
// ============================================================================

// PipelineConfig coordinates the behavior and resilience policies
// of the 5-stage transaction execution pipeline.
//
// # Architectural Pipeline Stages
//
// Every HTTP transaction executed by an aoni client passes through five deterministic stages:
//  1. Stage 1 (Preparation & Modifiers): Encodes bodies, injects headers, binds context metadata.
//  2. Stage 2 (Middleware & Telemetry): Enforces circuit breaking, retries, hedging, and HAR logging.
//  3. Stage 3 (Protocol Engine & Janitors): Dispatches to standard or Fast engine, manages Alt-Svc cache.
//  4. Stage 4 (L4/L7 Transport): Happy Eyeballs v3 racing, proxy failover, socket tuning.
//  5. Stage 5 (Decoders & Resilience): Decompression, soft-error sniffing, structured unmarshaling.
//
// PipelineConfig governs which stages are active and configures their memory bounds and thresholds.
type PipelineConfig struct {
	// ProxyFailover coordinates automatic proxy endpoint rotation and retry failover.
	//
	// Distributed Resiliency:
	// When active proxies experience silent TCP drops, rate limiting (HTTP 429), or egress bans,
	// ProxyFailover rotates to the next healthy proxy candidate across retries without failing the parent request.
	// If nil, proxy failover is disabled.
	ProxyFailover *ProxyFailoverConfig

	// Hedging configures speculative parallel request dispatching to eliminate p99 tail latency.
	//
	// The "Tail at Scale" Paradigm:
	// If an initial request has not returned headers within the configured percentile RTT (e.g. p95),
	// a secondary speculative request is launched concurrently. The first socket to deliver valid headers
	// wins the race, and the losing socket is cancelled immediately to preserve bandwidth.
	//
	// Precedence:
	// When nil, automatically synthesized from [NetworkConfig.HedgingDelay] and [NetworkConfig.DynamicHedging].
	// When non-nil, explicitly overrides network-level hedging defaults.
	Hedging *HedgingConfig

	// Cache configures RFC 9111 HTTP response caching and RFC 9211 No-Vary-Search normalization.
	//
	// Zero-Roundtrip Performance:
	// Evaluates Cache-Control, ETag, and Last-Modified directives against memory, Redis, or disk storage,
	// serving cached payloads with 0 network latency and transparently issuing 304 conditional validations.
	// If nil, response caching is bypassed.
	Cache *CacheConfig

	// HAR configures W3C HTTP Archive (HAR 1.2) transaction recording.
	//
	// Forensic Diagnostics & Auditing:
	// Captures nanosecond-accurate connection timings (DNS, TCP, TLS, TTFB), unredacted or sanitized headers,
	// and request/response sizes for export to Chrome DevTools or corporate compliance archives.
	// If nil, HAR tracking is disabled.
	HAR *HARConfig

	// Redact configures sensitive authentication header and JSON payload key sanitization.
	//
	// Data Loss Prevention (DLP):
	// Strips bearer tokens, API keys, passwords, and session cookies from telemetry logs, HAR files,
	// and debug dumps before they leave memory.
	// If nil, default sensitive headers (Authorization, Cookie, X-Api-Key) are redacted.
	Redact *RedactConfig

	// SizeLimit establishes the maximum permissible response body size in bytes.
	//
	// Out-Of-Memory (OOM) Defense:
	// Protects the runtime from decompression bombs, malicious endless chunked streams, and accidental
	// gigabyte downloads by bounding body reads with an [io.LimitReader]. Exceeding this boundary
	// terminates the stream immediately with an error before heap exhaustion occurs.
	// Set to <= 0 for unlimited body streaming.
	//
	// Precedence:
	// If set to 0 (unset), automatically inherits [ClientDefaults.MaxResponseSize] (default: 10MB).
	SizeLimit int64

	// MultiReadThreshold defines the RAM buffering capacity (in bytes) for rewindable response streams.
	//
	// Tiered RAM-to-Disk Spilling:
	// Responses smaller than MultiReadThreshold are buffered completely in pooled memory ([sync.Pool]).
	// Payloads exceeding this threshold transparently spill over to temporary disk files, enabling unlimited
	// stream rewindability ([io.Seeker]) without exhausting server memory.
	// Set to 0 to disable memory caching and force direct streaming.
	//
	// Precedence:
	// If set to 0 (unset), automatically inherits [ClientDefaults.MultiReadThreshold].
	MultiReadThreshold int64

	// Inspect enables real-time transaction broadcasting to the embedded Web Inspector telemetry dashboard.
	// When true, all request/response pairs are mirrored over WebSockets to the diagnostic inspector UI.
	Inspect bool

	// Decompress enables transparent RFC 9110 response body decompression.
	//
	// Multi-Codec Acceleration:
	// Automatically negotiates and decompresses "gzip", "deflate", "br" (Brotli), and "zstd" (Zstandard)
	// payload streams using streaming decoders.
	Decompress bool

	// Validate enforces application-level status code and header integrity checks before body unmarshaling.
	// When true, responses with unexpected non-2xx status codes or invalid content types are rejected early.
	Validate bool
}

// Clone creates a deep copy of PipelineConfig and its sub-configurations.
func (p PipelineConfig) Clone() PipelineConfig {
	cloned := p
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

// IsActive reports whether any pipeline stage or middleware interception is enabled.
func (p PipelineConfig) IsActive() bool {
	return p.Decompress || p.Validate || p.HAR != nil || p.Cache != nil ||
		p.Hedging != nil || p.ProxyFailover != nil || p.Inspect
}

// DPIJitterConfig configures randomized delay bounds applied between socket write operations
// to evade Deep Packet Inspection (DPI) inter-packet arrival time (IAT) analysis.
type DPIJitterConfig struct {
	// MinDelay is the minimum sleep duration injected between socket write operations.
	MinDelay time.Duration

	// MaxDelay is the maximum sleep duration injected between socket write operations.
	MaxDelay time.Duration
}

// ProxyFailoverConfig configures proxy pool health monitoring and automatic failover.
type ProxyFailoverConfig struct {
	// Proxies is the ordered list of proxy endpoint candidate URLs.
	Proxies []string

	// RetryLimit sets the maximum number of alternative proxies tried before failing the transaction.
	RetryLimit int
}

// HedgingConfig configures speculative secondary request dispatching to eliminate tail latency.
type HedgingConfig struct {
	// DynamicHedging enables adaptive percentile RTT hedging delay calculation based on EWMA metrics.
	DynamicHedging *telemetry.DynamicHedgingConfig

	// DefaultDelay is the fixed fallback delay before launching a secondary speculative request.
	DefaultDelay time.Duration

	// MaxRequestsPerSecond caps the total number of speculative requests dispatched per second to prevent self-DDoS.
	MaxRequestsPerSecond int

	// AllowNonReadOnly permits request hedging for non-idempotent HTTP methods (POST/PUT/DELETE/PATCH).
	// WARNING: Enabling this for non-idempotent operations may result in duplicate database mutations.
	AllowNonReadOnly bool
}

// HARConfig configures W3C HAR 1.2 transaction recording.
type HARConfig struct {
	// Tracker manages active transaction recording and HAR export generation.
	Tracker telemetry.HARTracker
}

// RedactConfig configures sensitive header and JSON payload key sanitization rules.
type RedactConfig struct {
	// Headers defines exact-match header names to sanitize (stored as a fast lookup set).
	Headers map[string]struct{}

	// HeadersToRedact defines header names or patterns to redact from logs and HAR traces.
	HeadersToRedact []string

	// JSONKeysToRedact defines JSON field names to redact from logged request/response payloads.
	JSONKeysToRedact []string
}

// CacheConfig configures RFC 9111 HTTP response caching and RFC 9211 No-Vary-Search normalization.
type CacheConfig struct {
	// Store provides the persistence backend (in-memory LRU, Redis, or disk) for cached payloads.
	Store cache.Store[any, []byte]

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
	// VaryByHeaders specifies HTTP headers whose values contribute to the cache key calculation.
	VaryByHeaders []string

	// IgnoreParams lists query parameter keys to ignore when computing the cache key (e.g. utm_source, fbclid).
	IgnoreParams []string

	// ExceptParams lists query parameter keys that MUST be considered even if IgnoreAllParams is true.
	ExceptParams []string

	// IgnoreAllParams causes all query parameters to be ignored except those explicitly declared in ExceptParams.
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

	return new(*p)
}

// ============================================================================
// Section 8: Request Context & Internal Pipeline Bridges
// ============================================================================

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
	if cfg.Network == "" && c.cfg.Network.Network != "" {
		cfg.Network = c.cfg.Network.Network.String()
	}

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

	if cfg.DNSResolver == nil {
		cfg.DNSResolver = c.cfg.Network.DNSResolver
	}

	if cfg.HostRewrite == nil {
		cfg.HostRewrite = c.cfg.Network.HostRewrite
	}

	if cfg.Fragment == nil {
		cfg.Fragment = c.cfg.Network.FragmentConfig
	}

	if cfg.SocketController == nil {
		cfg.SocketController = c.cfg.Network.SocketController
	}

	if cfg.QueryEncoder == nil && c.cfg.Defaults.QueryEncoder != nil {
		cfg.QueryEncoder = c.cfg.Defaults.QueryEncoder
	}

	if len(c.cfg.Defaults.Decoders) > 0 {
		if cfg.Decoders == nil {
			cfg.Decoders = maps.Clone(c.cfg.Defaults.Decoders)
		} else {
			for k, v := range c.cfg.Defaults.Decoders {
				if _, ok := cfg.Decoders[k]; !ok {
					cfg.Decoders[k] = v
				}
			}
		}
	}
}

// resolvePipeline computes the active PipelineConfig for an outgoing HTTP request context.
func (c *Client) resolvePipeline(req *http.Request) PipelineConfig {
	if p, ok := pipeline.GetPipelineConfig(req.Context()); ok {
		return pipelineToAoniConfig(p)
	}

	pipe := c.cfg.Defaults.Pipeline
	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.cfg.Defaults.MaxResponseSize
	}

	if pipe.MultiReadThreshold == 0 && c.cfg.Defaults.MultiReadThreshold != 0 {
		pipe.MultiReadThreshold = c.cfg.Defaults.MultiReadThreshold
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
		Headers:                      c.cfg.Defaults.Headers,
		BeforeRequest:                c.cfg.Defaults.BeforeRequest,
		AfterResponse:                c.cfg.Defaults.AfterResponse,
		Inspector:                    c.cfg.Defaults.Inspector,
		ResponseValidators:           c.cfg.Defaults.ResponseValidators,
		SoftErrorDetectors:           c.cfg.Defaults.toInternalSoftErrorDetectors(),
		RefererState:                 c.referer,
		MaxResponseSize:              c.cfg.Defaults.MaxResponseSize,
		MultiReadThreshold:           c.cfg.Defaults.MultiReadThreshold,
		MultiReadDisableDisk:         c.cfg.Defaults.MultiReadDisableDisk,
		RefererAutomaton:             c.cfg.Defaults.RefererAutomaton,
		DictionaryStore:              c.cfg.Defaults.DictionaryStore,
		DisableDictionaryCompression: c.cfg.Defaults.DisableDictionaryCompression,
	}
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
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
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
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
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

// ============================================================================
// Section 9: Redirect Policies & Transport Helpers
// ============================================================================

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
			if urlkit.MatchDomainPattern(host, domainPattern) {
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

		if urlkit.IsCrossOrigin(req.URL, via[0].URL) {
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

// ApplyMSSLimit applies maximum segment size boundaries to TCP socket streams.
func ApplyMSSLimit(conn net.Conn, mss int) net.Conn {
	return transport.ApplyMSSLimit(conn, mss)
}

var applyMSSLimit = ApplyMSSLimit

// ExtensionConfig provides standard hooks for third-party plugins (like aoni-browser)
// to modify low-level transport and serialization behaviors without altering the core engine.
type ExtensionConfig struct {
	// DialTLSContext allows fully overriding the TLS handshake process (e.g., injecting uTLS).
	DialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// WrapTLSClient allows wrapping an already-dialed TCP connection with a custom TLS handshake (e.g., uTLS).
	// If provided, the engine will dial the underlying connection (handling proxies) and then delegate the TLS handshake to this function.
	WrapTLSClient func(ctx context.Context, conn net.Conn, cfg *tls.Config, addr string) (net.Conn, error)

	// HeaderOrder defines strict HTTP/1 and HTTP/2 header serialization order.
	HeaderOrder []string

	// PseudoHeaderOrder defines strict HTTP/2 pseudo-header serialization order (e.g., :method, :authority, :path).
	PseudoHeaderOrder []string

	// OverrideH2Settings allows injecting custom HTTP/2 SETTINGS frames (e.g., MAX_CONCURRENT_STREAMS).
	OverrideH2Settings map[uint16]uint32

	// JA4Callback is invoked when a JA4 fingerprint is computed.
	JA4Callback func(report any)

	// Extra stores extension-specific arbitrary state (e.g., *profile.BrowserTLSConfig) keyed by extension domain.
	Extra map[string]any
}

// Clone creates a memory-isolated deep copy of the extension config.
func (e ExtensionConfig) Clone() ExtensionConfig {
	cloned := e
	cloned.HeaderOrder = slices.Clone(e.HeaderOrder)
	cloned.PseudoHeaderOrder = slices.Clone(e.PseudoHeaderOrder)
	cloned.OverrideH2Settings = maps.Clone(e.OverrideH2Settings)

	if e.Extra != nil {
		cloned.Extra = make(map[string]any, len(e.Extra))
		for k, v := range e.Extra {
			if cloner, ok := v.(interface{ Clone() any }); ok {
				cloned.Extra[k] = cloner.Clone()
			} else {
				cloned.Extra[k] = v
			}
		}
	}

	return cloned
}

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

	// AlpnH2 specifies the ALPN token for negotiating HTTP/2 over TLS 1.2+ (RFC 9113 §3.1 & §3.2).
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

// RequiresRequestContext reports whether any subsystem configuration requires attaching RequestConfig to request contexts.
func (c Config) RequiresRequestContext() bool {
	return c.Network.RequiresRequestContext() ||
		c.Fingerprint.RequiresRequestContext() ||
		c.Defaults.RequiresRequestContext()
}

// IsBaremetalEligible reports whether the entire client configuration permits fast 0-alloc baremetal execution.
func (c Config) IsBaremetalEligible() bool {
	return !c.RequiresRequestContext() &&
		c.Fingerprint.IsBaremetalEligible() &&
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
	CookieJar http.CookieJar

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
}

// DigestAuthConfig holds RFC 7616 HTTP Digest Access Authentication credentials.
type DigestAuthConfig struct {
	Username string
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
	SourceRotator *fip.SourceIPRotator

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
	// Default: 300ms.
	HappyEyeballsDelay time.Duration

	// HedgingDelay sets the fixed fallback duration before launching a secondary speculative request.
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
	SSRFGuard bool

	// TCPQuickACK enables the TCP_QUICKACK socket option on Linux, disabling delayed ACKs for lower latency.
	TCPQuickACK bool

	// EnablePowerManagement attaches an OS power lifecycle watcher that purges stale keep-alive connections
	// upon laptop sleep/wake transitions, preventing silent 15-second write timeouts on dead sockets.
	EnablePowerManagement bool

	// ExperimentalFlags consolidates opt-in hardware and OS experimental accelerations (io_uring, SIMD, RIO, TCP Fast Open).
	ExperimentalFlags ExperimentalFlag

	// CPUAffinityCores locks network worker OS threads to designated CPU core indices to eliminate thread migration overhead.
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

// RequiresRequestContext reports whether network configurations require attaching RequestConfig to request contexts.
func (n NetworkConfig) RequiresRequestContext() bool {
	return n.SocketController != nil || n.SSRFGuard || n.ProxyAddr != nil
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

// ClientHintsMap maps W3C Client Hints header keys (e.g. "Sec-CH-UA-Platform") to string values.
type ClientHintsMap map[string]string

// Clone creates a memory-isolated copy of the client hints map.
func (c ClientHintsMap) Clone() ClientHintsMap {
	if c == nil {
		return nil
	}

	cloned := make(ClientHintsMap, len(c))
	maps.Copy(cloned, c)

	return cloned
}

// BrowserProfile holds user-agent strings and synchronized Client Hints headers
// for realistic browser persona emulation and profile rotation.
type BrowserProfile struct {
	// UserAgent is the exact browser User-Agent header string.
	UserAgent string

	// ClientHints maps W3C Client Hints header keys (e.g. "Sec-CH-UA", "Sec-CH-UA-Platform", "Sec-CH-UA-Mobile")
	// to their corresponding browser version strings to match the declared User-Agent.
	ClientHints ClientHintsMap
}

// Clone creates a deep copy of BrowserProfile and its ClientHints map.
func (b BrowserProfile) Clone() BrowserProfile {
	return BrowserProfile{
		UserAgent:   b.UserAgent,
		ClientHints: b.ClientHints.Clone(),
	}
}

// CertificatePinMap maps domain patterns (e.g. "*.example.com") to expected SHA-256 SPKI fingerprint hashes.
type CertificatePinMap map[string][]string

// Clone creates a memory-isolated deep copy of the certificate pin map.
func (c CertificatePinMap) Clone() CertificatePinMap {
	if c == nil {
		return nil
	}

	cloned := make(CertificatePinMap, len(c))
	for k, v := range c {
		cloned[k] = slices.Clone(v)
	}

	return cloned
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

// FingerprintConfig controls TLS ClientHello emulation, HTTP/2 SETTINGS frames,
// header order serialization, p0f OS stack spoofing, and ECH/0-RTT features.
//
// # TLS Emulation Precedence
//
// When multiple TLS fingerprint options are configured, the transport layer evaluates them
// in the following strict hierarchical order:
//  1. TLSClientHelloSpecProvider (explicit dynamic uTLS ClientHelloSpec builder)
//  2. TLSClientHelloID (specific uTLS ClientHelloID preset, e.g. HelloChrome_120)
//  3. BrowserID (predefined high-level browser profile)
type FingerprintConfig struct {
	// BrowserID specifies a predefined browser fingerprint profile (Chrome, Firefox, Safari).
	BrowserID BrowserID

	// TLSClientHelloID overrides BrowserID with a specific uTLS ClientHelloID preset.
	TLSClientHelloID *utls.ClientHelloID

	// TLSClientHelloSpecProvider dynamically generates a uTLS ClientHelloSpec for each connection,
	// allowing fine-grained control over TLS extensions, cipher suites, supported curves, and ALPN tokens.
	TLSClientHelloSpecProvider fingerprint.ClientHelloSpecProvider

	// TLSQUICClientHelloSpec configures TLS cipher suites and transport parameters for HTTP/3 QUIC handshakes.
	TLSQUICClientHelloSpec *utls.ClientHelloSpec

	// HeaderOrder specifies the exact HTTP/1.1 or HTTP/2 header key serialization sequence.
	//
	// L7 Fingerprint Evasion:
	// Modern WAFs calculate JA4H and header hashes based on header ordering (e.g. :method, :authority, :scheme, :path).
	// HeaderOrder ensures outgoing headers strictly match genuine browser serialization order.
	HeaderOrder []string

	// H2Settings overrides default HTTP/2 SETTINGS and PRIORITY frame parameters
	// (HEADER_TABLE_SIZE, INITIAL_WINDOW_SIZE, MAX_FRAME_SIZE, MAX_CONCURRENT_STREAMS) to mirror target browsers.
	H2Settings *h2.Settings

	// H3Settings overrides default QUIC/HTTP/3 flow control receive window limits and QPACK settings.
	H3Settings *h3.Settings

	// P0fSignature spoofs L3/L4 TCP/IP stack parameters (TTL, Window Size, MSS, SYN packet options)
	// to defeat passive OS fingerprinting systems (p0f / SYN packet analyzers).
	P0fSignature *p0f.Signature

	// PacketPadding injects randomized HTTP header padding to disguise exact payload byte lengths against DPI analysis.
	PacketPadding *fingerprint.PaddingConfig

	// CertificatePins maps domain patterns to expected SHA-256 Subject Public Key Info (SPKI) hashes (RFC 7469).
	// Connections to pinned domains are terminated immediately if the server's certificate SPKI does not match.
	CertificatePins CertificatePinMap

	// CertCompression specifies RFC 8879 certificate compression algorithms (Brotli, Zstd, Zlib) for TLS handshakes.
	CertCompression []cert.CompressionAlgorithm

	// ECHConfigList contains raw RFC 9484 Encrypted Client Hello configuration bytes.
	ECHConfigList []byte

	// SessionCache manages proxy-isolated TLS session ticket resumption across reconnects.
	SessionCache fingerprint.SessionCache

	// JA4Callback is a post-handshake hook invoked with calculated JA4/JA4H fingerprint reports.
	JA4Callback func(ja4.Report)

	// H2Configurer allows direct customization of low-level x/net/http2 transport configurations.
	H2Configurer fingerprint.HTTP2Configurer

	// AutoECH automatically resolves Encrypted Client Hello (ECH) keys via DNS HTTPS (Type 65 / RFC 9460) records.
	AutoECH bool

	// Enable0RTT enables TLS 1.3 / QUIC Early Data session resumption (RFC 8446 / RFC 9001 / RFC 9846).
	//
	// CAUTION: Replay Attack Vulnerability:
	// 0-RTT data is susceptible to network replay attacks. Use primarily for idempotent GET/HEAD requests.
	Enable0RTT bool
}

// Clone creates a deep copy of FingerprintConfig and its nested maps, slices, and pointers.
func (f FingerprintConfig) Clone() FingerprintConfig {
	cloned := f
	cloned.TLSClientHelloID = clonePtr(f.TLSClientHelloID)
	cloned.H2Settings = clonePtr(f.H2Settings)
	cloned.H3Settings = clonePtr(f.H3Settings)
	cloned.PacketPadding = clonePtr(f.PacketPadding)
	cloned.CertificatePins = f.CertificatePins.Clone()

	if len(f.HeaderOrder) > 0 {
		cloned.HeaderOrder = slices.Clone(f.HeaderOrder)
	}

	if len(f.CertCompression) > 0 {
		cloned.CertCompression = slices.Clone(f.CertCompression)
	}

	if len(f.ECHConfigList) > 0 {
		cloned.ECHConfigList = slices.Clone(f.ECHConfigList)
	}

	return cloned
}

// ToPipelineFingerprint extracts pipeline-specific fingerprint parameters.
func (f FingerprintConfig) ToPipelineFingerprint() pipeline.ClientFingerprint {
	return pipeline.ClientFingerprint{
		PacketPadding: f.PacketPadding,
	}
}

// RequiresRequestContext reports whether fingerprint settings require attaching RequestConfig to request contexts.
func (f FingerprintConfig) RequiresRequestContext() bool {
	return f.TLSClientHelloSpecProvider != nil ||
		len(f.CertificatePins) > 0 ||
		f.P0fSignature != nil ||
		f.JA4Callback != nil
}

// IsBaremetalEligible reports whether fingerprint settings permit bypassing the pipeline.
func (f FingerprintConfig) IsBaremetalEligible() bool {
	return !f.RequiresRequestContext() && f.PacketPadding == nil
}

// ClientDefaults configures default headers, interceptor hooks, resource limits, decoders, and pipeline policies.
type ClientDefaults struct {
	// BaseURL is the default root endpoint used to resolve relative request paths (RFC 3986).
	// Relative subpaths (e.g. "/users/1") are resolved against BaseURL with zero allocations.
	BaseURL *url.URL

	// Headers are default HTTP headers sent with every outgoing request unless explicitly overridden.
	Headers http.Header

	// MaxResponseSize caps response body reads in bytes to prevent Out-Of-Memory (OOM) crashes.
	// Set to <= 0 for unlimited. Default: 10MB (10 * 1024 * 1024 bytes).
	MaxResponseSize int64

	// MultiReadThreshold sets the RAM buffering boundary (in bytes) before spilling over to temporary disk files.
	// Enables replayable/rewindable stream reading ([io.Seeker]) without unbounded heap growth.
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
	ResponseValidator func(*http.Response) error

	// SoftErrorDetectors inspects initial body bytes non-destructively for application-level soft errors
	// (e.g. HTTP 200 OK responses containing HTML login pages or JSON business error payloads).
	SoftErrorDetectors []SoftErrorDetector

	// OnPanic handles panics occurring inside request execution pipelines or middleware chains.
	OnPanic func(ctx context.Context, err any, stack []byte)

	// BaseResponse provides an envelope factory function used for structured API response unwrapping.
	BaseResponse func() BaseResponse

	// ChallengeSolver delegates Anti-DDoS and WAF challenge solving (e.g. Cloudflare Turnstile) to external drivers.
	ChallengeSolver challenge.Solver

	// ChallengeDetector determines whether an HTTP response represents a WAF/DDoS challenge page.
	ChallengeDetector challenge.Detector

	// Inspector captures and records request traces for real-time diagnostic telemetry inspection.
	Inspector telemetry.TrafficInspector

	// HeadersCookieJar provides a fallback cookie jar implementation.
	HeadersCookieJar http.CookieJar

	// QueryEncoder marshals structs or maps into URL query parameters.
	QueryEncoder QueryEncoder

	// Decoders maps MIME content types (e.g. "application/json", "application/protobuf") to response body decoders.
	Decoders DecoderMap

	// Logger receives structured diagnostic log events.
	Logger core.Logger

	// DefaultMods holds default functional request modifiers applied to every outgoing request.
	DefaultMods []RequestModifier

	// UARotationProfiles holds user agents and Client Hints for automatic browser persona rotation.
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

	cloned.Decoders = d.Decoders.Clone()

	if len(d.UARotationProfiles) > 0 {
		cloned.UARotationProfiles = make([]BrowserProfile, len(d.UARotationProfiles))
		for i, p := range d.UARotationProfiles {
			cloned.UARotationProfiles[i] = p.Clone()
		}
	}

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
		len(d.UARotationProfiles) == 0 &&
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

// PipelineConfig coordinates the behavior, resilience policies, and evasion capabilities
// of the 5-stage transaction execution pipeline.
//
// # Architectural Pipeline Stages
//
// Every HTTP transaction executed by an aoni client passes through five deterministic stages:
//  1. Stage 1 (Preparation & Modifiers): Encodes bodies, injects headers, binds context metadata.
//  2. Stage 2 (Middleware & Telemetry): Enforces circuit breaking, retries, hedging, and HAR logging.
//  3. Stage 3 (Protocol Engine & Janitors): Dispatches to standard or Fast engine, manages Alt-Svc cache.
//  4. Stage 4 (L4/L7 Transport): uTLS evasion, Happy Eyeballs v3 racing, proxy failover, socket tuning.
//  5. Stage 5 (Decoders & Resilience): Decompression, soft-error sniffing, structured unmarshaling.
//
// PipelineConfig governs which stages are active and configures their memory bounds and thresholds.
type PipelineConfig struct {
	// DPIJitter configures randomized microsecond-scale write delay jitter between TCP segments.
	//
	// Deep Packet Inspection (DPI) evasion:
	// Stateful firewalls and ISP middleboxes classify automated traffic not only by TLS ClientHello
	// signatures, but also by inter-packet arrival times (IAT) and TCP packet sizing.
	// DPIJitter disrupts ML-based timing analysis by injecting controlled entropy into socket writes.
	// If nil, socket writes proceed at full silicon line speed with 0 delay.
	DPIJitter *DPIJitterConfig

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
	// If nil, speculative hedging is disabled.
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
	// Set to <= 0 for unlimited body streaming. Default: 10MB (10 * 1024 * 1024 bytes).
	SizeLimit int64

	// MultiReadThreshold defines the RAM buffering capacity (in bytes) for rewindable response streams.
	//
	// Tiered RAM-to-Disk Spilling:
	// Responses smaller than MultiReadThreshold are buffered completely in pooled memory ([sync.Pool]).
	// Payloads exceeding this threshold transparently spill over to temporary disk files, enabling unlimited
	// stream rewindability ([io.Seeker]) without exhausting server memory.
	// Set to 0 to disable memory caching and force direct streaming.
	MultiReadThreshold int64

	// RotateUA enables automatic User-Agent and Client Hints rotation across sequential transactions.
	//
	// Anti-Clustering Evasion:
	// Prevents edge WAFs from clustering requests from the same client session by rotating browser
	// personas while maintaining synchronized TLS ClientHello profiles.
	RotateUA bool

	// Inspect enables real-time transaction broadcasting to the embedded Web Inspector telemetry dashboard.
	// When true, all request/response pairs are mirrored over WebSockets to the diagnostic inspector UI.
	Inspect bool

	// Decompress enables transparent RFC 9110 response body decompression.
	//
	// Multi-Codec Acceleration:
	// Automatically negotiates and decompresses "gzip", "deflate", "br" (Brotli), and "zstd" (Zstandard)
	// payload streams using zero-allocation streaming decoders.
	Decompress bool

	// Validate enforces application-level status code and header integrity checks before body unmarshaling.
	// When true, responses with unexpected non-2xx status codes or invalid content types are rejected early.
	Validate bool

	// Challenge enables autonomous Anti-DDoS and WAF challenge detection and solving.
	//
	// Autonomous Bypass:
	// Detects HTTP 403/503 challenge pages (Cloudflare Turnstile, AWS WAF, Akamai), pauses the execution
	// pipeline, invokes the registered [challenge.Solver], and automatically retries the original request
	// with acquired clearance cookies and authorization headers.
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

// IsActive reports whether any pipeline stage or middleware interception is enabled.
func (p PipelineConfig) IsActive() bool {
	return p.Decompress || p.Validate || p.Challenge || p.HAR != nil || p.Cache != nil ||
		p.Hedging != nil || p.DPIJitter != nil || p.ProxyFailover != nil || p.Inspect || p.RotateUA
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
			cfg.Decoders = maps.Clone(c.cfg.Defaults.Decoders)
		} else {
			for k, v := range c.cfg.Defaults.Decoders {
				if _, ok := cfg.Decoders[k]; !ok {
					cfg.Decoders[k] = v
				}
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

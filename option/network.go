// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/foundation/net/ip"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ipc"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// WithNetwork sets the default L4 transport or IPC protocol for socket dialing.
//
// Common networks: [aoni.NetworkTCP], [aoni.NetworkUDP], [aoni.NetworkUnix].
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithNetwork(aoni.NetworkTCP),
//	)
func WithNetwork(network aoni.Network) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.Network = network
	}
}

// WithNetworkString sets the default L4 or IPC network protocol from a raw string.
//
// Supported values include "tcp", "tcp4", "tcp6", "udp", and "unix".
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithNetworkString("tcp4"),
//	)
func WithNetworkString(network string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.Network = aoni.Network(network)
	}
}

// WithInterface binds outgoing TCP/UDP sockets directly to a specific OS network interface.
//
// Useful for multi-homed servers, VPN egress binding, or forcing cellular/Wi-Fi routes.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithInterface("eth0"),
//	)
//
// # Invariants & OS Requirements
//
// Requires `SO_BINDTODEVICE` (Linux) or `IP_BOUND_IF` (macOS/BSD). On Linux, may require
// elevated capabilities (`CAP_NET_RAW`).
func WithInterface(iface string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.InterfaceName = iface
	}
}

// WithSocketMark assigns a Linux netfilter socket mark (SO_MARK) for policy-based routing.
//
// Packets emitted by this socket will carry the specified mark, enabling `iptables` / `nftables`
// and policy routing tables (`ip rule`) to select specific routing tables.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithSocketMark(0x100),
//	)
func WithSocketMark(mark uint32) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketMark = mark
	}
}

// WithCustomNetworkDriver attaches a custom L3/L4 network stack driver.
//
// Enables userspace network stacks (such as gVisor Netstack or DPDK) to intercept and manage
// all underlying packet flows.
func WithCustomNetworkDriver(driver netdial.RawStackDriver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.StackDriver = driver
	}
}

// WithL2Device attaches a custom Data Link Layer (Ethernet) device driver for raw frame injection.
func WithL2Device(device netdial.L2Device) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.L2Device = device
	}
}

// WithLocalAddr binds outgoing TCP connections to a single local IP address.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithLocalAddr("192.168.1.50"),
//	)
func WithLocalAddr(addr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator([]string{addr})
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithLocalAddrPool registers a pool of local IP addresses to cycle through across outgoing requests.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithLocalAddrPool([]string{"10.0.0.2", "10.0.0.3", "10.0.0.4"}),
//	)
func WithLocalAddrPool(addrs []string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator(addrs)
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithProxy configures an upstream proxy server URL.
//
// Supported schemes: "http://", "https://", "socks5://", "socks5h://".
//
// # Example
//
//	proxyURL, _ := url.Parse("socks5://127.0.0.1:9050")
//	client := aoni.NewClient(nil,
//	    option.WithProxy(proxyURL),
//	)
func WithProxy(proxyURL *url.URL) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyAddr = proxyURL
		if proxyURL != nil {
			cfg.Network.TransportProxy = http.ProxyURL(proxyURL)
		}
	}
}

// WithProxyString parses a proxy URL and sets it as the default upstream proxy.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithProxyString("socks5h://user:pass@127.0.0.1:1080"),
//	)
//
// # Invariants
//
// If proxyStr is malformed, the option is safely ignored without panicking.
func WithProxyString(proxyStr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		u, err := proxy.Parse(proxyStr)
		if err != nil {
			cfg.Network.ProxyAddr = nil
			cfg.Network.TransportProxy = nil
			return
		}

		cfg.Network.ProxyAddr = u
		cfg.Network.TransportProxy = http.ProxyURL(u)
	}
}

// WithProxyDNS routes DNS hostname lookups through the configured SOCKS5 or HTTP CONNECT proxy
// instead of resolving locally, eliminating DNS leaks.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithProxyString("socks5://127.0.0.1:1080"),
//	    option.WithProxyDNS(),
//	)
func WithProxyDNS() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyDNS = true
	}
}

// WithAdaptiveProxyTimeout enables dynamic proxy connection timeout calculation
// based on observed network round-trip time (RTT) metrics.
//
// Automatically adjusts handshake deadlines according to moving network EWMA percentiles.
func WithAdaptiveProxyTimeout(cfg ...proxy.AdaptiveTimeoutConfig) aoni.ClientOption {
	activeCfg := proxy.DefaultAdaptiveTimeoutConfig()
	if len(cfg) > 0 {
		activeCfg = cfg[0]
	}

	return func(c *aoni.Config) {
		if c.Network.DynamicHedging == nil {
			dhc := telemetry.DefaultDynamicHedgingConfig()
			c.Network.DynamicHedging = &dhc
		}

		tracker := c.Network.DynamicHedging.Tracker
		c.Defaults.DefaultMods = append(c.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			adaptiveTimeout := proxy.ComputeProxyTimeout(tracker, activeCfg)
			pipeline.GetOrInitRequestConfig(req).TimeoutOverride = adaptiveTimeout
		}))
	}
}

// WithDNSResolver replaces the default system DNS resolver with a custom [netutil.DNSResolver].
//
// Allows integration with DoH (DNS over HTTPS), DoT (DNS over TLS), or DoQ (DNS over QUIC).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithDNSResolver(dohResolver),
//	)
func WithDNSResolver(resolver netutil.DNSResolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.DNSResolver = resolver
	}
}

// WithHostRewrite configures static hostname-to-IP remapping rules, overriding DNS resolution.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithHostRewrite(map[string]string{
//	        "api.internal.com": "10.0.4.15",
//	    }),
//	)
func WithHostRewrite(rules map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HostRewrite = &netutil.HostRewriteConfig{Rules: rules}
	}
}

// WithHappyEyeballs configures the dual-stack IPv4/IPv6 connection racing delay (RFC 8305).
//
// Defaults to 300ms if not explicitly overridden.
//
// # RFC Compliance
//
// Conforms to RFC 8305 (Happy Eyeballs Version 2: Better Connectivity Using Concurrency).
func WithHappyEyeballs(delay time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HappyEyeballsDelay = delay
	}
}

// WithSSRFGuard enables anti-SSRF protections, preventing requests to private, loopback,
// carrier-grade NAT, or cloud metadata IP addresses (e.g. 169.254.169.254).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithSSRFGuard(),
//	)
func WithSSRFGuard() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SSRFGuard = true
	}
}

// WithTCPDelay sets default pre-dial TCP delay jitter bounds to randomize connection timing against traffic analysis.
func WithTCPDelay(min, max time.Duration) aoni.ClientOption {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithTCPDelay(minDelay, maxDelay))
	}
}

// WithFragmentation configures TCP packet fragmentation parameters for DPI evasion.
func WithFragmentation(frag fragment.Config) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.FragmentConfig = &frag
	}
}

// WithSocketController registers a custom socket control hook invoked prior to connect/bind.
func WithSocketController(controller netutil.SocketController) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketController = controller
	}
}

// WithTimeout sets the overall end-to-end request deadline (including dial, TLS handshake, and body read).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithTimeout(10 * time.Second),
//	)
func WithTimeout(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.Timeout = d
	}
}

// WithRedirectLimit sets the maximum number of consecutive HTTP redirects followed before failing.
//
// Set to 0 to disable automatic redirect following completely.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithRedirectLimit(5),
//	)
func WithRedirectLimit(max int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.RedirectLimit = max
	}
}

// WithAllowedRedirectDomains restricts HTTP redirect navigation exclusively to trusted domain names.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithAllowedRedirectDomains("auth.example.com", "app.example.com"),
//	)
func WithAllowedRedirectDomains(domains ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CheckRedirect = aoni.AllowedDomainsRedirectPolicy(domains...)
	}
}

// WithBlockRedirectTo halts redirect chains immediately if the target URL path matches any of patterns.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithBlockRedirectTo("/login", "/challenge"),
//	)
func WithBlockRedirectTo(patterns ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CheckRedirect = aoni.BlockPathRedirectPolicy(patterns...)
	}
}

// WithConnectionPool configures idle connection pool limits, max idle conns per host, and idle timeouts.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithConnectionPool(aoni.ConnectionPoolConfig{
//	        MaxIdleConns:        1000,
//	        MaxIdleConnsPerHost: 100,
//	        IdleConnTimeout:     90 * time.Second,
//	    }),
//	)
func WithConnectionPool(pool aoni.ConnectionPoolConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.ConnectionPool = &pool
	}
}

// WithInsecureSkipVerify disables remote TLS certificate verification globally on the transport.
//
// > [!WARNING]
// > Use only in testing or internal development environments. Bypasses MITM attack defenses.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithInsecureSkipVerify(),
//	)
func WithInsecureSkipVerify() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.InsecureSkipVerify = true
	}
}

// WithUnixSocket binds the client transport directly to a local Unix domain socket.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithUnixSocket("/var/run/docker.sock"),
//	)
func WithUnixSocket(socketPath string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = &http.Client{
			Transport: ipc.NewUnixTransport(socketPath),
		}
	}
}

// WithNamedPipe binds the client transport to a Windows Named Pipe.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithNamedPipe(`\\.\pipe\docker_engine`),
//	)
func WithNamedPipe(pipePath string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = &http.Client{
			Transport: ipc.NewNamedPipeTransport(pipePath),
		}
	}
}

// WithCoreAffinity locks client worker and network polling threads to specific physical CPU cores.
func WithCoreAffinity(cores ...int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.CPUAffinityCores = cores
	}
}

// WithPACEngine attaches a dynamic Proxy Auto-Configuration (PAC) routing engine.
func WithPACEngine(engine *proxy.PACEngine) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if engine != nil {
			cfg.Network.TransportProxy = engine.ProxyFunc()
		}
	}
}

// WithPACRules creates and attaches a [proxy.PACEngine] initialized with declarative routing rules.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithPACRules("DIRECT",
//	        proxy.NewDomainPACRule("*.internal.net", "PROXY proxy.corp:8080"),
//	    ),
//	)
func WithPACRules(defaultRoute string, rules ...proxy.PACRule) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		engine := proxy.NewPACEngine(defaultRoute)
		for _, r := range rules {
			engine.AddRule(r)
		}

		cfg.Network.TransportProxy = engine.ProxyFunc()
	}
}

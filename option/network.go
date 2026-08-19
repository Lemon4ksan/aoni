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
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ipc"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// WithInterface binds outgoing TCP sockets directly to a specific network interface (e.g. "eth0", "wg0").
func WithInterface(iface string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.InterfaceName = iface
	}
}

// WithSocketMark assigns a Linux netfilter socket mark (SO_MARK) for policy-based routing.
func WithSocketMark(mark uint32) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketMark = mark
	}
}

// WithCustomNetworkDriver returns an [aoni.ClientOption] attaching a custom L3/L4 network stack driver.
func WithCustomNetworkDriver(driver netdial.RawStackDriver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.StackDriver = driver
	}
}

// WithL2Device returns an [aoni.ClientOption] attaching a custom Data Link Layer (Ethernet) L2Device driver.
func WithL2Device(device netdial.L2Device) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.L2Device = device
	}
}

// WithLocalAddr returns an [aoni.ClientOption] binding outgoing TCP connections to a single local IP address.
func WithLocalAddr(addr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator([]string{addr})
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithLocalAddrPool returns an [aoni.ClientOption] registering a pool of local IP addresses to cycle through.
func WithLocalAddrPool(addrs []string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator(addrs)
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithProxy returns an [aoni.ClientOption] configuring a proxy server URL.
func WithProxy(proxyURL *url.URL) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyAddr = proxyURL
		if proxyURL != nil {
			cfg.Network.TransportProxy = http.ProxyURL(proxyURL)
		}
	}
}

// WithProxyString returns an [aoni.ClientOption] parsing and setting a proxy URL string.
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

// WithProxyDNS returns an [aoni.ClientOption] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyDNS = true
	}
}

// WithAdaptiveProxyTimeout enables dynamic proxy connection timeout calculation
// based on observed network round-trip time (RTT) metrics.
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
			aoni.GetOrInitRequestConfig(req).TimeoutOverride = adaptiveTimeout
		}))
	}
}

// WithDNSResolver returns an [aoni.ClientOption] replacing the default system DNS resolver with a [netutil.DNSResolver].
func WithDNSResolver(resolver netutil.DNSResolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.DNSResolver = resolver
	}
}

// WithHostRewrite returns an [aoni.ClientOption] configuring DNS hostname-to-IP remapping rules.
func WithHostRewrite(rules map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HostRewrite = &netutil.HostRewriteConfig{Rules: rules}
	}
}

// WithHappyEyeballs returns an [aoni.ClientOption] configuring IPv4/IPv6 stagger delay.
func WithHappyEyeballs(delay time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HappyEyeballsDelay = delay
	}
}

// WithSSRFGuard returns an [aoni.ClientOption] enabling SSRF safeguards against private and loopback IP addresses.
func WithSSRFGuard() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SSRFGuard = true
	}
}

// WithTCPDelay returns an [aoni.ClientOption] setting default pre-dial TCP delay jitter bounds.
func WithTCPDelay(min, max time.Duration) aoni.ClientOption {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithTCPDelay(minDelay, maxDelay))
	}
}

// WithFragmentation returns an [aoni.ClientOption] configuring TCP packet fragmentation parameters.
func WithFragmentation(frag fragment.Config) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.FragmentConfig = &frag
	}
}

// WithSocketController returns an [aoni.ClientOption] registering an [aoni.SocketController] socket control hook.
func WithSocketController(controller netutil.SocketController) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketController = controller
	}
}

// WithTimeout returns an [aoni.ClientOption] setting the end-to-end request transaction deadline duration.
func WithTimeout(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.Timeout = d
	}
}

// WithRedirectLimit returns an [aoni.ClientOption] setting the maximum number of HTTP redirects followed.
func WithRedirectLimit(max int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.RedirectLimit = max
	}
}

// WithAllowedRedirectDomains returns an [aoni.ClientOption] restricting HTTP redirects to trusted domain patterns.
func WithAllowedRedirectDomains(domains ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CheckRedirect = aoni.AllowedDomainsRedirectPolicy(domains...)
	}
}

// WithConnectionPool returns an [aoni.ClientOption] configuring keep-alive connection boundaries on the transport.
func WithConnectionPool(pool aoni.ConnectionPoolConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.ConnectionPool = &pool
	}
}

// WithInsecureSkipVerify returns an [aoni.ClientOption] bypassing TLS certificate verification globally on the transport.
func WithInsecureSkipVerify() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.InsecureSkipVerify = true
	}
}

// WithUnixSocket binds the client transport directly to a local Unix domain socket (e.g., "/var/run/docker.sock").
func WithUnixSocket(socketPath string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = &http.Client{
			Transport: ipc.NewUnixTransport(socketPath),
		}
	}
}

// WithNamedPipe binds the client transport to a Windows Named Pipe (e.g., "\\.\pipe\docker_engine").
func WithNamedPipe(pipePath string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = &http.Client{
			Transport: ipc.NewNamedPipeTransport(pipePath),
		}
	}
}

// WithCoreAffinity returns an [aoni.ClientOption] locking calling threads to target physical CPU cores.
func WithCoreAffinity(cores ...int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.CPUAffinityCores = cores
	}
}

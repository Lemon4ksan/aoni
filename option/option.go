// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package option provides functional options for customizing an [aoni.Client] configuration.
//
// Options are consumed by [aoni.NewClient] or [aoni.Client.With] to configure global client defaults,
// such as base URLs, timeouts, proxy rotators, TLS fingerprints, and execution pipeline behavior.
//
// Thread Safety:
// All options operate immutably on [aoni.Config] structures, preserving thread safety and concurrent client reuse.
package option

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/netutil/ipc"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// ============================================================================
// 1. FULL CONFIGURATION BLOCK OVERRIDES
// ============================================================================

// WithConfig returns an [aoni.ClientOption] that replaces the entire client configuration at once.
func WithConfig(cfg aoni.Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Network = cfg.Network.Clone()
		c.Fingerprint = cfg.Fingerprint.Clone()
		c.Defaults = cfg.Defaults.Clone()
		c.Engine = cfg.Engine
	}
}

// WithDefaultsBlock returns an [aoni.ClientOption] replacing only the [aoni.ClientDefaults] configuration layer.
func WithDefaultsBlock(defaults aoni.ClientDefaults) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults = defaults.Clone()
	}
}

// WithNetworkBlock returns an [aoni.ClientOption] replacing only the [aoni.NetworkConfig] configuration layer.
func WithNetworkBlock(network aoni.NetworkConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network = network.Clone()
	}
}

// WithFingerprintBlock returns an [aoni.ClientOption] replacing only the [aoni.FingerprintConfig] configuration layer.
func WithFingerprintBlock(fingerprint aoni.FingerprintConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint = fingerprint.Clone()
	}
}

// WithBaremetal switches the client to maximum-speed ("bare-metal") mode:
// it disables background rotation, HTML tag validation, copying of default headers, and unnecessary wrappers.
func WithBaremetal() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline.Decompress = false
		cfg.Defaults.Pipeline.Validate = false
		cfg.Defaults.Pipeline.Challenge = false
		cfg.Defaults.MaxResponseSize = -1
		cfg.Defaults.MultiReadThreshold = -1
		cfg.Defaults.RefererAutomaton = false
		cfg.Defaults.Headers = nil
	}
}

// ============================================================================
// 2. ENGINE, BASE URL & TRANSPORT OPTIONS
// ============================================================================

// WithEngine returns an [aoni.ClientOption] replacing the underlying [aoni.HTTPDoer] engine (e.g. custom [*http.Client]).
func WithEngine(engine aoni.HTTPDoer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithProtocol registers a custom [http.RoundTripper] handler for non-HTTP schemes (e.g., "file", "ftp", "s3").
func WithProtocol(scheme string, handler http.RoundTripper) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Engine.Protocols == nil {
			cfg.Engine.Protocols = make(map[string]http.RoundTripper)
		}

		normScheme := strings.ToLower(strings.TrimSpace(scheme))
		if normScheme != "" && handler != nil {
			cfg.Engine.Protocols[normScheme] = handler
		}
	}
}

// WithBaseURL returns an [aoni.ClientOption] setting the default base URL for resolving relative request paths.
func WithBaseURL(raw string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			return
		}

		formatted := raw
		if !strings.HasSuffix(formatted, "/") {
			formatted += "/"
		}

		baseURL, err := url.Parse(formatted)
		if err != nil {
			return
		}

		cfg.Defaults.BaseURL = baseURL
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

// WithHTTP2Config returns an [aoni.ClientOption] configuring low-level HTTP/2 connection parameters.
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithHTTP2Configurer returns an [aoni.ClientOption] configuring an [aoni.HTTP2Configurer] interface on the transport.
func WithHTTP2Configurer(configurer aoni.HTTP2Configurer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

// WithH2ServerPush configures whether the HTTP/2 client accepts server-pushed resources (RFC 9113 §8.4),
// storing them directly in the response cache to avoid duplicate asset fetches.
func WithH2ServerPush(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.H2Settings == nil {
			cfg.Fingerprint.H2Settings = &h2.ChromeSettings
		}

		if enable {
			cfg.Fingerprint.H2Settings.EnablePush = 1
		} else {
			cfg.Fingerprint.H2Settings.EnablePush = 0
		}
	}
}

// WithInsecureSkipVerify returns an [aoni.ClientOption] bypassing TLS certificate verification globally on the transport.
//
// Warning:
// Enabling this exposes outgoing connections to man-in-the-middle attacks.
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

// ============================================================================
// 3. HEADER, USER-AGENT & AUTH OPTIONS
// ============================================================================

// WithHeader returns an [aoni.ClientOption] adding a default header key-value pair sent with every request.
func WithHeader(key, value string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(key, value)
	}
}

// WithHeaderFunc returns an [aoni.ClientOption] setting a dynamic header evaluated via provider on every request.
func WithHeaderFunc(key string, provider func() string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if key == "" || provider == nil {
			return
		}

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithHeaderFunc(key, provider))
	}
}

// WithHeaders returns an [aoni.ClientOption] merging a map of default headers into the client configuration.
func WithHeaders(headers map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header, len(headers))
		}

		for k, v := range headers {
			cfg.Defaults.Headers.Set(k, v)
		}
	}
}

// WithoutHeaders returns an [aoni.ClientOption] purging all default request headers.
func WithoutHeaders() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers = make(http.Header)
	}
}

// WithUserAgent returns an [aoni.ClientOption] overriding the default User-Agent header field.
func WithUserAgent(ua string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithUARotationProfiles returns an [aoni.ClientOption] configuring browser profiles for automatic User-Agent rotation.
func WithUARotationProfiles(profiles []aoni.BrowserProfile) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.UARotationProfiles = profiles
	}
}

// WithOrigin returns an [aoni.ClientOption] setting a default Origin header.
func WithOrigin(origin string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Origin", origin)
	}
}

// WithBearer returns an [aoni.ClientOption] setting a default "Authorization: Bearer <token>" header.
func WithBearer(token string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth returns an [aoni.ClientOption] setting default HTTP Basic Authentication credentials.
func WithBasicAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		auth := username + ":" + password
		cfg.Defaults.Headers.Set(
			"Authorization",
			"Basic "+base64.StdEncoding.EncodeToString([]byte(auth)),
		)
	}
}

// WithRefererAutomaton returns an [aoni.ClientOption] toggling automatic Referer header tracking across requests.
func WithRefererAutomaton(enabled bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

// ============================================================================
// 4. NETWORK, PROXY & DNS OPTIONS
// ============================================================================

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
		c.Defaults.DefaultMods = append(c.Defaults.DefaultMods, func(req aoni.Request) {
			adaptiveTimeout := proxy.ComputeProxyTimeout(tracker, activeCfg)
			aoni.GetOrInitRequestConfig(req).TimeoutOverride = adaptiveTimeout
		})
	}
}

// WithDNSResolver returns an [aoni.ClientOption] replacing the default system DNS resolver with an [aoni.DNSResolver].
func WithDNSResolver(resolver aoni.DNSResolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.DNSResolver = resolver
	}
}

// WithHostRewrite returns an [aoni.ClientOption] configuring DNS hostname-to-IP remapping rules.
func WithHostRewrite(rules map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HostRewrite = &aoni.HostRewriteConfig{Rules: rules}
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
func WithSocketController(controller aoni.SocketController) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketController = controller
	}
}

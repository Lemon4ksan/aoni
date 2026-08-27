// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"maps"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

// WithNetwork overrides the default L4 network protocol for this specific request (e.g. "tcp4", "tcp6", "unix").
func WithNetwork(network any) RequestModifier {
	return Custom(func(req Request) {
		if s, ok := network.(string); ok {
			getOrInitRequestConfig(req).Network = s
		} else if stringer, ok := network.(interface{ String() string }); ok {
			getOrInitRequestConfig(req).Network = stringer.String()
		}
	})
}

// WithNetworkString overrides the L4 network protocol for this request from a raw string.
func WithNetworkString(network string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Network = network
	})
}

// WithOrderedHeaders enforces an exact HTTP/1.1 wire serialization order for request headers.
//
// Crucial for browser impersonation (matching Chrome or Firefox header ordering to bypass WAFs).
func WithOrderedHeaders(headers []string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).OrderedHeaders = headers
	})
}

// WithALPN overrides TLS ALPN (Application-Layer Protocol Negotiation) protocols for this connection.
func WithALPN(protos ...string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = protos
	})
}

// WithoutAltSvc disables Alt-Svc connection upgrades, forcing direct socket dialing to the origin.
func WithoutAltSvc() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DisableAltSvc = true
	})
}

// WithForceHTTP1 restricts ALPN negotiation strictly to HTTP/1.1 ("http/1.1").
func WithForceHTTP1() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"http/1.1"}
	})
}

// WithForceHTTP2 restricts ALPN negotiation strictly to HTTP/2 ("h2").
func WithForceHTTP2() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"h2"}
	})
}

// WithForceHTTP3 restricts ALPN negotiation strictly to HTTP/3 ("h3").
func WithForceHTTP3() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"h3"}
	})
}

// Without0RTT disables TLS 1.3 / QUIC 0-RTT Early Data for this request, forcing standard 1-RTT.
func Without0RTT() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Disable0RTT = true
	})
}

// WithTCPDelay adds randomized pre-dial delay jitter bounds to confuse timing-based traffic analysis.
func WithTCPDelay(min, max time.Duration) RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return Custom(func(req Request) {
		getOrInitRequestConfig(req).TCPDelay = netutil.TCPDelayRange{Min: minDelay, Max: maxDelay}
	})
}

// WithHappyEyeballs configures IPv4/IPv6 dual-stack stagger delay for this request (RFC 8305).
func WithHappyEyeballs(delay time.Duration) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).HappyEyeballsDelay = delay
	})
}

// WithProxyDNS forces remote DNS resolution through the upstream proxy, eliminating local DNS leaks.
func WithProxyDNS() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ProxyDNS = true
	})
}

// WithProxyOverride routes this specific request through a designated proxy URL.
//
// # Example
//
//	resp, err := client.Get(ctx, "/geo-target",
//	    mod.WithProxyOverride("socks5://192.168.1.100:1080"),
//	)
func WithProxyOverride(rawURL string) RequestModifier {
	return Custom(func(req Request) {
		if u, err := url.Parse(rawURL); err == nil {
			getOrInitRequestConfig(req).ProxyAddr = u
		}
	})
}

// WithSSRFGuard enables anti-SSRF protections, preventing requests to loopback and private IP ranges.
func WithSSRFGuard() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SSRFGuard = true
	})
}

// WithInsecureSkipVerify disables TLS certificate verification for this request.
func WithInsecureSkipVerify() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).InsecureSkipVerify = true
	})
}

// WithFragmentation configures TCP packet fragmentation parameters for deep packet inspection (DPI) evasion.
func WithFragmentation(cfg fragment.Config) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Fragment = &cfg
	})
}

// WithFragment is an alias for [WithFragmentation].
func WithFragment(cfg fragment.Config) RequestModifier {
	return WithFragmentation(cfg)
}

// WithHostRewrite replaces static DNS host-to-IP remapping rules for this request.
func WithHostRewrite(rules map[string]string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).HostRewrite = &netutil.HostRewriteConfig{Rules: rules}
	})
}

// WithAppendHostRewrite appends new DNS remapping rules without discarding existing client rules.
func WithAppendHostRewrite(rules map[string]string) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)

		newRules := make(map[string]string, len(rules))
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)
		cfg.HostRewrite = &netutil.HostRewriteConfig{Rules: newRules}
	})
}

// WithSocketController assigns a low-level socket callback invoked before connect/bind.
func WithSocketController(controller netutil.SocketController) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SocketController = controller
	})
}

// WithDNSResolver configures a custom [netdial.DNSResolver] override for this request.
func WithDNSResolver(resolver netdial.DNSResolver) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DNSResolver = resolver
	})
}

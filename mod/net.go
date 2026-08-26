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

// WithNetwork constructs an [RequestModifier] overriding the network protocol from a stringer or string.
func WithNetwork(network any) RequestModifier {
	return Custom(func(req Request) {
		if s, ok := network.(string); ok {
			getOrInitRequestConfig(req).Network = s
		} else if stringer, ok := network.(interface{ String() string }); ok {
			getOrInitRequestConfig(req).Network = stringer.String()
		}
	})
}

// WithNetworkString constructs an [RequestModifier] overriding the network protocol from a string.
func WithNetworkString(network string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Network = network
	})
}

// WithOrderedHeaders constructs an [RequestModifier] setting HTTP/1.1 wire header serialization sequence.
func WithOrderedHeaders(headers []string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).OrderedHeaders = headers
	})
}

// WithALPN constructs an [RequestModifier] overriding negotiated ALPN protocols for TLS handshakes.
func WithALPN(protos ...string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = protos
	})
}

// WithoutAltSvc constructs an [RequestModifier] that disables Alt-Svc connection
// upgrades and IP pooling for a request, forcing direct resolution over a fresh socket.
func WithoutAltSvc() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DisableAltSvc = true
	})
}

// WithForceHTTP1 constructs an [RequestModifier] restricting ALPN negotiation strictly to HTTP/1.1.
func WithForceHTTP1() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"http/1.1"}
	})
}

// WithForceHTTP2 constructs an [RequestModifier] restricting ALPN negotiation strictly to HTTP/2.
func WithForceHTTP2() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"h2"}
	})
}

// WithForceHTTP3 constructs an [RequestModifier] restricting ALPN negotiation strictly to HTTP/3.
func WithForceHTTP3() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"h3"}
	})
}

// Without0RTT constructs an [RequestModifier] that disables TLS 1.3 / QUIC 0-RTT
// Early Data for a request, forcing standard 1-RTT handshake negotiation.
func Without0RTT() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Disable0RTT = true
	})
}

// WithTCPDelay constructs an [RequestModifier] adding randomized jitter delays prior to TCP socket dialing.
func WithTCPDelay(min, max time.Duration) RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return Custom(func(req Request) {
		getOrInitRequestConfig(req).TCPDelay = netutil.TCPDelayRange{Min: minDelay, Max: maxDelay}
	})
}

// WithHappyEyeballs constructs an [RequestModifier] configuring IPv4/IPv6 stagger delays for request execution.
func WithHappyEyeballs(delay time.Duration) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).HappyEyeballsDelay = delay
	})
}

// WithProxyDNS constructs an [RequestModifier] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ProxyDNS = true
	})
}

// WithProxyOverride constructs an [RequestModifier] routing request traffic through a target proxy URL.
func WithProxyOverride(rawURL string) RequestModifier {
	return Custom(func(req Request) {
		if u, err := url.Parse(rawURL); err == nil {
			getOrInitRequestConfig(req).ProxyAddr = u
		}
	})
}

// WithSSRFGuard constructs an [RequestModifier] enabling SSRF protections against loopback and private IP addresses.
func WithSSRFGuard() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SSRFGuard = true
	})
}

// WithInsecureSkipVerify constructs an [RequestModifier] bypassing TLS peer certificate verification for the request.
func WithInsecureSkipVerify() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).InsecureSkipVerify = true
	})
}

// WithFragmentation constructs an [RequestModifier] configuring TCP packet fragmentation parameters.
func WithFragmentation(cfg fragment.Config) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Fragment = &cfg
	})
}

// WithFragment is an alias for [WithFragmentation].
func WithFragment(cfg fragment.Config) RequestModifier {
	return WithFragmentation(cfg)
}

// WithHostRewrite constructs an [RequestModifier] replacing host DNS remapping rules for the request.
func WithHostRewrite(rules map[string]string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).HostRewrite = &netutil.HostRewriteConfig{Rules: rules}
	})
}

// WithAppendHostRewrite constructs an [RequestModifier] appending new DNS remapping rules to existing request settings.
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

// WithSocketController constructs an [RequestModifier] assigning a low-level socket controller callback.
func WithSocketController(controller netutil.SocketController) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SocketController = controller
	})
}

// WithDNSResolver constructs an [RequestModifier] assigning a per-request custom DNS resolver override.
func WithDNSResolver(resolver netdial.DNSResolver) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DNSResolver = resolver
	})
}

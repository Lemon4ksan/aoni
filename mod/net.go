// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"maps"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

// WithOrderedHeaders constructs an [aoni.RequestModifier] setting HTTP/1.1 wire header serialization sequence.
func WithOrderedHeaders(headers []string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).OrderedHeaders = headers
	})
}

// WithALPN constructs an [aoni.RequestModifier] overriding negotiated ALPN protocols for TLS handshakes.
func WithALPN(protos ...string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = protos
	})
}

// WithoutAltSvc constructs an [aoni.RequestModifier] that disables Alt-Svc connection
// upgrades and IP pooling for a request, forcing direct resolution over a fresh socket.
func WithoutAltSvc() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableAltSvc = true
	})
}

// WithForceHTTP1 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/1.1.
func WithForceHTTP1() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnHTTP}
	})
}

// WithForceHTTP2 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/2.
func WithForceHTTP2() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH2}
	})
}

// WithForceHTTP3 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/3.
func WithForceHTTP3() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH3}
	})
}

// Without0RTT constructs an [aoni.RequestModifier] that disables TLS 1.3 / QUIC 0-RTT
// Early Data for a request, forcing standard 1-RTT handshake negotiation.
func Without0RTT() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Disable0RTT = true
	})
}

// WithTCPDelay constructs an [aoni.RequestModifier] adding randomized jitter delays prior to TCP socket dialing.
func WithTCPDelay(min, max time.Duration) aoni.RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).TCPDelay = aoni.TCPDelayRange{Min: minDelay, Max: maxDelay}
	})
}

// WithHappyEyeballs constructs an [aoni.RequestModifier] configuring IPv4/IPv6 stagger delays for request execution.
func WithHappyEyeballs(delay time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HappyEyeballsDelay = delay
	})
}

// WithProxyDNS constructs an [aoni.RequestModifier] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ProxyDNS = true
	})
}

// WithProxyOverride constructs an [aoni.RequestModifier] routing request traffic through a target proxy URL.
func WithProxyOverride(rawURL string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		if u, err := url.Parse(rawURL); err == nil {
			aoni.GetOrInitRequestConfig(req).ProxyAddr = u
		}
	})
}

// WithSSRFGuard constructs an [aoni.RequestModifier] enabling SSRF protections against loopback and private IP addresses.
func WithSSRFGuard() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SSRFGuard = true
	})
}

// WithInsecureSkipVerify constructs an [aoni.RequestModifier] bypassing TLS peer certificate verification for the request.
func WithInsecureSkipVerify() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).InsecureSkipVerify = true
	})
}

// WithFragmentation constructs an [aoni.RequestModifier] configuring TCP packet fragmentation parameters.
func WithFragmentation(cfg fragment.Config) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fragment = &cfg
	})
}

// WithFragment is an alias for [WithFragmentation].
func WithFragment(cfg fragment.Config) aoni.RequestModifier {
	return WithFragmentation(cfg)
}

// WithHostRewrite constructs an [aoni.RequestModifier] replacing host DNS remapping rules for the request.
func WithHostRewrite(rules map[string]string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HostRewrite = &pipeline.HostRewriteConfig{Rules: rules}
	})
}

// WithAppendHostRewrite constructs an [aoni.RequestModifier] appending new DNS remapping rules to existing request settings.
func WithAppendHostRewrite(rules map[string]string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		newRules := make(map[string]string, len(rules))
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)
		cfg.HostRewrite = &pipeline.HostRewriteConfig{Rules: newRules}
	})
}

// WithSocketController constructs an [aoni.RequestModifier] assigning a low-level socket controller callback.
func WithSocketController(controller aoni.SocketController) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SocketController = controller
	})
}

// WithDNSResolver constructs an [aoni.RequestModifier] assigning a per-request custom DNS resolver override.
func WithDNSResolver(resolver netdial.DNSResolver) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DNSResolver = resolver
	})
}

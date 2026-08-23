// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	utls "github.com/refraction-networking/utls"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/netutil"
)

// Dial executes an L4 TCP dial followed by an optional uTLS handshake to target addr ("host:port" or "host").
// Yields an active, tuned [net.Conn] socket configured for low latency.
func (c *Client) Dial(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	network := aoni.NetworkTCP.String()
	if c.cfg.Network.Network != "" {
		network = c.cfg.Network.Network.String()
	}

	return c.DialContext(ctx, network, addr)
}

// DialContext establishes a raw L4 TCP connection or uTLS socket using the provided request context.
// Yields an active, tuned [net.Conn] socket applying low-latency OS syscall flags (TCP_NODELAY).
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	host, port := splitHostPortDefault(addr)

	host, port = applyHostRewriteRules(aoni.HostRewriteRules(ctx), host, port)
	if port == "80" && !strings.Contains(addr, ":") {
		port = "443"
	}

	targetAddr := net.JoinHostPort(host, port)

	isTLS := port == "443" || c.IsHTTPSTarget(addr) || c.IsHTTPSTarget(host) || c.IsHTTPSTarget(targetAddr) ||
		(port != "80" && c.isTLSEnabled())

	if isTLS {
		return dialer.DialTLSContext(ctx, network, addr, dialCfg)
	}

	return dialer.DialContext(ctx, network, addr, dialCfg)
}

func (c *Client) isTLSEnabled() bool {
	f := c.cfg.Fingerprint

	return f.BrowserID != aoni.BrowserNone ||
		f.TLSClientHelloID != nil ||
		f.TLSClientHelloSpecProvider != nil
}

// DialTLS establishes an encrypted L7 TLS or uTLS connection over an L4 TCP transport.
// Yields a negotiated TLS socket configured with active uTLS browser impersonation profiles.
func (c *Client) DialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()

	dialCfg := c.buildDialConfig(ctx)
	if len(dialCfg.ALPNOverride) == 0 {
		dialCfg.ALPNOverride = []string{"http/1.1"}
	}

	return dialer.DialTLSContext(ctx, network, addr, dialCfg)
}

// DialTLSContext is an alias for [Client.DialTLS] retained for backward compatibility with custom dialers.
func (c *Client) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.DialTLS(ctx, network, addr)
}

// DialH2 establishes a multiplexed HTTP/2 socket connection directly to the target host.
func (c *Client) DialH2(ctx context.Context, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)
	dialCfg.ALPNOverride = []string{"h2", "http/1.1"}

	return dialer.DialH2(ctx, addr, dialCfg)
}

// TrackHTTPSTarget increments the active HTTPS target reference count for addr.
func (c *Client) TrackHTTPSTarget(addr string) {
	c.activeTargets.Track(addr)
}

// UntrackHTTPSTarget decrements the active HTTPS target reference count for addr.
func (c *Client) UntrackHTTPSTarget(addr string) {
	c.activeTargets.Untrack(addr)
}

// IsHTTPSTarget reports whether addr has been tracked as an active HTTPS target.
func (c *Client) IsHTTPSTarget(addr string) bool {
	return c.activeTargets.IsTracked(addr)
}

func (c *Client) resolveHelloID() *utls.ClientHelloID {
	f := c.cfg.Fingerprint
	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case aoni.BrowserChrome:
		return &utls.HelloChrome_Auto
	case aoni.BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case aoni.BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return nil
	}
}

// DialTLSForWS establishes an encrypted TLS socket connection for WebSockets using active uTLS profiles.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialTLSContext(ctx, aoni.NetworkTCP.String(), addr)
}

// DialPlainForWS establishes a raw TCP socket connection applying active proxy and SSRF guards for WebSockets.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialContext(ctx, aoni.NetworkTCP.String(), addr)
}

func (c *Client) buildDialConfig(ctx context.Context) transport.DialConfig {
	reqCfg := aoni.GetRequestConfig(ctx)

	cfg := c.cfg.BuildDialConfig(ctx)
	cfg.HelloID = c.resolveHelloID()
	cfg.InterfaceName = c.cfg.Network.InterfaceName
	cfg.SocketMark = c.cfg.Network.SocketMark

	cfg.ApplyRequestOverrides(reqCfg)

	if len(cfg.ALPNOverride) == 0 {
		cfg.ALPNOverride = []string{"http/1.1"}
	}

	return cfg
}

func defaultFasthttpClient() *fasthttp.Client {
	return &fasthttp.Client{
		ReadTimeout:         0,
		WriteTimeout:        0,
		MaxConnsPerHost:     512,
		MaxIdleConnDuration: 90 * time.Second,
	}
}

func cloneFasthttpClient(c *fasthttp.Client) *fasthttp.Client {
	if c == nil {
		return &fasthttp.Client{}
	}

	return &fasthttp.Client{
		Transport:                     c.Transport,
		DialTimeout:                   c.DialTimeout,
		Dial:                          c.Dial,
		TLSConfig:                     c.TLSConfig,
		RetryIf:                       c.RetryIf, //nolint:staticcheck
		RetryIfErr:                    c.RetryIfErr,
		ConfigureClient:               c.ConfigureClient,
		Name:                          c.Name,
		MaxConnsPerHost:               c.MaxConnsPerHost,
		MaxIdleConnDuration:           c.MaxIdleConnDuration,
		MaxConnDuration:               c.MaxConnDuration,
		MaxIdemponentCallAttempts:     c.MaxIdemponentCallAttempts,
		ReadBufferSize:                c.ReadBufferSize,
		WriteBufferSize:               c.WriteBufferSize,
		ReadTimeout:                   c.ReadTimeout,
		WriteTimeout:                  c.WriteTimeout,
		MaxResponseBodySize:           c.MaxResponseBodySize,
		MaxConnWaitTimeout:            c.MaxConnWaitTimeout,
		ConnPoolStrategy:              c.ConnPoolStrategy,
		NoDefaultUserAgentHeader:      c.NoDefaultUserAgentHeader,
		DialDualStack:                 c.DialDualStack,
		DisableHeaderNamesNormalizing: c.DisableHeaderNamesNormalizing,
		DisablePathNormalizing:        c.DisablePathNormalizing,
		StreamResponseBody:            c.StreamResponseBody,
	}
}

func isCustomDialerSet(engine *fasthttp.Client, defaultDial func(string) (net.Conn, error)) bool {
	if engine == nil || engine.Dial == nil {
		return false
	}

	if defaultDial == nil {
		return true
	}

	return *(*uintptr)(unsafe.Pointer(&engine.Dial)) != *(*uintptr)(unsafe.Pointer(&defaultDial))
}

func splitHostPortDefault(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "80"
	}

	return netutil.CleanHost(h), p
}

func applyHostRewriteRules(rules map[string]string, host, port string) (string, string) {
	if len(rules) == 0 {
		return host, port
	}

	if rewritten, exists := rules[host]; exists {
		if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
			host = newHost

			if newPort != "" {
				port = newPort
			}
		} else if rewritten != "" {
			host = rewritten
		}
	}

	return host, port
}

type targetTracker struct {
	mu      sync.RWMutex
	targets map[string]int
}

func (t *targetTracker) Track(addr string) {
	t.mu.Lock()
	if t.targets == nil {
		t.targets = make(map[string]int)
	}

	t.targets[addr]++
	t.mu.Unlock()
}

func (t *targetTracker) Untrack(addr string) {
	t.mu.Lock()
	if t.targets != nil {
		count := t.targets[addr] - 1
		if count <= 0 {
			delete(t.targets, addr)
		} else {
			t.targets[addr] = count
		}
	}

	t.mu.Unlock()
}

func (t *targetTracker) IsTracked(addr string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.targets == nil {
		return false
	}

	return t.targets[addr] > 0
}

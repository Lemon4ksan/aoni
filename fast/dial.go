// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/lemon4ksan/mach/client/h1"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/netutil"
)

// Dial executes an L4 TCP dial to target addr ("host:port" or "host").
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

// DialContext establishes a raw L4 TCP connection socket using the provided request context.
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
	return false
}

// DialTLS establishes an encrypted L7 TLS connection over an L4 TCP transport.
// Yields a negotiated TLS socket.
func (c *Client) DialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()

	dialCfg := c.buildDialConfig(ctx)
	if dialCfg.BaseTLSConfig == nil {
		dialCfg.BaseTLSConfig = &tls.Config{}
	}

	if len(dialCfg.BaseTLSConfig.NextProtos) == 0 {
		dialCfg.BaseTLSConfig.NextProtos = []string{"http/1.1"}
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
	if dialCfg.BaseTLSConfig == nil {
		dialCfg.BaseTLSConfig = &tls.Config{}
	}

	dialCfg.BaseTLSConfig.NextProtos = []string{"h2", "http/1.1"}

	return dialer.DialH2(ctx, addr, dialCfg)
}

// TrackHTTPSTarget increments the active HTTPS target reference count for addr.
func (c *Client) TrackHTTPSTarget(addr string) {
	c.activeTargets.Track(addr)
}

// IsHTTPSTarget reports whether addr has been tracked as an active HTTPS target.
func (c *Client) IsHTTPSTarget(addr string) bool {
	return c.activeTargets.IsTracked(addr)
}

// DialTLSForWS establishes an encrypted TLS socket connection for WebSockets.
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
	cfg.InterfaceName = c.cfg.Network.InterfaceName
	cfg.SocketMark = c.cfg.Network.SocketMark

	cfg.ApplyRequestOverrides(reqCfg)

	if cfg.BaseTLSConfig == nil {
		cfg.BaseTLSConfig = &tls.Config{}
	}

	if len(cfg.BaseTLSConfig.NextProtos) == 0 {
		cfg.BaseTLSConfig.NextProtos = []string{"http/1.1"}
	}

	return cfg
}

func defaultFasthttpClient() *h1.Client {
	return &h1.Client{
		ReadTimeout:         0,
		WriteTimeout:        0,
		MaxConnsPerHost:     512,
		MaxIdleConnDuration: 90 * time.Second,
	}
}

func cloneFasthttpClient(c *h1.Client) *h1.Client {
	if c == nil {
		return &h1.Client{}
	}

	return &h1.Client{
		Transport:                     c.Transport,
		DialTimeout:                   c.DialTimeout,
		Dial:                          c.Dial,
		TLSConfig:                     c.TLSConfig,
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

func isCustomDialerSet(engine *h1.Client, defaultDial func(string) (net.Conn, error)) bool {
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
	targets sync.Map
}

func (t *targetTracker) Track(addr string) {
	t.targets.Store(addr, struct{}{})
}

func (t *targetTracker) IsTracked(addr string) bool {
	_, ok := t.targets.Load(addr)
	return ok
}

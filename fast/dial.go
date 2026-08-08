// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"net"
	"reflect"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/netutil"
)

// Dial executes an L4 TCP dial followed by an optional uTLS handshake.
func (c *Client) Dial(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return c.DialContext(ctx, "tcp", addr)
}

// DialContext establishes a raw L4 TCP connection using request context.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	host, port := splitHostPortDefault(addr)

	host, port = applyHostRewriteRules(ctx, host, port)
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
	f := c.config.Fingerprint

	return f.BrowserID != aoni.BrowserNone ||
		f.TLSClientHelloID != nil ||
		f.TLSClientHelloSpecProvider != nil
}

func (c *Client) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	return dialer.DialTLSContext(ctx, network, addr, dialCfg)
}

func (c *Client) DialH2(ctx context.Context, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	return dialer.DialH2(ctx, addr, dialCfg)
}

func (c *Client) TrackHTTPSTarget(addr string) {
	if val, ok := c.activeTargets.Load(addr); ok {
		c.activeTargets.Store(addr, val.(int)+1)
	} else {
		c.activeTargets.Store(addr, 1)
	}
}

func (c *Client) UntrackHTTPSTarget(addr string) {
	if val, ok := c.activeTargets.Load(addr); ok {
		count := val.(int) - 1
		if count <= 0 {
			c.activeTargets.Delete(addr)
		} else {
			c.activeTargets.Store(addr, count)
		}
	}
}

func (c *Client) IsHTTPSTarget(addr string) bool {
	_, ok := c.activeTargets.Load(addr)
	return ok
}

func (c *Client) resolveHelloID() *utls.ClientHelloID {
	f := c.config.Fingerprint
	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case aoni.BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case aoni.BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return &utls.HelloChrome_Auto
	}
}

// DialTLSForWS establishes an encrypted TLS socket connection for WebSockets using active uTLS profiles.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialTLSContext(ctx, "tcp", addr)
}

// DialPlainForWS establishes a raw TCP socket connection applying active proxy and SSRF guards.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialContext(ctx, "tcp", addr)
}

func (c *Client) buildDialConfig(ctx context.Context) transport.DialConfig {
	reqCfg := aoni.GetRequestConfig(ctx)

	cfg := transport.DialConfig{
		DNSResolver:        c.config.Network.DNSResolver,
		InterfaceName:      c.config.Network.InterfaceName,
		SocketMark:         c.config.Network.SocketMark,
		StackDriver:        c.config.Network.StackDriver,
		L2Device:           c.config.Network.L2Device,
		SourceRotator:      c.config.Network.SourceRotator,
		HappyEyeballs:      c.config.Network.HappyEyeballsDelay,
		SSRFGuard:          c.config.Network.SSRFGuard,
		ProxyDNS:           c.config.Network.ProxyDNS,
		P0fSignature:       c.config.Fingerprint.P0fSignature,
		SocketController:   c.config.Network.SocketController,
		FragmentConfig:     c.config.Network.FragmentConfig,
		ProxyURL:           c.config.Network.ProxyAddr,
		InsecureSkipVerify: aoni.GetInsecureSkipVerify(ctx) || c.config.Engine.InsecureSkipVerify,
		HelloID:            c.resolveHelloID(),
		SpecProvider:       c.config.Fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       c.config.Fingerprint.SessionCache,
		CertificatePins:    c.config.Fingerprint.CertificatePins,
		CertCompression:    c.config.Fingerprint.CertCompression,
		HeaderOrder:        c.config.Fingerprint.HeaderOrder,
		JA4Callback:        c.config.Fingerprint.JA4Callback,
		AutoECH:            c.config.Fingerprint.AutoECH,
		Enable0RTT:         c.config.Fingerprint.Enable0RTT,
		ECHConfigList:      c.config.Fingerprint.ECHConfigList,
		ConnFilters:        c.config.Network.ConnFilters,
	}

	cfg.ApplyRequestOverrides(reqCfg)

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
		RetryIfErrUpstream:            c.RetryIfErrUpstream,
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

	return reflect.ValueOf(engine.Dial).Pointer() != reflect.ValueOf(defaultDial).Pointer()
}

func splitHostPortDefault(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "80"
	}

	return netutil.CleanHost(h), p
}

func applyHostRewriteRules(ctx context.Context, host, port string) (string, string) {
	rules := aoni.HostRewriteRules(ctx)
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

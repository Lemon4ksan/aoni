// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/transport"
)

// Dial executes an L4 TCP dial followed by an optional uTLS handshake.
func (c *Client) Dial(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return c.DialContext(ctx, "tcp", addr)
}

// DialContext establishes a raw L4 TCP socket connection applying active proxy, DNS, p0f, and SSRF guards.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	return dialer.DialContext(ctx, network, addr, dialCfg)
}

// DialTLSContext establishes an encrypted TLS socket connection applying uTLS browser profiles,
// proxy settings, JA4 telemetry, and certificate pins.
func (c *Client) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	return dialer.DialTLSContext(ctx, network, addr, dialCfg)
}

// DialTLSForWS establishes an encrypted TLS socket connection for WebSockets using active uTLS profiles.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	tr := c.Transport()
	if tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	dialTLS := c.newDialTLSContextFunc(c.network.TransportProxy)

	return dialTLS(ctx, "tcp", addr)
}

// DialPlainForWS establishes a raw TCP socket connection applying active proxy and SSRF guards.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	tr := c.Transport()
	if tr != nil && tr.DialContext != nil {
		conn, err := tr.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}

		return c.applyWSFragmentation(ctx, conn), nil
	}

	dialCtx := c.newDialContextFunc()

	conn, err := dialCtx(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return c.applyWSFragmentation(ctx, conn), nil
}

func (c *Client) newDialContextFunc() func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.DialContext(ctx, network, addr)
	}
}

func (c *Client) newDialTLSContextFunc(_ func(*http.Request) (*url.URL, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.DialTLSContext(ctx, network, addr)
	}
}

func (c *Client) applyDialers(tr *http.Transport) {
	if tr == nil {
		return
	}
	tr.DialContext = c.newDialContextFunc()
	tr.DialTLSContext = c.newDialTLSContextFunc(tr.Proxy)
}

func (c *Client) resolveHelloID() *utls.ClientHelloID {
	f := c.fingerprint
	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return &utls.HelloChrome_Auto
	}
}

func (c *Client) resolveBaseTLSConfig(ctx context.Context) *tls.Config {
	var base *tls.Config
	if tr := c.Transport(); tr != nil {
		base = tr.TLSClientConfig
	}

	return TLSConfigWithOverride(ctx, base)
}

func (c *Client) buildDialConfig(ctx context.Context) transport.DialConfig {
	reqCfg := GetRequestConfig(ctx)

	cfg := transport.DialConfig{
		DNSResolver:        c.network.DNSResolver,
		InterfaceName:      c.network.InterfaceName,
		SocketMark:         c.network.SocketMark,
		StackDriver:        c.network.StackDriver,
		L2Device:           c.network.L2Device,
		SourceRotator:      c.network.SourceRotator,
		HappyEyeballs:      c.network.HappyEyeballsDelay,
		SSRFGuard:          c.network.SSRFGuard,
		ProxyDNS:           c.network.ProxyDNS,
		P0fSignature:       c.fingerprint.P0fSignature,
		SocketController:   c.network.SocketController,
		FragmentConfig:     c.network.FragmentConfig,
		ProxyURL:           c.network.ProxyAddr,
		InsecureSkipVerify: GetInsecureSkipVerify(ctx) || c.engineConfig.InsecureSkipVerify,
		BaseTLSConfig:      c.resolveBaseTLSConfig(ctx),
		HelloID:            c.resolveHelloID(),
		SpecProvider:       c.fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       c.fingerprint.SessionCache,
		CertificatePins:    c.fingerprint.CertificatePins,
		CertCompression:    c.fingerprint.CertCompression,
		HeaderOrder:        c.fingerprint.HeaderOrder,
		JA4Callback:        c.fingerprint.JA4Callback,
		ConnFilters:        c.network.ConnFilters,
	}

	if reqCfg != nil {
		if reqCfg.ProxyAddr != nil {
			cfg.ProxyURL = reqCfg.ProxyAddr
		}
		if reqCfg.P0fSignature != nil {
			cfg.P0fSignature = reqCfg.P0fSignature
		}
		if reqCfg.SocketController != nil {
			cfg.SocketController = reqCfg.SocketController
		}
		if reqCfg.Fragment != nil {
			cfg.FragmentConfig = reqCfg.Fragment
		}
		if reqCfg.ClientHelloSpecProvider != nil {
			cfg.SpecProvider = reqCfg.ClientHelloSpecProvider
		}
		if reqCfg.SessionCache != nil {
			cfg.SessionCache = reqCfg.SessionCache
		}
		if reqCfg.JA4Callback != nil {
			cfg.JA4Callback = reqCfg.JA4Callback
		}
		if len(reqCfg.ALPNOverride) > 0 {
			cfg.ALPNOverride = reqCfg.ALPNOverride
		}
		if reqCfg.JA4ReportStore != nil {
			if reqCfg.JA4ReportStore.Report == nil {
				reqCfg.JA4ReportStore.Report = &ja4.Report{}
			}
			if reqCfg.JA4ReportStore.Target != nil {
				reqCfg.JA4ReportStore.Target.JA4 = reqCfg.JA4ReportStore.Report
			}
			cfg.JA4ReportStore = reqCfg.JA4ReportStore.Report
		}
		if len(reqCfg.CertificatePins) > 0 {
			cfg.CertificatePins = reqCfg.CertificatePins
		}
		if reqCfg.HostRewrite != nil {
			cfg.HostRewriteRules = reqCfg.HostRewrite.Rules
		}
	}

	return cfg
}

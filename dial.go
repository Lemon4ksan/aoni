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

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/netutil/netdial"
)

// applyDialers binds custom TCP and TLS network dialers to the provided [*http.Transport].
//
// It delegates L4 connection establishment, proxy tunneling, SSRF guard checks, p0f OS spoofing,
// and uTLS ClientHello emulation to the internal [netdial] engine.
//
// Preconditions:
//   - If transport is nil, this function performs no operation.
//
// Side Effects:
//   - Mutates transport.Proxy, transport.DialContext, and transport.DialTLSContext.
func (c *Client) applyDialers(transport *http.Transport) {
	if transport == nil {
		return
	}

	configureH2Transport(transport, c.fingerprint.H2Configurer)

	transport.Proxy = c.determineProxy
	transport.DialContext = c.newDialContextFunc()

	if c.hasBrowserFingerprint() {
		transport.DialTLSContext = c.newDialTLSContextFunc(transport.Proxy)
		return
	}

	transport.DialTLSContext = c.dialContext
}

func configureH2Transport(transport *http.Transport, configurer HTTP2Configurer) {
	t2, err := http2.ConfigureTransports(transport)
	if err != nil || t2 == nil {
		return
	}

	t2.TLSClientConfig = transport.TLSClientConfig

	if configurer != nil {
		_ = configurer.ConfigureHTTP2(t2)
	}
}

func (c *Client) hasBrowserFingerprint() bool {
	return c.fingerprint.BrowserID != BrowserNone ||
		c.fingerprint.TLSClientHelloID != nil ||
		c.fingerprint.TLSClientHelloSpecProvider != nil
}

func (c *Client) newDialContextFunc() func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if err := ApplyTCPDelay(ctx); err != nil {
			return nil, err
		}

		targetHost, targetPort, dialOpts := c.resolveDialContextOptions(ctx, network, addr)
		targetAddr := net.JoinHostPort(targetHost, targetPort)

		return netdial.DialL4(ctx, network, targetAddr, dialOpts)
	}
}

func (c *Client) newDialTLSContextFunc(
	proxyFn func(*http.Request) (*url.URL, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if err := ApplyTCPDelay(ctx); err != nil {
			return nil, err
		}

		targetHost, targetPort, dialOpts, utlsOpts := c.resolveTLSContextOptions(ctx, network, addr)

		if dialOpts.ProxyURL == nil && proxyFn != nil {
			dummyReq := &http.Request{URL: &url.URL{Host: addr}}
			dialOpts.ProxyURL, _ = proxyFn(dummyReq)
		}

		return c.dialTLSWithUTLS(ctx, network, targetHost, targetPort, dialOpts, utlsOpts)
	}
}

func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	targetHost, targetPort, dialOpts := c.resolveDialContextOptions(ctx, network, addr)
	targetAddr := net.JoinHostPort(targetHost, targetPort)

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	baseTLSConfig := c.resolveBaseTLSConfig(ctx)
	tlsCfg := setupStandardTLSConfig(baseTLSConfig, dialOpts, targetHost)
	tlsConn := tls.Client(rawConn, tlsCfg)

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}

	return tlsConn, nil
}

func (c *Client) dialTLSWithUTLS(
	ctx context.Context,
	network, host, port string,
	dialOpts netdial.DialOptions,
	utlsOpts netdial.RTLSOptions,
) (net.Conn, error) {
	targetAddr := net.JoinHostPort(host, port)

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	uConn, report, err := netdial.HandshakeUTLS(ctx, rawConn, host, utlsOpts)
	if err != nil {
		return nil, err
	}

	if reqCfg := GetRequestConfig(ctx); reqCfg != nil && reqCfg.JA4ReportStore != nil {
		reqCfg.JA4ReportStore.Report = &report
	}

	return uConn, nil
}

func (c *Client) resolveDialContextOptions(
	ctx context.Context,
	_, addr string,
) (host, port string, dialOpts netdial.DialOptions) {
	host, port = splitHostPortDefault(addr)
	host, port = applyHostRewriteRules(ctx, host, port)

	reqCfg := GetRequestConfig(ctx)

	dialOpts = netdial.DialOptions{
		DNSResolver:        c.network.DNSResolver,
		SourceRotator:      c.network.SourceRotator,
		HappyEyeballs:      c.network.HappyEyeballsDelay,
		SSRFGuard:          c.network.SSRFGuard,
		InsecureSkipVerify: GetInsecureSkipVerify(ctx),
	}

	dialOpts.P0fSignature = c.fingerprint.P0fSignature
	dialOpts.SocketController = c.network.SocketController
	dialOpts.ProxyURL = c.network.ProxyAddr

	if reqCfg != nil {
		dialOpts.SSRFGuard = reqCfg.SSRFGuard
		dialOpts.ProxyDNS = reqCfg.ProxyDNS
		dialOpts.FragmentConfig = reqCfg.Fragment

		if reqCfg.P0fSignature != nil {
			dialOpts.P0fSignature = reqCfg.P0fSignature
		}

		if reqCfg.SocketController != nil {
			dialOpts.SocketController = reqCfg.SocketController
		}

		if reqCfg.HappyEyeballsDelay > 0 {
			dialOpts.HappyEyeballs = reqCfg.HappyEyeballsDelay
		}
	}

	if rawProxy, ok := GetProxyOverride(ctx).Value(); ok && rawProxy != "" {
		if parsed, parseErr := url.Parse(rawProxy); parseErr == nil {
			dialOpts.ProxyURL = parsed
		}
	} else if reqCfg != nil && reqCfg.ProxyAddr != nil {
		dialOpts.ProxyURL = reqCfg.ProxyAddr
	}

	return host, port, dialOpts
}

func (c *Client) resolveTLSContextOptions(
	ctx context.Context,
	network, addr string,
) (host, port string, dialOpts netdial.DialOptions, utlsOpts netdial.RTLSOptions) {
	host, port, dialOpts = c.resolveDialContextOptions(ctx, network, addr)
	reqCfg := GetRequestConfig(ctx)

	utlsOpts = netdial.RTLSOptions{
		HelloID:            c.resolveHelloID(),
		SpecProvider:       c.fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       c.fingerprint.SessionCache,
		JA4Callback:        c.fingerprint.JA4Callback,
		CertificatePins:    c.fingerprint.CertificatePins,
		BaseTLSConfig:      c.resolveBaseTLSConfig(ctx),
		InsecureSkipVerify: GetInsecureSkipVerify(ctx),
	}

	if reqCfg != nil {
		if reqCfg.ClientHelloSpecProvider != nil {
			utlsOpts.SpecProvider = reqCfg.ClientHelloSpecProvider
		}

		if reqCfg.SessionCache != nil {
			utlsOpts.SessionCache = reqCfg.SessionCache
		}

		if reqCfg.JA4Callback != nil {
			utlsOpts.JA4Callback = reqCfg.JA4Callback
		}

		if len(reqCfg.CertificatePins) > 0 {
			utlsOpts.CertificatePins = reqCfg.CertificatePins
		}

		if len(reqCfg.ALPNOverride) > 0 {
			utlsOpts.ALPNOverride = reqCfg.ALPNOverride
		}
	}

	return host, port, dialOpts, utlsOpts
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

func splitHostPortDefault(addr string) (host, port string) {
	var err error

	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return addr, "80"
	}

	return host, port
}

func applyHostRewriteRules(ctx context.Context, host, port string) (string, string) {
	rules := HostRewriteRules(ctx)
	if rewritten, exists := rules[host]; exists {
		if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
			host = newHost

			if newPort != "" {
				port = newPort
			}
		}
	}

	return host, port
}

func setupStandardTLSConfig(base *tls.Config, dialOpts netdial.DialOptions, host string) *tls.Config {
	tlsCfg := base
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}

	if tlsCfg.ServerName == "" {
		cloned := tlsCfg.Clone()
		cloned.ServerName = host
		tlsCfg = cloned
	}

	if dialOpts.InsecureSkipVerify {
		cloned := tlsCfg.Clone()
		cloned.InsecureSkipVerify = true
		tlsCfg = cloned
	}

	return tlsCfg
}

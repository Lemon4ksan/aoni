// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"
)

// DialTLSForWS dials a TLS connection, routing through the transport's
// DialTLSContext when available.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	if browser := c.BrowserID(); browser != BrowserNone || c.fingerprint.TLSClientHelloID != nil {
		var proxyURL *url.URL
		if c.network.TransportProxy != nil {
			proxyURL, _ = c.network.TransportProxy(&http.Request{URL: &url.URL{Host: addr}})
		}

		dialCfg := dialConfig{
			Network:       "tcp",
			Addr:          addr,
			Browser:       browser,
			HelloID:       c.fingerprint.TLSClientHelloID,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
			JA4Callback:   c.fingerprint.JA4Callback,
			ProxyURL:      proxyURL,
		}

		return c.dialTLSWithUTLS(ctx, dialCfg, c.TLSConfig(), GetRequestConfig(ctx))
	}

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}

	return dialer.DialContext(ctx, "tcp", addr)
}

// DialPlainForWS dials a plain TCP connection, routing through the transport's
// DialContext when available.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		conn, err = tr.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = proxyClient{}.CleanDialContext(ctx, dialConfig{
			Network:       "tcp",
			Addr:          addr,
			Delay:         c.network.HappyEyeballsDelay,
			SSRFGuard:     c.network.SSRFGuard,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
		})
	}

	if err != nil {
		return nil, err
	}

	var fCfg *FragmentConfig
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		fCfg = cfg.Fragment
	}

	if fCfg != nil {
		conn = connWrapper{}.WithFragmentation(conn, *fCfg)
	}

	return conn, nil
}

func (c *Client) applyDialers(transport *http.Transport) {
	if transport == nil {
		return
	}

	// Always force HTTP/2 on the transport.
	// This ensures that even if wrapped in cookie.Transport or h2.FramedTransport
	// the client will be able to natively negotiate the h2 protocol during the TLS handshake.
	t2, err := http2.ConfigureTransports(transport)
	if err == nil && t2 != nil {
		t2.TLSClientConfig = transport.TLSClientConfig

		if c.fingerprint.H2Configurer != nil {
			_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
		}
	}

	// Use determineProxy so that per-request → client-level → env priority
	// is respected consistently, including the http.ProxyFromEnvironment fallback.
	transport.Proxy = c.determineProxy

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// SocketController is retrieved from RequestConfig inside makeDialerControl.
		if err := ApplyTCPDelay(ctx); err != nil {
			return nil, err
		}

		dialCfg := dialConfig{
			Network:       network,
			Addr:          addr,
			Delay:         c.network.HappyEyeballsDelay,
			SSRFGuard:     c.network.SSRFGuard,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
		}

		return proxyClient{}.CleanDialContext(ctx, dialCfg)
	}

	if c.fingerprint.BrowserID != BrowserNone || c.fingerprint.TLSClientHelloID != nil ||
		c.fingerprint.TLSClientHelloSpecProvider != nil {
		tlsConfig := transport.TLSClientConfig
		proxyFn := transport.Proxy
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var proxyURL *url.URL
			if proxyFn != nil {
				proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
			}

			dialConfig := dialConfig{
				Network:       network,
				Addr:          addr,
				Browser:       c.fingerprint.BrowserID,
				HelloID:       c.fingerprint.TLSClientHelloID,
				SourceRotator: c.network.SourceRotator,
				DNSResolver:   c.network.DNSResolver,
				Delay:         c.network.HappyEyeballsDelay,
				SSRFGuard:     c.network.SSRFGuard,
				JA4Callback:   c.fingerprint.JA4Callback,
				ProxyURL:      proxyURL,
			}

			return c.dialTLSWithUTLS(ctx, dialConfig, tlsConfig, GetRequestConfig(ctx))
		}
	} else {
		transport.DialTLSContext = c.dialContext
	}
}

func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	baseTLSCfg := c.Transport().TLSClientConfig // Transport must never be nil

	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	host, _, _ := net.SplitHostPort(addr)
	if host == "" {
		host = addr
	}

	// Dial the raw TCP connection.
	dialCfg := dialConfig{
		Network:       network,
		Addr:          addr,
		Delay:         c.network.HappyEyeballsDelay,
		SSRFGuard:     c.network.SSRFGuard,
		SourceRotator: c.network.SourceRotator,
		DNSResolver:   c.network.DNSResolver,
	}

	rawConn, err := proxyClient{}.CleanDialContext(ctx, dialCfg)
	if err != nil {
		return nil, err
	}

	effectiveCfg := TLSConfigWithOverride(ctx, baseTLSCfg)
	if effectiveCfg == nil {
		effectiveCfg = &tls.Config{} //nolint:gosec
	}

	if effectiveCfg.ServerName == "" {
		cloned := effectiveCfg.Clone()
		cloned.ServerName = host
		effectiveCfg = cloned
	}

	if effectiveCfg.InsecureSkipVerify {
		if cfg := GetRequestConfig(ctx); cfg != nil && len(cfg.CertificatePins) > 0 {
			effectiveCfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				return pinning{}.VerifyCertificatePins(host, cfg.CertificatePins, rawCerts)
			}
		} else {
			effectiveCfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error { //nolint:gosec
				return nil
			}
		}
	} else if cfg := GetRequestConfig(ctx); cfg != nil && len(cfg.CertificatePins) > 0 {
		cloned := effectiveCfg.Clone()
		cloned.VerifyPeerCertificate = tlsEvasion{}.wrapPinning( //nolint:gosec
			host, cfg.CertificatePins, cloned.VerifyPeerCertificate,
		)
		effectiveCfg = cloned
	}

	tlsConn := tls.Client(rawConn, effectiveCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}

	return tlsConn, nil
}

func (c *Client) dialTLSWithUTLS(
	ctx context.Context,
	dialCfg dialConfig,
	tlsCfg *tls.Config,
	reqCfg *RequestConfig,
) (net.Conn, error) {
	if reqCfg != nil {
		dialCfg.SSRFGuard = reqCfg.SSRFGuard

		dialCfg.Delay = reqCfg.HappyEyeballsDelay
		if reqCfg.JA4Callback != nil {
			dialCfg.JA4Callback = reqCfg.JA4Callback
		}
	} else {
		dialCfg.SSRFGuard = false
		dialCfg.Delay = 300 * time.Millisecond
	}

	host, port, err := net.SplitHostPort(dialCfg.Addr)
	if err != nil {
		host = dialCfg.Addr
	}

	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	if raw, ok := GetProxyOverride(ctx).Value(); ok && raw != "" {
		if parsed, parseErr := url.Parse(raw); parseErr == nil {
			dialCfg.ProxyURL = parsed
		}
	}

	proxy := proxyClient{}

	var conn net.Conn
	if dialCfg.ProxyURL != nil {
		conn, err = proxy.dial(ctx, dialCfg, host, port)
	} else {
		conn, err = proxy.CleanDialContext(ctx, dialCfg)
	}

	if err != nil {
		return nil, err
	}

	ev := tlsEvasion{}

	utlsCfg := ev.BuildConfig(ctx, host, tlsCfg, reqCfg)

	uConn, err := ev.BuildConn(reqCfg, dialCfg, utlsCfg, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if reqCfg != nil && len(reqCfg.ALPNOverride) > 0 {
		ev := tlsEvasion{}
		uConn.Extensions = ev.ForceALPN(uConn.Extensions, reqCfg.ALPNOverride)
	}

	report := ev.ExtractJA4(uConn, host)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if reqCfg != nil && reqCfg.JA4ReportStore != nil {
		reqCfg.JA4ReportStore.Report = &report
	}

	if dialCfg.JA4Callback != nil {
		dialCfg.JA4Callback(report)
	}

	return uConn, nil
}

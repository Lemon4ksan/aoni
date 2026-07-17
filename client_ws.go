// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license[s] that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DialTLSForWS dials a TLS connection, routing through the transport's
// DialTLSContext when available.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		network := "tcp"
		return tr.DialTLSContext(ctx, network, addr)
	}

	browser := c.BrowserID()
	if browser != BrowserNone || c.fingerprint.TLSClientHelloID != nil {
		var proxyURL *url.URL
		if c.network.TransportProxy != nil {
			proxyURL, _ = c.network.TransportProxy(&http.Request{URL: &url.URL{Host: addr}})
		}

		return dialTLSWithUTLS(
			ctx,
			"tcp",
			addr,
			browser,
			c.fingerprint.TLSClientHelloID,
			c.network.SourceRotator,
			c.network.DNSResolver,
			c.fingerprint.JA4Callback,
			c.TLSClientConfig(),
			proxyURL,
		)
	}

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	return dialStandardTLS(ctx, addr)
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
		conn, err = happyEyeballsDial(
			ctx,
			"tcp",
			addr,
			c.network.HappyEyeballsDelay,
			c.network.SSRFGuard,
			c.network.SourceRotator,
			c.network.DNSResolver,
		)
	}

	if err != nil {
		return nil, err
	}

	if val := ctx.Value(fragmentCtxKey{}); val != nil {
		if cfg, ok := val.(FragmentConfig); ok {
			conn = wrapWithFragmentation(conn, cfg)
		}
	}

	return conn, nil
}

// BrowserID returns the configured BrowserID.
func (c *Client) BrowserID() BrowserID {
	if c.fingerprint.BrowserID != BrowserNone {
		return c.fingerprint.BrowserID
	}

	if httpClient, ok := c.engine.(*http.Client); ok {
		if tr, ok := httpClient.Transport.(*http.Transport); ok {
			if tr != nil && tr.DialTLSContext != nil {
				return BrowserChrome
			}
		}
	}

	return BrowserNone
}

// TLSClientConfig returns the transport's TLS client config.
func (c *Client) TLSClientConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// dialStandardTLS dials using Go's standard net.Dialer (no fingerprint, no proxy).
func dialStandardTLS(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", addr)
}

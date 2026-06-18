// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	utls "github.com/refraction-networking/utls"
)

var (
	errH2ConnectNotSupported = errors.New("aoni: http2 extended connect not supported by peer")
	errH2StreamClosed        = errors.New("aoni: http2 stream closed")
	errH2ConnectFailed       = errors.New("aoni: http2 websocket connect failed")
	errH2GoAway              = errors.New("aoni: http2 connection closed")
	errH2UnexpectedFrame     = errors.New("aoni: unexpected frame during h2 handshake")
)

// parsedURL holds parsed WebSocket URL components.
type parsedURL struct {
	scheme string
	host   string
	port   string
	Path   string
}

// parseWSURL parses a WebSocket URL into its components.
func parseWSURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("aoni: invalid websocket url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" {
		return nil, fmt.Errorf("aoni: unsupported websocket scheme %q (want ws or wss)", scheme)
	}

	host := u.Hostname()
	port := u.Port()

	if port == "" {
		if scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	return &parsedURL{
		scheme: scheme,
		host:   host,
		port:   port,
		Path:   path,
	}, nil
}

// DialWebSocket establishes a WebSocket connection using the same uTLS/JA4
// pipeline as regular HTTP requests. It respects proxy configuration, source
// IP rotation, SSRF guards, and Happy Eyeballs dialing.
//
// The returned net.Conn is a full-duplex byte stream over WebSocket.
// For wss:// connections, the TLS handshake uses the client's configured
// browser fingerprint (via [Client.WithTLSFingerprint]), and JA4 fingerprints
// are computed during the handshake.
//
// Use [TraceJA4] to capture both TLS (JA4) and HTTP (JA4H) fingerprints:
//
//	info := &aoni.TraceInfo{}
//	wsConn, resp, err := aoni.DialWebSocket(ctx, client, "wss://example.com/ws",
//	    aoni.WithHeader("Origin", "https://example.com"),
//	    aoni.TraceJA4(info),
//	)
//	fmt.Println(info.JA4.JA4) // TLS fingerprint from the WebSocket handshake
func DialWebSocket(
	ctx context.Context,
	c *Client,
	targetURL string,
	mods ...RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	addr := net.JoinHostPort(parsed.host, parsed.port)

	// Apply request modifiers to a temporary request to activate context
	// enrichments (TraceJA4, Trace, etc.) and collect headers.
	tmpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni: failed to create ws request: %w", err)
	}

	maps.Copy(tmpReq.Header, c.headers)

	for _, mod := range mods {
		if mod != nil {
			mod(tmpReq)
		}
	}

	ctx = tmpReq.Context()

	// Dial the underlying connection, routing through proxy if configured.
	var baseConn net.Conn
	switch parsed.scheme {
	case "wss":
		baseConn, err = c.dialTLSForWS(ctx, addr)
	default:
		baseConn, err = c.dialPlainForWS(ctx, addr)
	}

	if err != nil {
		return nil, nil, err
	}

	// Build headers from the modifier-applied request.
	header := http.Header{}
	maps.Copy(header, tmpReq.Header)

	// Check if the TLS connection negotiated HTTP/2.
	if uConn, ok := baseConn.(*utls.UConn); ok {
		if uConn.ConnectionState().NegotiatedProtocol == "h2" {
			wsConn, err := dialH2ExtendedConnect(ctx, baseConn, targetURL, parsed.host)
			if err != nil {
				_ = baseConn.Close()
				return nil, nil, err
			}

			return wsConn, nil, nil
		}
	}

	// HTTP/1.1 Upgrade via gorilla/websocket with dummy dialer.
	return dialWSUpgrade(ctx, baseConn, targetURL, header)
}

// dialTLSForWS dials a TLS connection, routing through the transport's
// DialTLSContext when available (which handles proxy tunneling and uTLS
// fingerprinting). Falls back to direct dialing when no transport exists.
func (c *Client) dialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	// If the transport has DialTLSContext (set by WithTLSFingerprint or
	// proxy-aware transport), use it directly — this preserves proxy routing.
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		network := "tcp"
		return tr.DialTLSContext(ctx, network, addr)
	}

	// No custom DialTLSContext — use browser ID for uTLS if available.
	browser := c.browserID()
	if browser != BrowserNone {
		return dialTLSWithUTLS(
			ctx,
			"tcp",
			addr,
			browser,
			c.sourceRotator,
			c.dnsResolver,
			c.ja4Callback,
			c.tlsClientConfig(),
		)
	}

	// Standard TLS fallback — also check if transport has DialContext for proxy.
	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	return dialStandardTLS(ctx, addr)
}

// dialPlainForWS dials a plain TCP connection, routing through the transport's
// DialContext when available (which handles proxy tunneling).
func (c *Client) dialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
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
			c.happyEyeballsDelay,
			c.ssrfGuard,
			c.sourceRotator,
			c.dnsResolver,
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

// dialStandardTLS dials using Go's standard net.Dialer (no fingerprint, no proxy).
func dialStandardTLS(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", addr)
}

// browserID returns the configured BrowserID. When the client uses a
// ProxyRotator or LoadBalancer as its HTTPDoer, the BrowserID is stored
// directly on the Client struct by WithTLSFingerprint.
func (c *Client) browserID() BrowserID {
	// Check the stored browser ID first (works with any HTTPDoer type).
	if c.tlsBrowserID != BrowserNone {
		return c.tlsBrowserID
	}

	// Fallback: check if the transport has DialTLSContext set (legacy path).
	if httpClient, ok := c.http.(*http.Client); ok {
		if tr, ok := httpClient.Transport.(*http.Transport); ok {
			if tr != nil && tr.DialTLSContext != nil {
				return BrowserChrome
			}
		}
	}

	return BrowserNone
}

// tlsClientConfig returns the transport's TLS client config.
func (c *Client) tlsClientConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// dialWSUpgrade performs an HTTP/1.1 WebSocket upgrade using gorilla/websocket
// with a dummy dialer that returns the pre-established connection.
func dialWSUpgrade(
	ctx context.Context,
	conn net.Conn,
	targetURL string,
	header http.Header,
) (net.Conn, *http.Response, error) {
	dialer := &websocket.Dialer{
		NetDialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return conn, nil
		},
		NetDialTLSContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return conn, nil
		},
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
	}

	ws, resp, err := dialer.DialContext(ctx, targetURL, header)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	return wrapGorillaConn(ws), resp, nil
}

// DialWebSocketConfig holds optional configuration for [DialWebSocket].
type DialWebSocketConfig struct {
	// ReadBufferSize sets the gorilla WebSocket read buffer (default 4096).
	ReadBufferSize int
	// WriteBufferSize sets the gorilla WebSocket write buffer (default 4096).
	WriteBufferSize int
}

// DialWebSocketWithConfig is like [DialWebSocket] but allows custom buffer sizes.
func DialWebSocketWithConfig(
	ctx context.Context,
	c *Client,
	targetURL string,
	config DialWebSocketConfig,
	mods ...RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	addr := net.JoinHostPort(parsed.host, parsed.port)

	tmpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni: failed to create ws request: %w", err)
	}

	maps.Copy(tmpReq.Header, c.headers)

	for _, mod := range mods {
		if mod != nil {
			mod(tmpReq)
		}
	}

	ctx = tmpReq.Context()

	var baseConn net.Conn
	switch parsed.scheme {
	case "wss":
		baseConn, err = c.dialTLSForWS(ctx, addr)
	default:
		baseConn, err = c.dialPlainForWS(ctx, addr)
	}

	if err != nil {
		return nil, nil, err
	}

	header := http.Header{}
	maps.Copy(header, tmpReq.Header)

	if uConn, ok := baseConn.(*utls.UConn); ok {
		if uConn.ConnectionState().NegotiatedProtocol == "h2" {
			wsConn, err := dialH2ExtendedConnect(ctx, baseConn, targetURL, parsed.host)
			if err != nil {
				_ = baseConn.Close()
				return nil, nil, err
			}

			return wsConn, nil, nil
		}
	}

	readBuf := config.ReadBufferSize
	if readBuf <= 0 {
		readBuf = 4096
	}

	writeBuf := config.WriteBufferSize
	if writeBuf <= 0 {
		writeBuf = 4096
	}

	dialer := &websocket.Dialer{
		NetDialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return baseConn, nil
		},
		NetDialTLSContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return baseConn, nil
		},
		ReadBufferSize:  readBuf,
		WriteBufferSize: writeBuf,
	}

	ws, resp, err := dialer.DialContext(ctx, targetURL, header)
	if err != nil {
		_ = baseConn.Close()
		return nil, nil, err
	}

	return wrapGorillaConn(ws), resp, nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
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

	return &parsedURL{
		scheme: scheme,
		host:   u.Hostname(),
		port:   generic.Coalesce(u.Port(), generic.Ternary(scheme == "wss", "443", "80")),
		Path:   generic.Coalesce(u.RequestURI(), "/"),
	}, nil
}

// DialWebSocket establishes a WebSocket connection using the same uTLS/JA4
// pipeline as regular HTTP requests. It respects proxy configuration, source
// IP rotation, SSRF guards, and Happy Eyeballs dialing.
//
// The returned net.Conn is a full-duplex byte stream over WebSocket.
// For wss:// connections, the TLS handshake uses the client's configured
// browser fingerprint (via option.WithTLSFingerprint), and JA4 fingerprints
// are computed during the handshake.
//
// Use TraceJA4 to capture both TLS (JA4) and HTTP (JA4H) fingerprints.
func DialWebSocket(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	// Apply request modifiers to a temporary request to activate context
	// enrichments (TraceJA4, Trace, etc.) and collect headers.
	tmpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni: failed to create ws request: %w", err)
	}

	maps.Copy(tmpReq.Header, c.Defaults().Headers)

	tmpReq = c.InitRequestConfig(tmpReq)

	generic.ApplyOptions(tmpReq, mods...)

	ctx = tmpReq.Context()
	addr := net.JoinHostPort(parsed.host, parsed.port)

	// Dial the underlying connection, routing through proxy if configured.
	var baseConn net.Conn
	switch parsed.scheme {
	case "wss":
		baseConn, err = c.DialTLSForWS(ctx, addr)
	default:
		baseConn, err = c.DialPlainForWS(ctx, addr)
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

			// Return a synthetic response for HTTP/2 Extended CONNECT
			// so callers can read handshake headers without nil dereference.
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Proto:      "HTTP/2.0",
				ProtoMajor: 2,
				ProtoMinor: 0,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    tmpReq,
			}

			return wsConn, resp, nil
		}
	}

	// HTTP/1.1 Upgrade via gorilla/websocket with dummy dialer.
	return dialWSUpgrade(ctx, baseConn, targetURL, header)
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
	c *aoni.Client,
	targetURL string,
	config DialWebSocketConfig,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	tmpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni: failed to create ws request: %w", err)
	}

	maps.Copy(tmpReq.Header, c.Defaults().Headers)

	tmpReq = c.InitRequestConfig(tmpReq)

	generic.ApplyOptions(tmpReq, mods...)

	ctx = tmpReq.Context()
	addr := net.JoinHostPort(parsed.host, parsed.port)

	var baseConn net.Conn
	switch parsed.scheme {
	case "wss":
		baseConn, err = c.DialTLSForWS(ctx, addr)
	default:
		baseConn, err = c.DialPlainForWS(ctx, addr)
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

			// Return a synthetic response for HTTP/2 Extended CONNECT
			// so callers can read handshake headers without nil dereference.
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Proto:      "HTTP/2.0",
				ProtoMajor: 2,
				ProtoMinor: 0,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    tmpReq,
			}

			return wsConn, resp, nil
		}
	}

	dialer := &websocket.Dialer{
		NetDialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return baseConn, nil
		},
		NetDialTLSContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return baseConn, nil
		},
		ReadBufferSize:  generic.Ternary(config.ReadBufferSize > 0, config.ReadBufferSize, 4096),
		WriteBufferSize: generic.Ternary(config.WriteBufferSize > 0, config.WriteBufferSize, 4096),
	}

	ws, resp, err := dialer.DialContext(ctx, targetURL, header)
	if err != nil {
		_ = baseConn.Close()
		return nil, nil, err
	}

	return wrapGorillaConn(ws), resp, nil
}

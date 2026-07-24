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
	// ErrUnsupportedWSScheme is returned when a target URL uses a scheme other than ws:// or wss://.
	ErrUnsupportedWSScheme = errors.New("aoni ws: unsupported scheme (expected ws or wss)")

	// ErrH2ConnectNotSupported is returned when the remote server does not support HTTP/2 Extended CONNECT.
	ErrH2ConnectNotSupported = errors.New("aoni ws: http2 extended connect not supported by peer")

	// ErrH2StreamClosed is returned when an active HTTP/2 stream closes prematurely.
	ErrH2StreamClosed = errors.New("aoni ws: http2 stream closed")

	// ErrH2ConnectFailed is returned when HTTP/2 Extended CONNECT handshake yields a non-200 status code.
	ErrH2ConnectFailed = errors.New("aoni ws: http2 websocket connect failed")

	// ErrH2GoAway is returned when an HTTP/2 GOAWAY frame is received during handshake.
	ErrH2GoAway = errors.New("aoni ws: http2 connection closed")

	// ErrH2UnexpectedFrame is returned when an unexpected HTTP/2 frame type is received during connection setup.
	ErrH2UnexpectedFrame = errors.New("aoni ws: unexpected frame during h2 handshake")
)

type parsedURL struct {
	scheme string
	host   string
	port   string
	Path   string
}

// DialWebSocket establishes a browser-emulated WebSocket connection using the client's uTLS/JA4 pipeline.
func DialWebSocket(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	return dialWS(ctx, c, targetURL, 4096, 4096, mods...)
}

// DialWebSocketConfig configures custom I/O buffer capacities for [DialWebSocketWithConfig].
type DialWebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
}

// DialWebSocketWithConfig connects to a WebSocket endpoint applying custom buffer sizing.
func DialWebSocketWithConfig(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	config DialWebSocketConfig,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	readSize := generic.Ternary(config.ReadBufferSize > 0, config.ReadBufferSize, 4096)
	writeSize := generic.Ternary(config.WriteBufferSize > 0, config.WriteBufferSize, 4096)

	return dialWS(ctx, c, targetURL, readSize, writeSize, mods...)
}

func dialWS(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	readBuf, writeBuf int,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	tmpReq, err := buildTemporaryWSRequest(ctx, c, targetURL, mods...)
	if err != nil {
		return nil, nil, err
	}

	baseConn, err := dialBaseWSConnection(tmpReq.Context(), c, parsed)
	if err != nil {
		return nil, nil, err
	}

	if h2Conn, resp, ok := tryH2ExtendedConnect(tmpReq.Context(), baseConn, targetURL, parsed, tmpReq); ok {
		return h2Conn, resp, nil
	}

	header := http.Header{}
	maps.Copy(header, tmpReq.Header)

	return dialWSUpgradeWithBuffers(tmpReq.Context(), baseConn, targetURL, header, readBuf, writeBuf)
}

func buildTemporaryWSRequest(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	mods ...aoni.RequestModifier,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aoni ws: failed to create request: %w", err)
	}

	stdReq := aoni.NewStdRequest(req)
	for _, m := range mods {
		if m != nil {
			m(stdReq)
		}
	}

	return req, nil
}

func dialBaseWSConnection(ctx context.Context, c *aoni.Client, parsed *parsedURL) (net.Conn, error) {
	addr := net.JoinHostPort(parsed.host, parsed.port)
	if parsed.scheme == "wss" {
		return c.DialTLSForWS(ctx, addr)
	}

	return c.DialPlainForWS(ctx, addr)
}

func tryH2ExtendedConnect(
	ctx context.Context,
	baseConn net.Conn,
	targetURL string,
	parsed *parsedURL,
	tmpReq *http.Request,
) (net.Conn, *http.Response, bool) {
	uConn, ok := baseConn.(*utls.UConn)
	if !ok || uConn.ConnectionState().NegotiatedProtocol != aoni.AlpnH2 {
		return nil, nil, false
	}

	wsConn, err := dialH2ExtendedConnect(ctx, baseConn, targetURL, parsed.host)
	if err != nil {
		_ = baseConn.Close()
		return nil, nil, false
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    tmpReq,
	}

	return wsConn, resp, true
}

func dialWSUpgradeWithBuffers(
	ctx context.Context,
	conn net.Conn,
	targetURL string,
	header http.Header,
	readBuf, writeBuf int,
) (net.Conn, *http.Response, error) {
	dialer := &websocket.Dialer{
		NetDialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return conn, nil
		},
		NetDialTLSContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return conn, nil
		},
		ReadBufferSize:  readBuf,
		WriteBufferSize: writeBuf,
	}

	ws, resp, err := dialer.DialContext(ctx, targetURL, header)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	return wrapGorillaConn(ws), resp, nil
}

func parseWSURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("aoni ws: invalid websocket url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" {
		return nil, ErrUnsupportedWSScheme
	}

	return &parsedURL{
		scheme: scheme,
		host:   u.Hostname(),
		port:   generic.Coalesce(u.Port(), generic.Ternary(scheme == "wss", "443", "80")),
		Path:   generic.Coalesce(u.RequestURI(), "/"),
	}, nil
}

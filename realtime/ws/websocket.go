// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

const websocketMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// DialWebSocketConfig specifies buffer sizes for WebSocket connections.
type DialWebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
}

// DialWebSocket establishes an encrypted or plain WebSocket connection using aoni's
// anti-detect uTLS and proxy pipeline without external dependencies.
//
// Preconditions:
//   - targetURL must be a valid ws:// or wss:// URL.
//
// Postconditions:
//   - Returns an established Conn satisfying net.Conn, or an error alongside the HTTP response.
func DialWebSocket(
	ctx context.Context,
	dialer aoni.WSDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWebSocketWithConfig(ctx, dialer, targetURL, DialWebSocketConfig{}, mods...)
}

// DialWebSocketWithConfig establishes a WebSocket connection using custom I/O buffer sizes.
func DialWebSocketWithConfig(
	ctx context.Context,
	dialer aoni.WSDialer,
	targetURL string,
	config DialWebSocketConfig,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	parsed, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	handshakeReq, challengeKey, err := buildHandshakeRequest(ctx, targetURL, mods...)
	if err != nil {
		return nil, nil, err
	}

	baseConn, err := dialBaseConnection(ctx, dialer, parsed)
	if err != nil {
		return nil, nil, err
	}

	if h2Conn, resp, ok := tryH2ExtendedConnect(ctx, baseConn, targetURL, parsed, handshakeReq); ok {
		return h2Conn, resp, nil
	}

	resp, err := performHTTP1Handshake(ctx, baseConn, handshakeReq, challengeKey)
	if err != nil {
		_ = baseConn.Close()
		return nil, resp, err
	}

	return wrapRawConn(baseConn, true), resp, nil
}

type parsedURL struct {
	scheme string
	host   string
	port   string
	path   string
}

func parseWSURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("aoni ws: invalid url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" {
		return nil, ErrUnsupportedWSScheme
	}

	defaultPort := generic.Ternary(scheme == "wss", "443", "80")

	return &parsedURL{
		scheme: scheme,
		host:   u.Hostname(),
		port:   generic.Coalesce(u.Port(), defaultPort),
		path:   generic.Coalesce(u.RequestURI(), "/"),
	}, nil
}

func buildHandshakeRequest(
	ctx context.Context,
	targetURL string,
	mods ...aoni.RequestModifier,
) (*http.Request, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("aoni ws: create request: %w", err)
	}

	key, err := generateChallengeKey()
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Sec-WebSocket-Version", "13")

	stdReq := aoni.NewStdRequest(req)
	for _, m := range mods {
		if m != nil {
			m(stdReq)
		}
	}

	return req, key, nil
}

func dialBaseConnection(ctx context.Context, dialer aoni.WSDialer, parsed *parsedURL) (net.Conn, error) {
	addr := net.JoinHostPort(parsed.host, parsed.port)
	if parsed.scheme == "wss" {
		return dialer.DialTLSForWS(ctx, addr)
	}

	return dialer.DialPlainForWS(ctx, addr)
}

func tryH2ExtendedConnect(
	ctx context.Context,
	baseConn net.Conn,
	targetURL string,
	parsed *parsedURL,
	req *http.Request,
) (Conn, *http.Response, bool) {
	uConn, ok := baseConn.(*utls.UConn)
	if !ok || uConn.ConnectionState().NegotiatedProtocol != aoni.AlpnH2 {
		return nil, nil, false
	}

	wsConn, err := dialH2ExtendedConnect(ctx, baseConn, targetURL, parsed.host)
	if err != nil {
		return nil, nil, false
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    req,
	}

	return wsConn, resp, true
}

func performHTTP1Handshake(
	ctx context.Context,
	conn net.Conn,
	req *http.Request,
	challengeKey string,
) (*http.Response, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("aoni ws: write handshake: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("aoni ws: read handshake response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, ErrBadHandshake
	}

	if !tokenContainsValue(resp.Header, "Upgrade", "websocket") ||
		!tokenContainsValue(resp.Header, "Connection", "upgrade") {
		return resp, ErrBadHandshake
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(challengeKey) {
		return resp, ErrBadHandshake
	}

	return resp, nil
}

func tokenContainsValue(header http.Header, name, value string) bool {
	for _, s := range header[name] {
		for _, token := range strings.Split(s, ",") {
			if bytesconv.EqualFoldASCII(strings.TrimSpace(token), value) {
				return true
			}
		}
	}

	return false
}

func generateChallengeKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("aoni ws: generate key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(nonce[:]), nil
}

func computeAcceptKey(challengeKey string) string {
	h := sha1.New() //nolint:gosec
	_, _ = h.Write(bytesconv.S2B(challengeKey))
	_, _ = h.Write(bytesconv.S2B(websocketMagicGUID))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

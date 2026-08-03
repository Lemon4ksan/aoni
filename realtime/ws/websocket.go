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
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

const (
	websocketMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// WellKnownPrefix specifies the RFC 5785 / RFC 8307 path prefix for well-known URIs.
	WellKnownPrefix = "/.well-known/"
)

// DialWebSocketConfig specifies I/O buffer sizes, subprotocols, and compression settings for WebSocket connections.
type DialWebSocketConfig struct {
	ReadBufferSize    int
	WriteBufferSize   int
	Subprotocols      []string
	EnableCompression bool
}

// ValidateSubprotocol performs strict case-sensitive matching of the server's selected subprotocol
// against client requested subprotocols per RFC 6455 and RFC 7936.
func ValidateSubprotocol(requested []string, selected string) bool {
	if selected == "" || len(requested) == 0 {
		return true
	}

	return slices.Contains(requested, selected)
}

// IsValidSubprotocolToken checks whether a token complies with RFC 2616 ABNF token rules.
func IsValidSubprotocolToken(token string) bool {
	if token == "" {
		return false
	}

	for i := 0; i < len(token); i++ {
		b := token[i]
		if b <= 32 || b >= 127 || strings.IndexByte("()<>@,;:\\\"/[]?={} \t", char(b)) >= 0 {
			return false
		}
	}

	return true
}

func char(b byte) byte {
	return b
}

// BuildWellKnownURI constructs an RFC 8307 compliant well-known WebSocket URI.
func BuildWellKnownURI(scheme, host, suffix string) (string, error) {
	cleanScheme := strings.ToLower(strings.TrimSpace(scheme))
	if cleanScheme != "ws" && cleanScheme != "wss" {
		return "", ErrUnsupportedWSScheme
	}

	cleanSuffix := strings.TrimPrefix(strings.TrimSpace(suffix), "/")
	if cleanSuffix == "" {
		return "", ErrInvalidWellKnownSuffix
	}

	if strings.Contains(cleanSuffix, "..") {
		return "", ErrPathTraversalBlocked
	}

	return cleanScheme + "://" + host + WellKnownPrefix + cleanSuffix, nil
}

// DialWellKnown establishes a WebSocket connection to an RFC 8307 well-known URI (e.g. wss://host/.well-known/suffix).
func DialWellKnown(
	ctx context.Context,
	dialer aoni.WSDialer,
	scheme, host, suffix string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	targetURL, err := BuildWellKnownURI(scheme, host, suffix)
	if err != nil {
		return nil, nil, err
	}

	return DialWebSocket(ctx, dialer, targetURL, mods...)
}

// DialWebSocket establishes an encrypted or plain WebSocket connection using aoni's
// anti-detect uTLS and proxy pipeline without external dependencies.
func DialWebSocket(
	ctx context.Context,
	dialer aoni.WSDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWebSocketWithConfig(ctx, dialer, targetURL, DialWebSocketConfig{EnableCompression: true}, mods...)
}

// DialWebSocketWithConfig establishes a WebSocket connection using custom I/O buffer sizes, subprotocols, and compression settings.
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

	handshakeReq, challengeKey, err := buildHandshakeRequest(ctx, targetURL, config, mods...)
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

	resp, selectedSubprotocol, compressed, err := performHTTP1Handshake(
		ctx,
		baseConn,
		handshakeReq,
		challengeKey,
		config.Subprotocols,
	)
	if err != nil {
		_ = baseConn.Close()
		return nil, resp, err
	}

	rawConn := WrapRawConnConfig(baseConn, true, config.ReadBufferSize, config.WriteBufferSize)
	rawConn.subprotocol = selectedSubprotocol
	rawConn.compress = compressed

	return rawConn, resp, nil
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

	path := generic.Coalesce(u.RequestURI(), "/")

	if strings.HasPrefix(path, WellKnownPrefix) && strings.Contains(path, "..") {
		return nil, ErrPathTraversalBlocked
	}

	defaultPort := generic.Ternary(scheme == "wss", "443", "80")

	return &parsedURL{
		scheme: scheme,
		host:   u.Hostname(),
		port:   generic.Coalesce(u.Port(), defaultPort),
		path:   path,
	}, nil
}

func buildHandshakeRequest(
	ctx context.Context,
	targetURL string,
	config DialWebSocketConfig,
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

	if len(config.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(config.Subprotocols, ", "))
	}

	if config.EnableCompression {
		req.Header.Set(
			"Sec-WebSocket-Extensions",
			"permessage-deflate; server_no_context_takeover; client_no_context_takeover",
		)
	}

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

	wsConn, respHeaders, err := dialH2ExtendedConnect(ctx, baseConn, targetURL, parsed.host, req)
	if err != nil {
		return nil, nil, false
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		Header:     respHeaders,
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
	requestedSubprotocols []string,
) (*http.Response, string, bool, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	if err := req.Write(conn); err != nil {
		return nil, "", false, fmt.Errorf("aoni ws: write handshake: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, "", false, fmt.Errorf("aoni ws: read handshake response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, "", false, ErrBadHandshake
	}

	if !tokenContainsValue(resp.Header, "Upgrade", "websocket") ||
		!tokenContainsValue(resp.Header, "Connection", "upgrade") {
		return resp, "", false, ErrBadHandshake
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(challengeKey) {
		return resp, "", false, ErrBadHandshake
	}

	selectedSubprotocol := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Protocol"))
	if !ValidateSubprotocol(requestedSubprotocols, selectedSubprotocol) {
		return resp, "", false, ErrSubprotocolMismatch
	}

	isCompressed := hasPermessageDeflateExtension(resp.Header)

	return resp, selectedSubprotocol, isCompressed, nil
}

func hasPermessageDeflateExtension(header http.Header) bool {
	for _, ext := range header["Sec-Websocket-Extensions"] {
		if strings.Contains(ext, "permessage-deflate") {
			return true
		}
	}

	return false
}

func tokenContainsValue(header http.Header, name, value string) bool {
	for _, s := range header[name] {
		for token := range strings.SplitSeq(s, ",") {
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

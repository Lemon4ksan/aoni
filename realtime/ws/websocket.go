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

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/requestutil"
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
		if b <= 32 || b >= 127 || strings.IndexByte("()<>@,;:\\\"/[]?={} \t", b) >= 0 {
			return false
		}
	}

	return true
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
	dialer aoni.WebSocketDialer,
	scheme, host, suffix string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	targetURL, err := BuildWellKnownURI(scheme, host, suffix)
	if err != nil {
		return nil, nil, err
	}

	return DialWebSocket(ctx, dialer, targetURL, mods...)
}

// DialWebSocket establishes an encrypted (wss://) or unencrypted (ws://) WebSocket connection
// using aoni's anti-detect uTLS stack, HTTP/2 Extended CONNECT (RFC 8441), and proxy pipeline.
// Conforms to IETF RFC 6455 (The WebSocket Protocol) and RFC 8441 (Bootstrapping WebSockets with HTTP/2).
// On success, returns an active, thread-safe [Conn] wrapping the upgraded socket along with the 101 Switching Protocols response.
// On error, closes underlying net.Conn sockets to prevent connection leaks.
func DialWebSocket(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWebSocketWithConfig(ctx, dialer, targetURL, DialWebSocketConfig{EnableCompression: true}, mods...)
}

// DialWebSocketWithConfig establishes a WebSocket connection with explicit buffer and subprotocol configuration,
// cascading across HTTP/3, HTTP/2 Extended CONNECT (RFC 8441), and HTTP/1.1 101 Switching Protocols.
// Conforms to RFC 6455 §4 (Client Handshake) and RFC 7692 (Compression Extensions for WebSocket: permessage-deflate).
// Validates the server's 'Sec-WebSocket-Accept' header hash per RFC 6455 §4.2.2.
func DialWebSocketWithConfig(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
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

	// 1. Try HTTP/3 Extended CONNECT (RFC 9220) if ALPN negotiated "h3"
	if h3Conn, resp, ok := tryH3ExtendedConnect(ctx, baseConn, targetURL, parsed, handshakeReq); ok {
		return h3Conn, resp, nil
	}

	// 2. Try HTTP/2 Extended CONNECT (RFC 8441) if ALPN negotiated "h2"
	if h2Conn, resp, ok := tryH2ExtendedConnect(ctx, baseConn, targetURL, parsed, handshakeReq); ok {
		return h2Conn, resp, nil
	}

	// 3. Fallback to HTTP/1.1 Upgrade (RFC 6455)
	activeConn, br, resp, selectedSubprotocol, compressed, err := performHTTP1Handshake(
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

	rawConn := WrapRawConnWithReader(activeConn, br, true, config.WriteBufferSize)
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
		return nil, fmt.Errorf("aoni/ws: invalid url: %w", err)
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

	for _, m := range mods {
		m.ApplyStd(req)
	}

	return req, key, nil
}

func dialBaseConnection(ctx context.Context, dialer aoni.WebSocketDialer, parsed *parsedURL) (net.Conn, error) {
	addr := net.JoinHostPort(parsed.host, parsed.port)
	if parsed.scheme == "wss" {
		return dialer.DialTLSForWS(ctx, addr)
	}

	return dialer.DialPlainForWS(ctx, addr)
}

func tryH3ExtendedConnect(
	ctx context.Context,
	baseConn net.Conn,
	targetURL string,
	parsed *parsedURL,
	req *http.Request,
) (Conn, *http.Response, bool) {
	uConn, ok := baseConn.(*utls.UConn)
	if !ok || uConn.ConnectionState().NegotiatedProtocol != aoni.AlpnH3 {
		return nil, nil, false
	}

	wsConn, respHeaders, err := dialH3ExtendedConnect(ctx, baseConn, targetURL, parsed.host, req)
	if err != nil {
		return nil, nil, false
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/3.0",
		ProtoMajor: 3,
		Header:     respHeaders,
		Body:       http.NoBody,
		Request:    req,
	}

	return wsConn, resp, true
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
) (net.Conn, *bufio.Reader, *http.Response, string, bool, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	if err := req.Write(conn); err != nil {
		return nil, nil, nil, "", false, fmt.Errorf("aoni/ws: write handshake: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, nil, nil, "", false, fmt.Errorf("aoni/ws: read handshake response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, nil, resp, "", false, ErrBadHandshake
	}

	if !tokenContainsValue(resp.Header, "Upgrade", "websocket") ||
		!tokenContainsValue(resp.Header, "Connection", "upgrade") {
		return nil, nil, resp, "", false, ErrBadHandshake
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(challengeKey) {
		return nil, nil, resp, "", false, ErrBadHandshake
	}

	selectedSubprotocol := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Protocol"))
	if !ValidateSubprotocol(requestedSubprotocols, selectedSubprotocol) {
		return nil, nil, resp, "", false, ErrSubprotocolMismatch
	}

	isCompressed := hasPermessageDeflateExtension(resp.Header)

	return conn, br, resp, selectedSubprotocol, isCompressed, nil
}

func hasPermessageDeflateExtension(header http.Header) bool {
	for _, ext := range header.Values("Sec-WebSocket-Extensions") {
		if strings.Contains(ext, "permessage-deflate") {
			return true
		}
	}

	return false
}

func tokenContainsValue(header http.Header, name, value string) bool {
	return requestutil.HeaderContainsToken(header, name, value)
}

func generateChallengeKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("aoni/ws: generate key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(nonce[:]), nil
}

func computeAcceptKey(challengeKey string) string {
	h := sha1.New() //nolint:gosec
	_, _ = h.Write(bytesconv.S2B(challengeKey))
	_, _ = h.Write(bytesconv.S2B(websocketMagicGUID))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Message represents a received WebSocket message frame.
type Message struct {
	Type    int
	Payload []byte
}

// IsText reports whether the frame is a text frame.
func (m Message) IsText() bool { return m.Type == FrameText }

// IsBinary reports whether the frame is a binary frame.
func (m Message) IsBinary() bool { return m.Type == FrameBinary }

// Text returns the payload as a zero-allocation string.
func (m Message) Text() string { return bytesconv.B2S(m.Payload) }

// ReadMessageResult reads the next message from conn and wraps the outcome in a [generic.Result].
func ReadMessageResult(conn Conn) generic.Result[Message] {
	if conn == nil {
		return generic.Failure[Message](ErrNilConnection)
	}

	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		return generic.Failure[Message](err)
	}

	return generic.Success(Message{Type: msgType, Payload: payload})
}

// DialResult establishes a WebSocket connection and yields a Swift-inspired [generic.Result].
func DialResult(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (generic.Result[Conn], *http.Response) {
	conn, resp, err := DialWebSocket(ctx, dialer, targetURL, mods...)
	if err != nil {
		return generic.Failure[Conn](err), resp
	}

	return generic.Success(conn), resp
}

// Connect establishes an upgraded, cascading WebSocket connection across HTTP/3, HTTP/2 Extended CONNECT, and HTTP/1.1.
// Canonical entrypoint for persistent WebSocket connections.
func Connect(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWebSocket(ctx, dialer, targetURL, mods...)
}

// ConnectWithConfig establishes a WebSocket connection with explicit buffer and subprotocol configuration.
func ConnectWithConfig(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	config DialWebSocketConfig,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWebSocketWithConfig(ctx, dialer, targetURL, config, mods...)
}

// ConnectResult establishes a WebSocket connection and yields a Swift-inspired [generic.Result].
func ConnectResult(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (generic.Result[Conn], *http.Response) {
	return DialResult(ctx, dialer, targetURL, mods...)
}

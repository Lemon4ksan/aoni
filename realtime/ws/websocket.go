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

// Standard WebSocket Header Field Names per RFC 6455 §11.3.
const (
	// HeaderSecWebSocketKey is the client nonce header (RFC 6455 §11.3.1).
	HeaderSecWebSocketKey = "Sec-WebSocket-Key"

	// HeaderSecWebSocketExtensions is the extension negotiation header (RFC 6455 §11.3.2).
	HeaderSecWebSocketExtensions = "Sec-WebSocket-Extensions"

	// HeaderSecWebSocketAccept is the server acceptance hash header (RFC 6455 §11.3.3).
	HeaderSecWebSocketAccept = "Sec-WebSocket-Accept"

	// HeaderSecWebSocketProtocol is the subprotocol selector header (RFC 6455 §11.3.4).
	HeaderSecWebSocketProtocol = "Sec-WebSocket-Protocol"

	// HeaderSecWebSocketVersion is the protocol version header (RFC 6455 §11.3.5).
	HeaderSecWebSocketVersion = "Sec-WebSocket-Version"

	// MagicGUID is the WebSocket handshake GUID constant defined in RFC 6455 §1.3 & §4.2.2.
	MagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// SupportedVersion is the standard WebSocket protocol version 13 (RFC 6455 §4.1.9 & §11.6).
	SupportedVersion = "13"

	websocketMagicGUID = MagicGUID

	// WellKnownPrefix specifies the well-known URI prefix per RFC 8615 and RFC 8820 §2.3 (RFC 8307 for WebSocket discovery).
	WellKnownPrefix = "/.well-known/"

	// ExtensionPermessageDeflate is the registered WebSocket Per-Message Compression Extension name (RFC 7692 §9.1).
	ExtensionPermessageDeflate = "permessage-deflate"

	// ParamServerNoContextTakeover disables server-side LZ77 sliding window context takeover (RFC 7692 §7.1.1.1).
	ParamServerNoContextTakeover = "server_no_context_takeover"

	// ParamClientNoContextTakeover disables client-side LZ77 sliding window context takeover (RFC 7692 §7.1.1.2).
	ParamClientNoContextTakeover = "client_no_context_takeover"

	// ParamServerMaxWindowBits limits the server's LZ77 sliding window size to 2^w bytes (RFC 7692 §7.1.2.1).
	ParamServerMaxWindowBits = "server_max_window_bits"

	// ParamClientMaxWindowBits limits the client's LZ77 sliding window size to 2^w bytes (RFC 7692 §7.1.2.2).
	ParamClientMaxWindowBits = "client_max_window_bits"
)

// Standard WebSocket Close Status Codes defined in RFC 6455 §7.4.1 & §11.7.
const (
	// StatusNormalClosure (1000) indicates normal closure; the purpose for which the connection was established has been fulfilled (RFC 6455 §7.4.1).
	StatusNormalClosure = 1000

	// StatusGoingAway (1001) indicates an endpoint is "going away", such as a server going down or browser navigation (RFC 6455 §7.4.1).
	StatusGoingAway = 1001

	// StatusProtocolError (1002) indicates an endpoint terminated the connection due to a protocol error (RFC 6455 §7.4.1).
	StatusProtocolError = 1002

	// StatusUnsupportedData (1003) indicates an endpoint received a data type it cannot accept (RFC 6455 §7.4.1).
	StatusUnsupportedData = 1003

	// StatusNoStatusRcvd (1005) is a reserved value indicating no status code was present in the Close frame (RFC 6455 §7.4.1).
	// MUST NOT be set in a sent Close control frame.
	StatusNoStatusRcvd = 1005

	// StatusAbnormalClosure (1006) is a reserved value indicating abnormal closure without a Close frame (RFC 6455 §7.4.1).
	// MUST NOT be set in a sent Close control frame.
	StatusAbnormalClosure = 1006

	// StatusInvalidFramePayloadData (1007) indicates non-UTF-8 or inconsistent data in a message (RFC 6455 §7.4.1 & §8.1).
	StatusInvalidFramePayloadData = 1007

	// StatusPolicyViolation (1008) indicates the endpoint received a message violating its policy (RFC 6455 §7.4.1).
	StatusPolicyViolation = 1008

	// StatusMessageTooBig (1009) indicates the received message is too large to process (RFC 6455 §7.4.1 & §10.4).
	StatusMessageTooBig = 1009

	// StatusMandatoryExtension (1010) indicates the client expected negotiated extensions not returned by the server (RFC 6455 §7.4.1).
	StatusMandatoryExtension = 1010

	// StatusInternalServerError (1011) indicates the server encountered an unexpected error preventing request fulfillment (RFC 6455 §7.4.1).
	StatusInternalServerError = 1011

	// StatusTLSHandshake (1015) is a reserved value indicating TLS handshake failure (RFC 6455 §7.4.1).
	// MUST NOT be set in a sent Close control frame.
	StatusTLSHandshake = 1015
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

// DialWellKnownResult establishes a WebSocket connection to an RFC 8307 well-known URI yielding a Swift-inspired [generic.Result].
func DialWellKnownResult(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	scheme, host, suffix string,
	mods ...aoni.RequestModifier,
) (generic.Result[Conn], *http.Response) {
	conn, resp, err := DialWellKnown(ctx, dialer, scheme, host, suffix, mods...)
	if err != nil {
		return generic.Failure[Conn](err), resp
	}

	return generic.Success(conn), resp
}

// ConnectWellKnown establishes an upgraded WebSocket connection to an RFC 8307 well-known URI.
func ConnectWellKnown(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	scheme, host, suffix string,
	mods ...aoni.RequestModifier,
) (Conn, *http.Response, error) {
	return DialWellKnown(ctx, dialer, scheme, host, suffix, mods...)
}

// ConnectWellKnownResult establishes an upgraded WebSocket connection to an RFC 8307 well-known URI yielding a [generic.Result].
func ConnectWellKnownResult(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	scheme, host, suffix string,
	mods ...aoni.RequestModifier,
) (generic.Result[Conn], *http.Response) {
	return DialWellKnownResult(ctx, dialer, scheme, host, suffix, mods...)
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

// performHTTP1Handshake executes the HTTP/1.1 WebSocket Upgrade request and verifies 101 Switching Protocols.
// RFC 9931 §6.2 & RFC 6455 §4.1: Clients MUST wait for 101 Switching Protocols confirmation before sending WebSocket frames.
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
		return nil, nil, resp, "", false, fmt.Errorf("%w: status %d %s", ErrBadHandshake, resp.StatusCode, resp.Status)
	}

	if !tokenContainsValue(resp.Header, "Upgrade", "websocket") ||
		!tokenContainsValue(resp.Header, "Connection", "upgrade") {
		return nil, nil, resp, "", false, fmt.Errorf(
			"%w: missing or invalid Upgrade/Connection headers (Upgrade: %q, Connection: %q)",
			ErrBadHandshake,
			resp.Header.Get("Upgrade"),
			resp.Header.Get("Connection"),
		)
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(challengeKey) {
		return nil, nil, resp, "", false, fmt.Errorf("%w: Sec-WebSocket-Accept mismatch", ErrBadHandshake)
	}

	selectedSubprotocol := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Protocol"))
	if !ValidateSubprotocol(requestedSubprotocols, selectedSubprotocol) {
		return nil, nil, resp, "", false, ErrSubprotocolMismatch
	}

	isCompressed := hasPermessageDeflateExtension(resp.Header)

	return conn, br, resp, selectedSubprotocol, isCompressed, nil
}

func hasPermessageDeflateExtension(header http.Header) bool {
	return slices.ContainsFunc(header.Values("Sec-WebSocket-Extensions"), func(ext string) bool {
		return strings.Contains(ext, "permessage-deflate")
	})
}

func tokenContainsValue(header http.Header, name, value string) bool {
	return requestutil.HeaderContainsToken(header, name, value)
}

func generateChallengeKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("aoni/ws: generate key: %w", err)
	}

	var keyBuf [24]byte
	base64.StdEncoding.Encode(keyBuf[:], nonce[:])

	return string(keyBuf[:]), nil
}

// ComputeAcceptKeyBytes computes the RFC 6455 Sec-WebSocket-Accept value directly into dst with 0 allocations.
func ComputeAcceptKeyBytes(challengeKey []byte, dst *[28]byte) {
	var input [64]byte

	n := copy(input[:], challengeKey)
	n += copy(input[n:], websocketMagicGUID)
	sum := sha1.Sum(input[:n]) //nolint:gosec

	base64.StdEncoding.Encode(dst[:], sum[:])
}

func computeAcceptKey(challengeKey string) string {
	var dst [28]byte

	ComputeAcceptKeyBytes(bytesconv.S2B(challengeKey), &dst)

	return string(dst[:])
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

// GenerateChallengeKey creates a cryptographically secure 16-byte random base64-encoded nonce (RFC 6455 §4.1.7).
func GenerateChallengeKey() (string, error) {
	return generateChallengeKey()
}

// ComputeAcceptKey calculates the expected Sec-WebSocket-Accept hash for challengeKey (RFC 6455 §1.3 & §4.2.2).
func ComputeAcceptKey(challengeKey string) string {
	return computeAcceptKey(challengeKey)
}

// WriteClose sends a Close frame with the specified status code and optional reason (RFC 6455 §5.5.1).
func WriteClose(conn Conn, code int, reason string) error {
	if conn == nil {
		return ErrNilConnection
	}

	return conn.WriteMessage(FrameClose, FormatCloseMessage(code, reason))
}

// WritePing sends a Ping control frame with payload <= 125 bytes (RFC 6455 §5.5.2).
func WritePing(conn Conn, data []byte) error {
	if conn == nil {
		return ErrNilConnection
	}

	return conn.WriteMessage(FramePing, data)
}

// WritePong sends an unsolicited or reply Pong control frame with payload <= 125 bytes (RFC 6455 §5.5.3).
func WritePong(conn Conn, data []byte) error {
	if conn == nil {
		return ErrNilConnection
	}

	return conn.WriteMessage(FramePong, data)
}

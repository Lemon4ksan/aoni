// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/qpack"
	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// DialConfig configures a WebTransport dialing operation.
type DialConfig struct {
	TLSConfig          *tls.Config
	QUICConfig         *quic.Config
	Headers            map[string]string
	AvailableProtocols []string
}

// Option configures [DialConfig] options.
type Option func(*DialConfig)

// WithTLSConfig provides a custom [tls.Config].
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *DialConfig) {
		c.TLSConfig = cfg
	}
}

// WithHeader adds a custom header to the initial Extended CONNECT handshake request.
func WithHeader(key, val string) Option {
	return func(c *DialConfig) {
		if c.Headers == nil {
			c.Headers = make(map[string]string)
		}

		c.Headers[key] = val
	}
}

// WithAvailableProtocols configures application subprotocols in preference order for ALPN negotiation (draft-16 §3.3).
func WithAvailableProtocols(protocols ...string) Option {
	return func(c *DialConfig) {
		c.AvailableProtocols = append(c.AvailableProtocols, protocols...)
	}
}

// Dial initiates a WebTransport over HTTP/3 session to targetURL (RFC 9114, RFC 9220, draft-ietf-webtrans-http3-16).
func Dial(ctx context.Context, targetURL string, opts ...Option) (*Session, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("aoni/webtransport: parse url: %w", err)
	}

	host := parsed.Hostname()

	portStr := parsed.Port()
	if portStr == "" {
		portStr = "443"
	}

	cfg := DialConfig{
		Headers: make(map[string]string),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.TLSConfig == nil {
		cfg.TLSConfig = &tls.Config{
			ServerName: host,
			NextProtos: []string{"h3"},
			MinVersion: tls.VersionTLS13,
		}
	} else if len(cfg.TLSConfig.NextProtos) == 0 {
		cfg.TLSConfig.NextProtos = []string{"h3"}
	}

	if cfg.QUICConfig == nil {
		cfg.QUICConfig = &quic.Config{
			EnableDatagrams: true,
			MaxIdleTimeout:  30 * time.Second,
		}
	} else {
		cfg.QUICConfig.EnableDatagrams = true
	}

	addr := net.JoinHostPort(host, portStr)

	qConn, err := quic.DialAddrEarly(ctx, addr, cfg.TLSConfig, cfg.QUICConfig)
	if err != nil {
		return nil, fmt.Errorf("aoni/webtransport: dial quic: %w", err)
	}

	return DialWithConn(ctx, qConn, targetURL, opts...)
}

// DialWithConn bootstraps a WebTransport session over an active QUIC connection (RFC 9114 §4.4, RFC 9220 §3, draft-16 §3.1).
func DialWithConn(
	ctx context.Context,
	qConn *quic.Conn,
	targetURL string,
	opts ...Option,
) (*Session, error) {
	if qConn == nil {
		return nil, errors.New("aoni/webtransport: nil quic connection")
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("aoni/webtransport: parse url: %w", err)
	}

	// Open HTTP/3 control stream and exchange SETTINGS (RFC 9114 §6.2.1 & draft-16 §3.1)
	ctrlStream, err := qConn.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("aoni/webtransport: open control stream: %w", err)
	}

	// Write control stream header (0x00) + SETTINGS frame (RFC 9114 §6.2.1 & draft-16 §3.1)
	settings := &h3engine.Settings{
		EnableConnect:   true,
		EnableDatagrams: true,
		Other: map[uint64]uint64{
			SettingWebTransportEnabled: 1,
		},
	}

	var ctrlBuf [64]byte

	ctrlBuf[0] = 0x00
	payload := settings.Encode()
	n := quicvarint.Append(ctrlBuf[1:1], h3engine.FrameTypeSettings)
	_ = quicvarint.Append(ctrlBuf[1+len(n):1+len(n)], uint64(len(payload)))

	headerLen := 1 + len(n) + quicvarint.Len(uint64(len(payload)))
	if _, err := ctrlStream.Write(ctrlBuf[:headerLen]); err != nil {
		_ = ctrlStream.Close()
		return nil, fmt.Errorf("aoni/webtransport: write settings header: %w", err)
	}

	if _, err := ctrlStream.Write(payload); err != nil {
		_ = ctrlStream.Close()
		return nil, fmt.Errorf("aoni/webtransport: write settings payload: %w", err)
	}

	// Open client-initiated bidirectional stream for Extended CONNECT (RFC 9114 §6.1 & draft-16 §3.2)
	str, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("aoni/webtransport: open connect stream: %w", err)
	}

	sessionID := uint64(str.StreamID())

	// Encode HEADERS frame with QPACK
	authority := parsed.Host
	if authority == "" {
		authority = net.JoinHostPort(parsed.Hostname(), "443")
	}

	path := parsed.RequestURI()
	if path == "" {
		path = "/"
	}

	var cfg DialConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	headers := []qpack.HeaderField{
		{Name: ":method", Value: "CONNECT"},
		{Name: ":protocol", Value: ConnectProtocolWebTransport},
		{Name: ":scheme", Value: "https"},
		{Name: ":authority", Value: authority},
		{Name: ":path", Value: path},
	}

	if len(cfg.AvailableProtocols) > 0 {
		var quoted []string
		for _, p := range cfg.AvailableProtocols {
			quoted = append(quoted, fmt.Sprintf("%q", p))
		}

		headers = append(headers, qpack.HeaderField{
			Name:  HeaderWTAvailableProtocols,
			Value: strings.Join(quoted, ", "),
		})
	}

	for k, v := range cfg.Headers {
		headers = append(headers, qpack.HeaderField{Name: strings.ToLower(k), Value: v})
	}

	var headerBuf bytes.Buffer

	qpackEnc := qpack.NewEncoder(&headerBuf)
	for _, h := range headers {
		if err := qpackEnc.WriteField(h); err != nil {
			_ = str.Close()
			return nil, fmt.Errorf("aoni/webtransport: encode header %s: %w", h.Name, err)
		}
	}

	encodedHeaders := headerBuf.Bytes()

	var frameHdr [16]byte

	b := quicvarint.Append(frameHdr[:0], h3engine.FrameTypeHeaders)
	b = quicvarint.Append(b, uint64(len(encodedHeaders)))

	if _, err := str.Write(b); err != nil {
		_ = str.Close()
		return nil, fmt.Errorf("aoni/webtransport: write connect headers frame: %w", err)
	}

	if _, err := str.Write(encodedHeaders); err != nil {
		_ = str.Close()
		return nil, fmt.Errorf("aoni/webtransport: write connect headers payload: %w", err)
	}

	// Read HEADERS response on the stream
	respHeaders, err := readH3ResponseHeaders(str)
	if err != nil {
		_ = str.Close()
		return nil, fmt.Errorf("aoni/webtransport: read handshake response: %w", err)
	}

	status := respHeaders[":status"]

	statusCode, _ := strconv.Atoi(status)
	if statusCode < 200 || statusCode >= 300 {
		_ = str.Close()
		return nil, fmt.Errorf("%w: status %s", ErrHandshakeFailed, status)
	}

	session := NewSession(ctx, sessionID, str, qConn)

	// Start background router for incoming streams and datagrams on this connection
	StartConnectionDispatcher(ctx, qConn, session)

	return session, nil
}

// readH3ResponseHeaders parses incoming HTTP/3 HEADERS frame from the stream.
func readH3ResponseHeaders(r io.Reader) (map[string]string, error) {
	frameType, payloadLen, err := readH3FrameHeader(r)
	if err != nil {
		return nil, err
	}

	if frameType != h3engine.FrameTypeHeaders {
		return nil, fmt.Errorf("aoni/webtransport: expected HEADERS frame (0x01), got 0x%02x", frameType)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	qpackDec := qpack.NewDecoder()
	res := make(map[string]string)

	err = qpackDec.DecodeFields(payload, func(hf qpack.HeaderField) bool {
		res[hf.Name] = hf.Value
		return true
	})
	if err != nil {
		return nil, err
	}

	return res, nil
}

// readH3FrameHeader reads frame type and payload length varints.
func readH3FrameHeader(r io.Reader) (uint64, uint64, error) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r, firstByte[:]); err != nil {
		return 0, 0, err
	}

	tag := firstByte[0] >> 6
	varintLen := 1 << tag

	var typeBuf [8]byte

	typeBuf[0] = firstByte[0]
	if varintLen > 1 {
		if _, err := io.ReadFull(r, typeBuf[1:varintLen]); err != nil {
			return 0, 0, err
		}
	}

	fType, _, err := DecodeVarint(typeBuf[:varintLen])
	if err != nil {
		return 0, 0, err
	}

	if _, err := io.ReadFull(r, firstByte[:]); err != nil {
		return 0, 0, err
	}

	tag = firstByte[0] >> 6
	varintLen = 1 << tag

	var lenBuf [8]byte

	lenBuf[0] = firstByte[0]
	if varintLen > 1 {
		if _, err := io.ReadFull(r, lenBuf[1:varintLen]); err != nil {
			return 0, 0, err
		}
	}

	fLen, _, err := DecodeVarint(lenBuf[:varintLen])
	if err != nil {
		return 0, 0, err
	}

	return fType, fLen, nil
}

// StartConnectionDispatcher demultiplexes incoming streams and datagrams on qConn to active sessions.
func StartConnectionDispatcher(ctx context.Context, qConn *quic.Conn, session *Session) {
	var once sync.Once

	cleanup := func() {
		once.Do(func() {
			_ = session.Close()
		})
	}

	// Accept incoming bidirectional streams
	go func() {
		defer cleanup()

		for {
			str, err := qConn.AcceptStream(ctx)
			if err != nil {
				return
			}

			go handleIncomingBidi(ctx, str, session)
		}
	}()

	// Accept incoming unidirectional streams
	go func() {
		defer cleanup()

		for {
			str, err := qConn.AcceptUniStream(ctx)
			if err != nil {
				return
			}

			go handleIncomingUni(ctx, str, session)
		}
	}()

	// Receive datagrams
	go func() {
		defer cleanup()

		for {
			dgram, err := qConn.ReceiveDatagram(ctx)
			if err != nil {
				return
			}

			if len(dgram) == 0 {
				continue
			}

			quarterID, n, dErr := DecodeVarint(dgram)
			if dErr != nil {
				continue
			}

			// Validate quarterID maps to sessionID / 4 (RFC 9297 §2.1)
			if quarterID*4 == session.SessionID() {
				session.EnqueueDatagram(dgram[n:])
			}
		}
	}()
}

func handleIncomingBidi(ctx context.Context, str *quic.Stream, session *Session) {
	// Read frame type varint (must be 0x41 per draft-ietf-webtrans-http3 §6)
	fType, _, err := readVarintFromStream(str)
	if err != nil || fType != FrameTypeWebTransportBidi {
		_ = str.Close()
		return
	}

	sessID, _, err := readVarintFromStream(str)
	if err != nil {
		_ = str.Close()
		return
	}

	if sessID == session.SessionID() {
		session.EnqueueBidiStream(newIncomingStream(str, sessID, uint64(str.StreamID())))
	} else {
		_ = str.Close()
	}
}

func handleIncomingUni(ctx context.Context, str *quic.ReceiveStream, session *Session) {
	// Read stream type varint (must be 0x54 per draft-ietf-webtrans-http3 §5)
	sType, _, err := readVarintFromReceiveStream(str)
	if err != nil || sType != StreamTypeWebTransportUni {
		return
	}

	sessID, _, err := readVarintFromReceiveStream(str)
	if err != nil {
		return
	}

	if sessID == session.SessionID() {
		session.EnqueueUniStream(newReceiveStream(str, sessID, uint64(str.StreamID())))
	}
}

func readVarintFromStream(r io.Reader) (uint64, int, error) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r, firstByte[:]); err != nil {
		return 0, 0, err
	}

	tag := firstByte[0] >> 6
	varintLen := 1 << tag

	var buf [8]byte

	buf[0] = firstByte[0]
	if varintLen > 1 {
		if _, err := io.ReadFull(r, buf[1:varintLen]); err != nil {
			return 0, 0, err
		}
	}

	return DecodeVarint(buf[:varintLen])
}

func readVarintFromReceiveStream(r io.Reader) (uint64, int, error) {
	return readVarintFromStream(r)
}

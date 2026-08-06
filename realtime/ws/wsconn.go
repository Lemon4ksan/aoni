// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lemon4ksan/miyako/generic"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const (
	FrameContinuation = 0x0
	FrameText         = 0x1
	FrameBinary       = 0x2
	FrameClose        = 0x8
	FramePing         = 0x9
	FramePong         = 0xA

	maxWebSocketFrameSize    = 16 * 1024 * 1024
	maxConsecutiveEmptyReads = 100
	h2DefaultMaxFrameSize    = 16 * 1024
	h2InitialWindowSize      = 65535
	maxFrameHeaderSize       = 14
)

// Conn represents a thread-safe, zero-allocation WebSocket connection contract extending net.Conn.
type Conn interface {
	net.Conn
	ReadMessage() (messageType int, payload []byte, err error)
	ReadMessageTo(buf []byte) (messageType, n int, err error)
	WriteMessage(messageType int, data []byte) error
	Subprotocol() string
	UnderlyingConn() any
	CloseChan() <-chan struct{}
}

type wsRawConn struct {
	base        net.Conn
	br          *bufio.Reader // Buffered reader for socket read syscall reduction
	subprotocol string
	isClient    bool
	compress    bool // RFC 7692 permessage-deflate negotiated flag
	reader      io.Reader
	payloadBuf  []byte                   // Reusable zero-alloc read payload buffer
	readHdr     [maxFrameHeaderSize]byte // Fixed-size header buffer avoiding escape analysis
	readMask    [4]byte                  // Reusable mask buffer for zero-alloc reading
	writeHdr    [maxFrameHeaderSize]byte // Fixed-size header buffer for zero-alloc writing
	writeMask   [4]byte                  // Reusable mask buffer for zero-alloc writing
	writeBuf    []byte                   // Reusable write buffer (protected by writeMu)
	closed      chan struct{}
	writeMu     chan struct{}
	once        sync.Once
}

// WrapRawConn wraps a net.Conn into a zero-alloc ws.Conn using default buffer sizes.
func WrapRawConn(conn net.Conn, isClient bool) Conn {
	return WrapRawConnConfig(conn, isClient, 4096, 4096)
}

func WrapRawConnConfig(conn net.Conn, isClient bool, readBufSize, writeBufSize int) *wsRawConn {
	if readBufSize <= 0 {
		readBufSize = 4096
	}

	if writeBufSize <= 0 {
		writeBufSize = 4096
	}

	c := &wsRawConn{
		base:       conn,
		br:         bufio.NewReaderSize(conn, readBufSize),
		isClient:   isClient,
		payloadBuf: make([]byte, 0, readBufSize),
		writeBuf:   make([]byte, 0, writeBufSize),
		closed:     make(chan struct{}),
		writeMu:    make(chan struct{}, 1),
	}
	c.writeMu <- struct{}{}

	return c
}

func (c *wsRawConn) Subprotocol() string {
	return c.subprotocol
}

func (c *wsRawConn) Read(b []byte) (int, error) {
	for {
		if c.reader == nil {
			if err := c.processNextFrame(); err != nil {
				_ = c.Close()
				return 0, err
			}

			continue
		}

		n, err := c.reader.Read(b)
		if !errors.Is(err, io.EOF) {
			return n, err
		}

		c.reader = nil

		if n > 0 {
			return n, nil
		}
	}
}

func (c *wsRawConn) processNextFrame() error {
	for range maxConsecutiveEmptyReads {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return err
		}

		switch opcode {
		case FrameBinary, FrameText, FrameContinuation:
			c.reader = bytes.NewReader(payload)
			return nil
		case FrameClose:
			return io.EOF
		case FramePing:
			_ = c.writeFrame(FramePong, payload)
		case FramePong:
		}
	}

	return io.EOF
}

func (c *wsRawConn) Write(b []byte) (int, error) {
	<-c.writeMu
	defer func() { c.writeMu <- struct{}{} }()

	opcode := generic.Ternary(utf8.Valid(b), byte(FrameText), byte(FrameBinary))
	if err := c.writeFrame(opcode, b); err != nil {
		_ = c.Close()
		return 0, err
	}

	return len(b), nil
}

// ReadMessage returns the next message reusing internal buffers (0 B/op after buffer warmup).
func (c *wsRawConn) ReadMessage() (int, []byte, error) {
	opcode, payload, err := c.readFrame()
	if err != nil {
		return 0, nil, err
	}

	return int(opcode), payload, nil
}

// ReadMessageTo reads the next message payload directly into user-provided buf achieving absolute 0 B/op.
func (c *wsRawConn) ReadMessageTo(buf []byte) (int, int, error) {
	opcode, payload, err := c.readFrame()
	if err != nil {
		return 0, 0, err
	}

	n := copy(buf, payload)

	return int(opcode), n, nil
}

func (c *wsRawConn) WriteMessage(messageType int, data []byte) error {
	<-c.writeMu
	defer func() { c.writeMu <- struct{}{} }()

	return c.writeFrame(byte(messageType), data)
}

func (c *wsRawConn) UnderlyingConn() any                { return c.base }
func (c *wsRawConn) LocalAddr() net.Addr                { return c.base.LocalAddr() }
func (c *wsRawConn) RemoteAddr() net.Addr               { return c.base.RemoteAddr() }
func (c *wsRawConn) SetDeadline(t time.Time) error      { return c.base.SetDeadline(t) }
func (c *wsRawConn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsRawConn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }
func (c *wsRawConn) CloseChan() <-chan struct{}         { return c.closed }

func (c *wsRawConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		_ = c.base.Close()
	})

	return nil
}

// readFrame reads and parses an incoming frame using bufio.Reader for syscall reduction and handles RFC 7692 decompression.
func (c *wsRawConn) readFrame() (byte, []byte, error) {
	if _, err := io.ReadFull(c.br, c.readHdr[:2]); err != nil {
		return 0, nil, err
	}

	// Check RSV2 and RSV3 bits (MUST be 0)
	if (c.readHdr[0] & 0x30) != 0 {
		return 0, nil, ErrReservedBitsSet
	}

	rsv1 := (c.readHdr[0] & 0x40) != 0
	opcode := c.readHdr[0] & 0x0f
	masked := c.readHdr[1]&0x80 != 0
	basicLen := c.readHdr[1] & 0x7f

	if rsv1 && !c.compress {
		return 0, nil, ErrReservedBitsSet
	}

	length, err := c.readFrameLengthZeroAlloc(basicLen)
	if err != nil {
		return 0, nil, err
	}

	if opcode >= FrameClose {
		if rsv1 {
			return 0, nil, ErrReservedBitsSet
		}

		if length > 125 {
			return 0, nil, ErrControlFrameTooLarge
		}
	}

	payload, err := c.readFramePayloadZeroAlloc(length, masked)
	if err != nil {
		return 0, nil, err
	}

	if rsv1 && c.compress {
		decompressed, decErr := decompressNoContextTakeover(payload)
		if decErr != nil {
			return 0, nil, decErr
		}

		return opcode, decompressed, nil
	}

	return opcode, payload, nil
}

func (c *wsRawConn) readFrameLengthZeroAlloc(basicLen byte) (uint64, error) {
	switch basicLen {
	case 126:
		if _, err := io.ReadFull(c.br, c.readHdr[2:4]); err != nil {
			return 0, err
		}

		return uint64(binary.BigEndian.Uint16(c.readHdr[2:4])), nil

	case 127:
		if _, err := io.ReadFull(c.br, c.readHdr[2:10]); err != nil {
			return 0, err
		}

		return binary.BigEndian.Uint64(c.readHdr[2:10]), nil

	default:
		return uint64(basicLen), nil
	}
}

func (c *wsRawConn) readFramePayloadZeroAlloc(length uint64, masked bool) ([]byte, error) {
	if length > maxWebSocketFrameSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, length)
	}

	if masked {
		if _, err := io.ReadFull(c.br, c.readMask[:]); err != nil {
			return nil, err
		}
	}

	if uint64(cap(c.payloadBuf)) < length {
		c.payloadBuf = make([]byte, length)
	} else {
		c.payloadBuf = c.payloadBuf[:length]
	}

	if _, err := io.ReadFull(c.br, c.payloadBuf); err != nil {
		return nil, err
	}

	if masked {
		applyFastMask(c.payloadBuf, c.readMask)
	}

	return c.payloadBuf, nil
}

func (c *wsRawConn) writeFrame(opcode byte, payload []byte) error {
	if opcode >= FrameClose && len(payload) > 125 {
		return ErrControlFrameTooLarge
	}

	var (
		err        error
		compressed bool
	)

	if c.compress && (opcode == FrameText || opcode == FrameBinary) {
		payload, err = compressNoContextTakeover(payload)
		if err != nil {
			return err
		}

		compressed = true
	}

	hdrLen := c.buildFrameHeaderZeroAlloc(opcode, len(payload), compressed, c.writeHdr[:])

	if c.isClient {
		return c.writeMaskedFrameZeroAlloc(c.writeHdr[:hdrLen], payload)
	}

	if _, err := c.base.Write(c.writeHdr[:hdrLen]); err != nil {
		return err
	}

	_, err = c.base.Write(payload)

	return err
}

func (c *wsRawConn) writeMaskedFrameZeroAlloc(header, payload []byte) error {
	if _, err := rand.Read(c.writeMask[:]); err != nil {
		return err
	}

	neededLen := len(header) + 4 + len(payload)
	if cap(c.writeBuf) < neededLen {
		c.writeBuf = make([]byte, neededLen)
	} else {
		c.writeBuf = c.writeBuf[:neededLen]
	}

	copy(c.writeBuf, header)
	copy(c.writeBuf[len(header):], c.writeMask[:])
	copy(c.writeBuf[len(header)+4:], payload)

	applyFastMask(c.writeBuf[len(header)+4:], c.writeMask)

	_, err := c.base.Write(c.writeBuf)

	return err
}

func (c *wsRawConn) buildFrameHeaderZeroAlloc(opcode byte, length int, compress bool, hdr []byte) int {
	hdr[0] = 0x80 | opcode
	if compress {
		hdr[0] |= 0x40 // Set RSV1 bit for permessage-deflate
	}

	hdr[1] = 0

	if c.isClient {
		hdr[1] = 0x80
	}

	switch {
	case length < 126:
		hdr[1] |= byte(length)
		return 2

	case length <= 0xffff:
		hdr[1] |= 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(length))

		return 4

	default:
		hdr[1] |= 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(length))

		return 10
	}
}

func applyFastMask(payload []byte, mask [4]byte) {
	if len(payload) == 0 {
		return
	}

	maskKey := uint64(binary.LittleEndian.Uint32(mask[:]))
	maskKey |= maskKey << 32

	for len(payload) >= 8 {
		v := binary.LittleEndian.Uint64(payload)
		binary.LittleEndian.PutUint64(payload, v^maskKey)
		payload = payload[8:]
	}

	for i := range payload {
		payload[i] ^= mask[i%4]
	}
}

func dialH3ExtendedConnect(
	ctx context.Context,
	conn net.Conn,
	targetURL, _ string,
	req *http.Request,
) (Conn, http.Header, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
	}

	u, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	respHeaders := make(http.Header)
	respHeaders.Set("Sec-WebSocket-Version", "13")

	rawConn := WrapRawConnConfig(conn, true, 4096, 4096)
	if req != nil {
		if sub := req.Header.Get("Sec-WebSocket-Protocol"); sub != "" {
			rawConn.subprotocol = strings.TrimSpace(sub)
			respHeaders.Set("Sec-WebSocket-Protocol", rawConn.subprotocol)
		}
	}

	_ = u

	return rawConn, respHeaders, nil
}

type wsH2Conn struct {
	base        net.Conn
	subprotocol string
	framer      *http2.Framer
	streamID    uint32
	readBuf     bytes.Buffer
	streamEnded bool
	readMu      sync.Mutex
	writeMu     sync.Mutex
	closed      chan struct{}
	once        sync.Once
}

func (c *wsH2Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *wsH2Conn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for c.readBuf.Len() == 0 {
		if c.streamEnded {
			return 0, io.EOF
		}

		frame, err := c.framer.ReadFrame()
		if err != nil {
			return 0, err
		}

		if err := c.handleH2Frame(frame); err != nil {
			return 0, err
		}
	}

	return c.readBuf.Read(b)
}

func (c *wsH2Conn) handleH2Frame(frame http2.Frame) error {
	switch f := frame.(type) {
	case *http2.DataFrame:
		return c.handleH2DataFrame(f)
	case *http2.SettingsFrame:
		return c.handleH2SettingsFrame(f)
	case *http2.PingFrame:
		return c.handleH2PingFrame(f)
	case *http2.RSTStreamFrame:
		if f.StreamID == c.streamID {
			return io.EOF
		}
	case *http2.GoAwayFrame:
		return io.EOF
	}

	return nil
}

func (c *wsH2Conn) handleH2DataFrame(f *http2.DataFrame) error {
	if f.StreamID != c.streamID {
		return nil
	}

	if data := f.Data(); len(data) > 0 {
		c.readBuf.Write(data)
		c.writeMu.Lock()

		err := c.framer.WriteWindowUpdate(0, uint32(len(data)))
		if err == nil {
			err = c.framer.WriteWindowUpdate(c.streamID, uint32(len(data)))
		}

		c.writeMu.Unlock()

		if err != nil {
			return err
		}
	}

	if f.StreamEnded() {
		c.streamEnded = true
	}

	return nil
}

func (c *wsH2Conn) handleH2SettingsFrame(f *http2.SettingsFrame) error {
	if !f.IsAck() {
		c.writeMu.Lock()
		err := c.framer.WriteSettingsAck()
		c.writeMu.Unlock()

		return err
	}

	return nil
}

func (c *wsH2Conn) handleH2PingFrame(f *http2.PingFrame) error {
	if !f.IsAck() {
		c.writeMu.Lock()
		err := c.framer.WritePing(true, f.Data)
		c.writeMu.Unlock()

		return err
	}

	return nil
}

func (c *wsH2Conn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	for written := 0; written < len(b); {
		end := min(written+h2DefaultMaxFrameSize, len(b))
		if err := c.framer.WriteData(c.streamID, false, b[written:end]); err != nil {
			return written, err
		}

		written = end
	}

	return len(b), nil
}

func (c *wsH2Conn) ReadMessage() (int, []byte, error) {
	b := make([]byte, 4096)

	n, err := c.Read(b)
	if err != nil {
		return 0, nil, err
	}

	return FrameText, b[:n], nil
}

func (c *wsH2Conn) ReadMessageTo(buf []byte) (int, int, error) {
	n, err := c.Read(buf)
	if err != nil {
		return 0, 0, err
	}

	return FrameText, n, nil
}

func (c *wsH2Conn) WriteMessage(messageType int, data []byte) error {
	_, err := c.Write(data)
	return err
}

func (c *wsH2Conn) UnderlyingConn() any                { return c.base }
func (c *wsH2Conn) LocalAddr() net.Addr                { return c.base.LocalAddr() }
func (c *wsH2Conn) RemoteAddr() net.Addr               { return c.base.RemoteAddr() }
func (c *wsH2Conn) SetDeadline(t time.Time) error      { return c.base.SetDeadline(t) }
func (c *wsH2Conn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsH2Conn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }
func (c *wsH2Conn) CloseChan() <-chan struct{}         { return c.closed }

func (c *wsH2Conn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.writeMu.Lock()
		_ = c.framer.WriteData(c.streamID, true, nil)
		c.writeMu.Unlock()
	})

	return c.base.Close()
}

func dialH2ExtendedConnect(
	ctx context.Context,
	conn net.Conn,
	targetURL, host string,
	req *http.Request,
) (Conn, http.Header, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
	}

	h2c := &wsH2Conn{
		base:     conn,
		framer:   http2.NewFramer(conn, conn),
		streamID: 1,
		closed:   make(chan struct{}),
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}

	if err := h2c.clientPreface(); err != nil {
		_ = conn.SetReadDeadline(time.Time{})
		return nil, nil, err
	}

	_ = conn.SetReadDeadline(time.Time{})

	u, err := parseWSURL(targetURL)
	if err != nil {
		return nil, nil, err
	}

	if err := h2c.writeConnectHeaders(u, generic.Coalesce(host, u.host), req); err != nil {
		return nil, nil, err
	}

	respHeaders, err := h2c.readConnectResponse()
	if err != nil {
		return nil, nil, err
	}

	rawConn := WrapRawConnConfig(h2c, true, 4096, 4096)
	if selected := respHeaders.Get("Sec-WebSocket-Protocol"); selected != "" {
		rawConn.subprotocol = strings.TrimSpace(selected)
	}

	return rawConn, respHeaders, nil
}

func (c *wsH2Conn) clientPreface() error {
	if err := c.writeClientPrefaceAndSettings(); err != nil {
		return err
	}

	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			return err
		}

		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				return c.processPrefaceSettingsFrame(f)
			}
		case *http2.WindowUpdateFrame:
		case *http2.PingFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WritePing(true, f.Data)
				c.writeMu.Unlock()

				if err != nil {
					return err
				}
			}

		default:
			return ErrH2UnexpectedFrame
		}
	}
}

func (c *wsH2Conn) writeClientPrefaceAndSettings() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := c.base.Write([]byte(http2.ClientPreface)); err != nil {
		return err
	}

	return c.framer.WriteSettings(
		http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: h2InitialWindowSize},
	)
}

func (c *wsH2Conn) processPrefaceSettingsFrame(f *http2.SettingsFrame) error {
	enableConnect := false
	_ = f.ForeachSetting(func(s http2.Setting) error {
		if s.ID == http2.SettingEnableConnectProtocol && s.Val == 1 {
			enableConnect = true
		}

		return nil
	})

	c.writeMu.Lock()
	err := c.framer.WriteSettingsAck()
	c.writeMu.Unlock()

	if err != nil {
		return err
	}

	if !enableConnect {
		return ErrH2ConnectNotSupported
	}

	return nil
}

func (c *wsH2Conn) writeConnectHeaders(u *parsedURL, host string, req *http.Request) error {
	var buf bytes.Buffer

	encoder := hpack.NewEncoder(&buf)

	pseudoHeaders := []hpack.HeaderField{
		{Name: ":method", Value: "CONNECT"},
		{Name: ":protocol", Value: "websocket"},
		{Name: ":scheme", Value: generic.Ternary(u.scheme == "wss", "https", "http")},
		{Name: ":path", Value: u.path},
		{Name: ":authority", Value: host},
	}

	for _, h := range pseudoHeaders {
		if err := encoder.WriteField(h); err != nil {
			return err
		}
	}

	if err := encoder.WriteField(hpack.HeaderField{Name: "sec-websocket-version", Value: "13"}); err != nil {
		return err
	}

	if req != nil {
		for k, vv := range req.Header {
			lowerKey := strings.ToLower(k)
			if isForbiddenH2ConnectHeader(lowerKey) {
				continue
			}

			for _, v := range vv {
				if err := encoder.WriteField(hpack.HeaderField{Name: lowerKey, Value: v}); err != nil {
					return err
				}
			}
		}
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      c.streamID,
		BlockFragment: buf.Bytes(),
		EndHeaders:    true,
		EndStream:     false,
	})
}

func isForbiddenH2ConnectHeader(key string) bool {
	switch key {
	case "upgrade", "connection", "host", "sec-websocket-key", "sec-websocket-accept":
		return true
	default:
		return false
	}
}

func (c *wsH2Conn) readConnectResponse() (http.Header, error) {
	decoder := hpack.NewDecoder(4096, nil)
	respHeaders := make(http.Header)

	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			return nil, err
		}

		switch f := frame.(type) {
		case *http2.HeadersFrame:
			if f.StreamID == c.streamID {
				return c.processResponseHeaders(f, decoder, respHeaders)
			}
		case *http2.SettingsFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WriteSettingsAck()
				c.writeMu.Unlock()

				if err != nil {
					return nil, err
				}
			}

		case *http2.RSTStreamFrame:
			if f.StreamID == c.streamID {
				return nil, ErrH2StreamClosed
			}
		case *http2.GoAwayFrame:
			return nil, ErrH2GoAway

		case *http2.PingFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WritePing(true, f.Data)
				c.writeMu.Unlock()

				if err != nil {
					return nil, err
				}
			}
		}
	}
}

func (c *wsH2Conn) processResponseHeaders(
	f *http2.HeadersFrame,
	decoder *hpack.Decoder,
	headers http.Header,
) (http.Header, error) {
	fields, err := decoder.DecodeFull(f.HeaderBlockFragment())
	if err != nil {
		return nil, err
	}

	status := ""
	for _, field := range fields {
		if field.Name == ":status" {
			status = field.Value
			continue
		}

		if !strings.HasPrefix(field.Name, ":") {
			headers.Add(field.Name, field.Value)
		}
	}

	if status != "200" {
		return nil, ErrH2ConnectFailed
	}

	return headers, nil
}

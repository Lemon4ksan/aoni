// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
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

	"github.com/gorilla/websocket"
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

	wsFrameContinuation = FrameContinuation
	wsFrameText         = FrameText
	wsFrameBinary       = FrameBinary
	wsFrameClose        = FrameClose
	wsFramePing         = FramePing
	wsFramePong         = FramePong

	maxWebSocketFrameSize    = 16 * 1024 * 1024
	maxConsecutiveEmptyReads = 100
	h2DefaultMaxFrameSize    = 16 * 1024
	h2InitialWindowSize      = 65535
)

type wsGorillaConn struct {
	base   *websocket.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *wsGorillaConn) Read(b []byte) (int, error) {
	_, p, err := c.base.ReadMessage()
	if err != nil {
		return 0, err
	}

	return copy(b, p), nil
}

func (c *wsGorillaConn) Write(b []byte) (int, error) {
	err := c.base.WriteMessage(websocket.TextMessage, b)
	return len(b), err
}

func (c *wsGorillaConn) ReadMessage() (int, []byte, error) {
	return c.base.ReadMessage()
}

func (c *wsGorillaConn) WriteMessage(messageType int, data []byte) error {
	return c.base.WriteMessage(messageType, data)
}

func (c *wsGorillaConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		_ = c.base.Close()
	})

	return nil
}

func (c *wsGorillaConn) RawConn() *websocket.Conn { return c.base }
func (c *wsGorillaConn) UnderlyingConn() any      { return c.base.UnderlyingConn() }
func (c *wsGorillaConn) LocalAddr() net.Addr      { return c.base.LocalAddr() }
func (c *wsGorillaConn) RemoteAddr() net.Addr     { return c.base.RemoteAddr() }
func (c *wsGorillaConn) SetDeadline(t time.Time) error {
	if err := c.base.SetReadDeadline(t); err != nil {
		return err
	}

	return c.base.SetWriteDeadline(t)
}
func (c *wsGorillaConn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsGorillaConn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }
func (c *wsGorillaConn) CloseChan() <-chan struct{}         { return c.closed }

func wrapGorillaConn(conn *websocket.Conn) *wsGorillaConn {
	return &wsGorillaConn{base: conn, closed: make(chan struct{})}
}

// WrapGorillaConn wraps a gorilla websocket.Conn into a ws.Conn.
func WrapGorillaConn(conn *websocket.Conn) Conn {
	return wrapGorillaConn(conn)
}

// WrapRawConn wraps a net.Conn into a ws.Conn.
func WrapRawConn(conn net.Conn, isClient bool) Conn {
	return wrapRawConn(conn, isClient)
}

// Conn represents a thread-safe WebSocket connection contract extending net.Conn.
type Conn interface {
	net.Conn
	ReadMessage() (messageType int, payload []byte, err error)
	WriteMessage(messageType int, data []byte) error
	UnderlyingConn() any
	CloseChan() <-chan struct{}
}

type wsRawConn struct {
	base     net.Conn
	isClient bool
	reader   io.Reader
	closed   chan struct{}
	writeMu  chan struct{}
	once     sync.Once
}

func wrapRawConn(conn net.Conn, isClient bool) *wsRawConn {
	c := &wsRawConn{
		base:     conn,
		isClient: isClient,
		closed:   make(chan struct{}),
		writeMu:  make(chan struct{}, 1),
	}
	c.writeMu <- struct{}{}

	return c
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
		case wsFrameBinary, wsFrameText, wsFrameContinuation:
			c.reader = bytes.NewReader(payload)
			return nil
		case wsFrameClose:
			return io.EOF
		case wsFramePing:
			_ = c.writeFrame(wsFramePong, payload)
		case wsFramePong:
		}
	}

	return io.EOF
}

func (c *wsRawConn) Write(b []byte) (int, error) {
	<-c.writeMu
	defer func() { c.writeMu <- struct{}{} }()

	opcode := generic.Ternary(utf8.Valid(b), byte(wsFrameText), byte(wsFrameBinary))
	if err := c.writeFrame(opcode, b); err != nil {
		_ = c.Close()
		return 0, err
	}

	return len(b), nil
}

func (c *wsRawConn) ReadMessage() (int, []byte, error) {
	opcode, payload, err := c.readFrame()
	if err != nil {
		return 0, nil, err
	}

	return int(opcode), payload, nil
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

func (c *wsRawConn) readFrame() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.base, header); err != nil {
		return 0, nil, err
	}

	if (header[0] & 0x70) != 0 {
		return 0, nil, ErrReservedBitsSet
	}

	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0

	length, err := c.readFrameLength(header[1] & 0x7f)
	if err != nil {
		return 0, nil, err
	}

	if opcode >= wsFrameClose && length > 125 {
		return 0, nil, ErrControlFrameTooLarge
	}

	payload, err := c.readFramePayload(length, masked)
	if err != nil {
		return 0, nil, err
	}

	return opcode, payload, nil
}

func (c *wsRawConn) readFrameLength(basicLen byte) (uint64, error) {
	switch basicLen {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(c.base, extended); err != nil {
			return 0, err
		}

		return uint64(binary.BigEndian.Uint16(extended)), nil

	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(c.base, extended); err != nil {
			return 0, err
		}

		return binary.BigEndian.Uint64(extended), nil

	default:
		return uint64(basicLen), nil
	}
}

func (c *wsRawConn) readFramePayload(length uint64, masked bool) ([]byte, error) {
	if length > maxWebSocketFrameSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, length)
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.base, mask[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.base, payload); err != nil {
		return nil, err
	}

	if masked {
		applyFastMask(payload, mask)
	}

	return payload, nil
}

func (c *wsRawConn) writeFrame(opcode byte, payload []byte) error {
	if opcode >= wsFrameClose && len(payload) > 125 {
		return ErrControlFrameTooLarge
	}

	header := c.buildFrameHeader(opcode, len(payload))
	if c.isClient {
		return c.writeMaskedFrame(header, payload)
	}

	if _, err := c.base.Write(header); err != nil {
		return err
	}

	_, err := c.base.Write(payload)

	return err
}

func (c *wsRawConn) writeMaskedFrame(header, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}

	header = append(header, mask[:]...)

	masked := make([]byte, len(payload))
	copy(masked, payload)
	applyFastMask(masked, mask)

	if _, err := c.base.Write(header); err != nil {
		return err
	}

	_, err := c.base.Write(masked)

	return err
}

func (c *wsRawConn) buildFrameHeader(opcode byte, length int) []byte {
	header := []byte{0x80 | opcode, 0}
	if c.isClient {
		header[1] = 0x80
	}

	switch {
	case length < 126:
		header[1] |= byte(length)
	case length <= 0xffff:
		header[1] |= 126
		extended := make([]byte, 2)
		binary.BigEndian.PutUint16(extended, uint16(length))
		header = append(header, extended...)
	default:
		header[1] |= 127
		extended := make([]byte, 8)
		binary.BigEndian.PutUint64(extended, uint64(length))
		header = append(header, extended...)
	}

	return header
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

type wsH2Conn struct {
	base        net.Conn
	framer      *http2.Framer
	streamID    uint32
	readBuf     bytes.Buffer
	streamEnded bool
	readMu      sync.Mutex
	writeMu     sync.Mutex
	closed      chan struct{}
	once        sync.Once
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

	return wsFrameText, b[:n], nil
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

	return wrapRawConn(h2c, true), respHeaders, nil
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/miyako/generic"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// WebSocket frame opcodes.
const (
	wsFrameContinuation = 0
	wsFrameText         = 1
	wsFrameBinary       = 2
	wsFrameClose        = 8
	wsFramePing         = 9
	wsFramePong         = 10
)

// HTTP/2 Extended CONNECT constants.
const (
	h2DefaultMaxFrameSize = 16 * 1024
	h2InitialWindowSize   = 65535
)

// maxConsecutiveEmptyReads limits how many empty messages are skipped
// before treating the stream as exhausted.
const maxConsecutiveEmptyReads = 100

// WebSocketConn represents an active WebSocket connection.
// It extends the [net.Conn] interface, allowing the connection to be used with standard
// Go abstractions, while simultaneously providing direct access to reading/writing
// typed WebSocket messages (Text/Binary), receiving the low-level socket
// and monitoring channel closure.
type WebSocketConn interface {
	net.Conn
	// ReadMessage считывает следующее сообщение из соединения, возвращая его тип
	// (TextMessage или BinaryMessage) и полезную нагрузку.
	ReadMessage() (messageType int, p []byte, err error)
	// WriteMessage отправляет сообщение определенного типа (TextMessage или BinaryMessage).
	WriteMessage(messageType int, data []byte) error
	// UnderlyingConn возвращает низкоуровневый объект соединения (например, *websocket.Conn или http2 stream).
	UnderlyingConn() any
	// CloseChan возвращает канал, закрывающийся при разрыве соединения.
	CloseChan() <-chan struct{}
}

// wsGorillaConn adapts a [github.com/gorilla/websocket.Conn] to the [WebSocketConn] interface.
type wsGorillaConn struct {
	base   *websocket.Conn
	reader io.Reader
	closed chan struct{}
	once   sync.Once
}

// RawConn returns the raw pointer to the gorilla library's [websocket.Conn].
// This is necessary for integration with external socket managers,
// which require direct access to the gorilla/websocket API.
func (c *wsGorillaConn) RawConn() *websocket.Conn {
	return c.base
}

func (c *wsGorillaConn) Read(b []byte) (int, error) {
	for {
		if c.reader != nil {
			n, err := c.reader.Read(b)
			if err == io.EOF {
				c.reader = nil

				if n > 0 {
					return n, nil
				}

				continue
			}

			return n, err
		}

		msgType, r, err := c.base.NextReader()
		if err != nil {
			_ = c.Close()
			return 0, err
		}

		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			c.reader = r
			continue
		}
	}
}

func (c *wsGorillaConn) Write(b []byte) (int, error) {
	msgType := generic.Ternary(utf8.Valid(b), websocket.TextMessage, websocket.BinaryMessage)

	if err := c.base.WriteMessage(msgType, b); err != nil {
		_ = c.Close()
		return 0, err
	}

	return len(b), nil
}

// ReadMessage reads the next message from the connection, returning its type (TextMessage/BinaryMessage)
// and the message data.
func (c *wsGorillaConn) ReadMessage() (int, []byte, error) {
	return c.base.ReadMessage()
}

// WriteMessage writes a message to the connection with the specified type (TextMessage/BinaryMessage)
// and data.
func (c *wsGorillaConn) WriteMessage(messageType int, data []byte) error {
	return c.base.WriteMessage(messageType, data)
}

// UnderlyingConn returns the raw pointer to the gorilla library's [websocket.Conn].
// This is necessary for integration with external socket managers,
// which require direct access to the gorilla/websocket API.
func (c *wsGorillaConn) UnderlyingConn() any {
	return c.base
}

// Close closes the connection, releasing any resources.
// It is safe to call multiple times, and will only close once.
func (c *wsGorillaConn) Close() error {
	c.once.Do(func() { close(c.closed); c.base.Close() })
	return nil
}

func (c *wsGorillaConn) LocalAddr() net.Addr  { return c.base.LocalAddr() }
func (c *wsGorillaConn) RemoteAddr() net.Addr { return c.base.RemoteAddr() }
func (c *wsGorillaConn) SetDeadline(t time.Time) error {
	if err := c.base.SetReadDeadline(t); err != nil {
		return err
	}

	return c.base.SetWriteDeadline(t)
}
func (c *wsGorillaConn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsGorillaConn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }

// CloseChan returns a channel that is closed when the connection is closed.
func (c *wsGorillaConn) CloseChan() <-chan struct{} { return c.closed }

var _ WebSocketConn = (*wsGorillaConn)(nil)

func wrapGorillaConn(conn *websocket.Conn) *wsGorillaConn {
	return &wsGorillaConn{base: conn, closed: make(chan struct{})}
}

// wsRawConn implements [WebSocketConn] by manually reading and writing WebSocket
// frames over a raw TCP/TLS connection.
type wsRawConn struct {
	base     net.Conn
	isClient bool
	reader   io.Reader
	closed   chan struct{}
	writeMu  chan struct{}
	once     sync.Once
}

func (c *wsRawConn) Read(b []byte) (int, error) {
	for {
		if c.reader != nil {
			n, err := c.reader.Read(b)
			if err == io.EOF {
				c.reader = nil

				if n > 0 {
					return n, nil
				}

				continue
			}

			return n, err
		}

		for range maxConsecutiveEmptyReads {
			opcode, payload, err := c.readFrame()
			if err != nil {
				_ = c.Close()
				return 0, err
			}

			switch opcode {
			case wsFrameBinary, wsFrameText, wsFrameContinuation:
				c.reader = bytes.NewReader(payload)

				n, err := c.reader.Read(b)
				if err == io.EOF {
					c.reader = nil

					if n > 0 {
						return n, nil
					}

					continue
				}

				return n, err

			case wsFrameClose:
				_ = c.Close()
				return 0, io.EOF
			case wsFramePing:
				_ = c.writeFrame(wsFramePong, payload)
			case wsFramePong:
				// ignore
			}
		}

		_ = c.Close()

		return 0, io.EOF
	}
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

func (c *wsRawConn) UnderlyingConn() any {
	return c.base
}

func (c *wsRawConn) LocalAddr() net.Addr                { return c.base.LocalAddr() }
func (c *wsRawConn) RemoteAddr() net.Addr               { return c.base.RemoteAddr() }
func (c *wsRawConn) SetDeadline(t time.Time) error      { return c.base.SetDeadline(t) }
func (c *wsRawConn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsRawConn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }
func (c *wsRawConn) CloseChan() <-chan struct{}         { return c.closed }

func (c *wsRawConn) Close() error {
	c.once.Do(func() { close(c.closed); c.base.Close() })
	return nil
}

// maxWebSocketFrameSize is the maximum allowed WebSocket frame payload size (16 MiB).
const maxWebSocketFrameSize = 16 * 1024 * 1024

func (c *wsRawConn) readFrame() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.base, header); err != nil {
		return 0, nil, err
	}

	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)

	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(c.base, extended); err != nil {
			return 0, nil, err
		}

		length = uint64(binary.BigEndian.Uint16(extended))

	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(c.base, extended); err != nil {
			return 0, nil, err
		}

		length = binary.BigEndian.Uint64(extended)
	}

	if length > maxWebSocketFrameSize {
		return 0, nil, fmt.Errorf("aoni ws: frame payload too large: %d bytes (max %d)", length, maxWebSocketFrameSize)
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.base, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.base, payload); err != nil {
		return 0, nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	return opcode, payload, nil
}

func (c *wsRawConn) writeFrame(opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode, 0}
	if c.isClient {
		header[1] = 0x80
	}

	length := len(payload)
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

	if c.isClient {
		var mask [4]byte
		if _, err := rand.Read(mask[:]); err != nil {
			return err
		}

		header = append(header, mask[:]...)

		masked := make([]byte, len(payload))
		for i := range payload {
			masked[i] = payload[i] ^ mask[i%4]
		}

		payload = masked
	}

	if _, err := c.base.Write(header); err != nil {
		return err
	}

	_, err := c.base.Write(payload)

	return err
}

var _ WebSocketConn = (*wsRawConn)(nil)

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

// wsH2Conn wraps an HTTP/2 stream as a net.Conn for WebSocket over H2.
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

		switch f := frame.(type) {
		case *http2.DataFrame:
			if f.StreamID != c.streamID {
				continue
			}

			if data := f.Data(); len(data) > 0 {
				c.readBuf.Write(data)
				c.writeMu.Lock()

				err = c.framer.WriteWindowUpdate(0, uint32(len(data))) //nolint:gosec
				if err == nil {
					err = c.framer.WriteWindowUpdate(c.streamID, uint32(len(data))) //nolint:gosec
				}

				c.writeMu.Unlock()

				if err != nil {
					return 0, err
				}
			}

			if f.StreamEnded() {
				c.streamEnded = true

				if c.readBuf.Len() > 0 {
					n, _ := c.readBuf.Read(b)
					return n, nil
				}

				return 0, io.EOF
			}

		case *http2.SettingsFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WriteSettingsAck()
				c.writeMu.Unlock()

				if err != nil {
					return 0, err
				}
			}

		case *http2.PingFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WritePing(true, f.Data)
				c.writeMu.Unlock()

				if err != nil {
					return 0, err
				}
			}

		case *http2.RSTStreamFrame:
			if f.StreamID == c.streamID {
				return 0, io.EOF
			}
		case *http2.GoAwayFrame:
			return 0, io.EOF
		}
	}

	return c.readBuf.Read(b)
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

func (c *wsH2Conn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.writeMu.Lock()
		_ = c.framer.WriteData(c.streamID, true, nil)
		c.writeMu.Unlock()
	})

	return c.base.Close()
}

func (c *wsH2Conn) LocalAddr() net.Addr                { return c.base.LocalAddr() }
func (c *wsH2Conn) RemoteAddr() net.Addr               { return c.base.RemoteAddr() }
func (c *wsH2Conn) SetDeadline(t time.Time) error      { return c.base.SetDeadline(t) }
func (c *wsH2Conn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsH2Conn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }

var _ net.Conn = (*wsH2Conn)(nil)

// dialH2ExtendedConnect establishes a WebSocket connection over HTTP/2 Extended CONNECT (RFC 8441).
// The caller must ensure conn is a TLS connection with negotiated ALPN "h2".
func dialH2ExtendedConnect(ctx context.Context, conn net.Conn, targetURL, host string) (net.Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	h2c := &wsH2Conn{
		base:     conn,
		framer:   http2.NewFramer(conn, conn),
		streamID: 1,
		closed:   make(chan struct{}),
	}

	// Set a read deadline from context to prevent goroutine leak in clientPreface.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}

	// Send HTTP/2 client preface + SETTINGS
	if err := h2c.clientPreface(); err != nil {
		_ = conn.SetReadDeadline(time.Time{}) // clear deadline
		return nil, err
	}

	// Clear the deadline after preface completes.
	_ = conn.SetReadDeadline(time.Time{})

	// Parse target URL for CONNECT headers
	u, err := parseWSURL(targetURL)
	if err != nil {
		return nil, err
	}

	if host == "" {
		host = u.host
	}

	// Send CONNECT headers
	if err := h2c.writeConnectHeaders(u, host); err != nil {
		return nil, err
	}

	// Read response
	if err := h2c.readConnectResponse(); err != nil {
		return nil, err
	}

	return wrapRawConn(h2c, true), nil
}

func (c *wsH2Conn) clientPreface() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := c.base.Write([]byte(http2.ClientPreface)); err != nil {
		return err
	}

	if err := c.framer.WriteSettings(
		http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: h2InitialWindowSize},
	); err != nil {
		return err
	}

	// Read until we get the server's SETTINGS
	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			return err
		}

		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				enableConnect := false
				_ = f.ForeachSetting(func(s http2.Setting) error {
					if s.ID == http2.SettingEnableConnectProtocol && s.Val == 1 {
						enableConnect = true
					}

					return nil
				})

				if err := c.framer.WriteSettingsAck(); err != nil {
					return err
				}

				if !enableConnect {
					return errH2ConnectNotSupported
				}

				return nil
			}

		case *http2.WindowUpdateFrame:
			// ignore
		case *http2.PingFrame:
			if !f.IsAck() {
				if err := c.framer.WritePing(true, f.Data); err != nil {
					return err
				}
			}

		default:
			return errH2UnexpectedFrame
		}
	}
}

func (c *wsH2Conn) writeConnectHeaders(u *parsedURL, host string) error {
	var buf bytes.Buffer

	encoder := hpack.NewEncoder(&buf)

	scheme := "https"
	if u.scheme == "ws" {
		scheme = "http"
	}

	headers := []hpack.HeaderField{
		{Name: ":method", Value: "CONNECT"},
		{Name: ":scheme", Value: scheme},
		{Name: ":authority", Value: host},
		{Name: ":path", Value: u.Path},
		{Name: ":protocol", Value: "websocket"},
	}
	for _, h := range headers {
		if err := encoder.WriteField(h); err != nil {
			return err
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

func (c *wsH2Conn) readConnectResponse() error {
	decoder := hpack.NewDecoder(4096, nil)
	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			return err
		}

		switch f := frame.(type) {
		case *http2.HeadersFrame:
			if f.StreamID != c.streamID {
				continue
			}

			fields, err := decoder.DecodeFull(f.HeaderBlockFragment())
			if err != nil {
				return err
			}

			status := ""
			for _, field := range fields {
				if field.Name == ":status" {
					status = field.Value
					break
				}
			}

			if status != "200" {
				return errH2ConnectFailed
			}

			return nil

		case *http2.SettingsFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WriteSettingsAck()
				c.writeMu.Unlock()

				if err != nil {
					return err
				}
			}

		case *http2.RSTStreamFrame:
			if f.StreamID == c.streamID {
				return errH2StreamClosed
			}
		case *http2.GoAwayFrame:
			return errH2GoAway
		case *http2.PingFrame:
			if !f.IsAck() {
				c.writeMu.Lock()
				err = c.framer.WritePing(true, f.Data)
				c.writeMu.Unlock()

				if err != nil {
					return err
				}
			}
		}
	}
}

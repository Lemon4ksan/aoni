// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
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

// wsGorillaConn adapts a [github.com/gorilla/websocket.Conn] to the [net.Conn] interface.
// Read reassembles WebSocket messages into a continuous byte stream.
// Write sends binary WebSocket frames.
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
			_ = c.close()
			return 0, err
		}

		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			c.reader = r
			continue
		}
	}
}

func (c *wsGorillaConn) Write(b []byte) (int, error) {
	msgType := websocket.BinaryMessage
	if utf8.Valid(b) {
		msgType = websocket.TextMessage
	}

	if err := c.base.WriteMessage(msgType, b); err != nil {
		_ = c.close()
		return 0, err
	}

	return len(b), nil
}

func (c *wsGorillaConn) Close() error {
	return c.close()
}

func (c *wsGorillaConn) close() error {
	c.once.Do(func() { close(c.closed) })
	return c.base.Close()
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

var _ net.Conn = (*wsGorillaConn)(nil)

func wrapGorillaConn(conn *websocket.Conn) *wsGorillaConn {
	return &wsGorillaConn{base: conn, closed: make(chan struct{})}
}

// wsRawConn implements net.Conn by manually reading and writing WebSocket
// frames over a raw TCP/TLS connection. Used for HTTP/2 Extended CONNECT
// where gorilla/websocket cannot be used.
type wsRawConn struct {
	base     net.Conn
	isClient bool
	reader   io.Reader
	closed   chan struct{}
	writeMu  chan struct{}
	once     sync.Once
}

func (c *wsRawConn) Read(b []byte) (int, error) {
	if c.reader != nil {
		n, err := c.reader.Read(b)
		if err == io.EOF {
			c.reader = nil
			return n, io.EOF
		}

		return n, err
	}

	for range maxConsecutiveEmptyReads {
		opcode, payload, err := c.readFrame()
		if err != nil {
			_ = c.close()
			return 0, err
		}

		switch opcode {
		case wsFrameBinary, wsFrameText, wsFrameContinuation:
			c.reader = bytes.NewReader(payload)

			n, err := c.reader.Read(b)
			if err == io.EOF {
				c.reader = nil
				return n, io.EOF
			}

			return n, err

		case wsFrameClose:
			_ = c.close()
			return 0, io.EOF
		case wsFramePing:
			_ = c.writeFrame(wsFramePong, payload)
		case wsFramePong:
			// ignore
		}
	}

	_ = c.close()

	return 0, io.EOF
}

func (c *wsRawConn) Write(b []byte) (int, error) {
	<-c.writeMu
	defer func() { c.writeMu <- struct{}{} }()

	opcode := byte(wsFrameBinary)
	if utf8.Valid(b) {
		opcode = byte(wsFrameText)
	}

	if err := c.writeFrame(opcode, b); err != nil {
		_ = c.close()
		return 0, err
	}

	return len(b), nil
}

func (c *wsRawConn) Close() error                       { return c.close() }
func (c *wsRawConn) LocalAddr() net.Addr                { return c.base.LocalAddr() }
func (c *wsRawConn) RemoteAddr() net.Addr               { return c.base.RemoteAddr() }
func (c *wsRawConn) SetDeadline(t time.Time) error      { return c.base.SetDeadline(t) }
func (c *wsRawConn) SetReadDeadline(t time.Time) error  { return c.base.SetReadDeadline(t) }
func (c *wsRawConn) SetWriteDeadline(t time.Time) error { return c.base.SetWriteDeadline(t) }
func (c *wsRawConn) CloseChan() <-chan struct{}         { return c.closed }

func (c *wsRawConn) close() error {
	c.once.Do(func() { close(c.closed) })
	return c.base.Close()
}

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

var _ net.Conn = (*wsRawConn)(nil)

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
	base     net.Conn
	framer   *http2.Framer
	streamID uint32
	readBuf  bytes.Buffer
	readMu   sync.Mutex
	writeMu  sync.Mutex
	closed   chan struct{}
	once     sync.Once
}

func (c *wsH2Conn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for c.readBuf.Len() == 0 {
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

			if f.StreamEnded() && c.readBuf.Len() == 0 {
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

	// Send HTTP/2 client preface + SETTINGS
	if err := h2c.clientPreface(); err != nil {
		return nil, err
	}

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

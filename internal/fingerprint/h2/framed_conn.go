// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"sync"
)

var h2Preface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

var h2BufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

type SettingsDTO struct {
	HeaderTableSize      uint32
	EnablePush           uint32
	MaxConcurrentStreams uint32
	InitialWindowSize    uint32
	MaxFrameSize         uint32
	MaxHeaderListSize    uint32
	ConnectionFlow       uint32
	InitialStreamID      uint32
	PriorityStreamDep    uint32
	PriorityExclusive    bool
	PriorityWeight       uint8
}

type FramedConn struct {
	net.Conn
	Settings       SettingsDTO
	OrderedKeys    []string
	mu             sync.Mutex
	prefaceSent    bool
	prefaceWritten bool
}

// WrapConn wraps conn with HTTP/2 frame building and SETTINGS patching.
func WrapConn(conn net.Conn, settings SettingsDTO, orderedKeys []string) net.Conn {
	return &FramedConn{
		Conn:        conn,
		Settings:    settings,
		OrderedKeys: orderedKeys,
	}
}

// ConnectionState delegates to the underlying connection's ConnectionState if available.
func (c *FramedConn) ConnectionState() tls.ConnectionState {
	if cs, ok := c.Conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		return cs.ConnectionState()
	}

	return tls.ConnectionState{}
}

// Handshake delegates to the underlying connection's Handshake if available.
func (c *FramedConn) Handshake() error {
	if hs, ok := c.Conn.(interface{ Handshake() error }); ok {
		return hs.Handshake()
	}

	return nil
}

func (c *FramedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.prefaceSent {
		return c.Conn.Write(b)
	}

	if len(b) < 24 || !bytes.Equal(b[:24], h2Preface) {
		return c.Conn.Write(b)
	}

	c.prefaceSent = true
	if len(b) < 33 {
		return c.Conn.Write(b)
	}

	preface := b[:24]

	settingsFrame := b[24:]
	if len(settingsFrame) < 9 {
		return c.Conn.Write(b)
	}

	payloadLen := int(settingsFrame[0])<<16 | int(settingsFrame[1])<<8 | int(settingsFrame[2])
	if len(settingsFrame) < 9+payloadLen {
		return c.Conn.Write(b)
	}

	replacement := c.buildSettingsFrame()
	if c.Settings.ConnectionFlow > 65535 {
		increment := c.Settings.ConnectionFlow - 65535
		windowUpdate := c.buildWindowUpdateFrame(increment)
		replacement = append(replacement, windowUpdate...)
	}

	remaining := settingsFrame[9+payloadLen:]

	var newRemaining []byte

	if len(remaining) >= 9 && remaining[3] == 0x2 {
		frameLen := 9 + int(remaining[0])<<16 | int(remaining[1])<<8 | int(remaining[2])
		if len(remaining) >= frameLen {
			newRemaining = c.BuildPriorityFrame(remaining[:frameLen])
			remaining = remaining[frameLen:]
		}
	}

	buf := h2BufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Grow(len(preface) + len(replacement) + len(newRemaining) + len(remaining))

	buf.Write(preface)
	buf.Write(replacement)

	if len(newRemaining) > 0 {
		buf.Write(newRemaining)
	}

	buf.Write(remaining)

	written, err := c.Conn.Write(buf.Bytes())
	h2BufferPool.Put(buf)

	if err != nil {
		return written, err
	}

	c.prefaceWritten = true

	return len(b), nil
}

// buildWindowUpdateFrame constructs an HTTP/2 WINDOW_UPDATE frame (Type 0x8) with increment bytes.
func (c *FramedConn) buildWindowUpdateFrame(increment uint32) []byte {
	frame := make([]byte, 13)
	frame[0], frame[1], frame[2] = 0x0, 0x0, 0x4
	frame[3] = 0x8 // WINDOW_UPDATE
	frame[4] = 0x0

	binary.BigEndian.PutUint32(frame[9:13], increment&0x7FFFFFFF)

	return frame
}

// buildSettingsFrame serializes active SettingsDTO parameters into an HTTP/2 SETTINGS frame (Type 0x4).
func (c *FramedConn) buildSettingsFrame() []byte {
	var payload bytes.Buffer

	if c.Settings.HeaderTableSize > 0 {
		writeSettingEntry(&payload, 0x1, c.Settings.HeaderTableSize)
	}

	if c.Settings.EnablePush > 0 || c.Settings.InitialWindowSize > 0 {
		writeSettingEntry(&payload, 0x2, c.Settings.EnablePush)
	}

	if c.Settings.MaxConcurrentStreams > 0 {
		writeSettingEntry(&payload, 0x3, c.Settings.MaxConcurrentStreams)
	}

	if c.Settings.InitialWindowSize > 0 {
		writeSettingEntry(&payload, 0x4, c.Settings.InitialWindowSize)
	}

	if c.Settings.MaxFrameSize > 0 {
		writeSettingEntry(&payload, 0x5, c.Settings.MaxFrameSize)
	}

	if c.Settings.MaxHeaderListSize > 0 {
		writeSettingEntry(&payload, 0x6, c.Settings.MaxHeaderListSize)
	}

	frame := make([]byte, 9+payload.Len())
	frame[0] = byte(payload.Len() >> 16) //nolint:gosec
	frame[1] = byte(payload.Len() >> 8)  //nolint:gosec
	frame[2] = byte(payload.Len())       //nolint:gosec
	frame[3] = 0x4                       // SETTINGS

	copy(frame[9:], payload.Bytes())

	return frame
}

// BuildPriorityFrame constructs an HTTP/2 PRIORITY frame (Type 0x2) based on configured priority weights.
func (c *FramedConn) BuildPriorityFrame(original []byte) []byte {
	if len(original) < 9 {
		return nil
	}

	payload := make([]byte, 5)
	streamDep := c.Settings.PriorityStreamDep

	binary.BigEndian.PutUint32(payload[0:4], streamDep&0x7FFFFFFF)

	if c.Settings.PriorityExclusive {
		payload[0] |= 0x80
	}

	payload[4] = c.Settings.PriorityWeight

	frame := make([]byte, 14)
	frame[0], frame[1], frame[2] = 0x0, 0x0, 0x5
	frame[3] = 0x2 // PRIORITY
	frame[4] = 0x0

	copy(frame[5:9], original[5:9])
	copy(frame[9:], payload)

	return frame
}

// writeSettingEntry encodes a 2-byte setting ID and 4-byte setting value into w.
func writeSettingEntry(w io.Writer, id uint16, value uint32) {
	var buf [6]byte

	buf[0] = byte(id >> 8)     //nolint:gosec
	buf[1] = byte(id)          //nolint:gosec
	buf[2] = byte(value >> 24) //nolint:gosec
	buf[3] = byte(value >> 16) //nolint:gosec
	buf[4] = byte(value >> 8)  //nolint:gosec
	buf[5] = byte(value)       //nolint:gosec

	_, _ = w.Write(buf[:])
}

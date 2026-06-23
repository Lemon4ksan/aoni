// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"sync"
)

// h2framedConn wraps a [net.Conn] and intercepts the HTTP/2 client preface
// to replace the SETTINGS frame and PRIORITY frame with browser-specific values.
// This enables full HTTP/2 fingerprint impersonation matching the TLS profile.
type h2framedConn struct {
	net.Conn
	settings       HTTP2Settings
	mu             sync.Mutex
	prefaceSent    bool
	prefaceWritten bool
}

func (c *h2framedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.prefaceSent {
		return c.Conn.Write(b)
	}

	// Detect HTTP/2 client preface: "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n" (24 bytes)
	if len(b) < 24 || !bytes.Equal(b[:24], []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")) {
		return c.Conn.Write(b)
	}

	c.prefaceSent = true

	// Client preface is 24 bytes + SETTINGS frame (9-byte header + payload)
	if len(b) < 33 {
		// Incomplete preface, write as-is
		return c.Conn.Write(b)
	}

	preface := b[:24]
	settingsFrame := b[24:]

	// Parse the original SETTINGS frame
	if len(settingsFrame) < 9 {
		return c.Conn.Write(b)
	}

	payloadLen := int(settingsFrame[0])<<16 | int(settingsFrame[1])<<8 | int(settingsFrame[2])
	// frameType := settingsFrame[3] // should be 0x4 (SETTINGS)
	// flags := settingsFrame[4]
	// streamID := binary.BigEndian.Uint32(settingsFrame[5:9]) & 0x7FFFFFFF

	if len(settingsFrame) < 9+payloadLen {
		return c.Conn.Write(b)
	}

	// Build replacement SETTINGS frame with browser-specific values
	replacement := c.buildSettingsFrame()

	// Check if there's a PRIORITY frame after SETTINGS (Firefox sends one)
	remaining := settingsFrame[9+payloadLen:]

	var newRemaining []byte

	if len(remaining) >= 9 {
		nextFrameType := remaining[3]
		if nextFrameType == 0x2 { // PRIORITY frame
			newRemaining = c.buildPriorityFrame(
				remaining[:9+int(remaining[0])<<16|int(remaining[1])<<8|int(remaining[2])],
			)
			remaining = remaining[9+int(remaining[0])<<16|int(remaining[1])<<8|int(remaining[2]):]
		}
	}

	// Assemble: preface + replacement settings + modified remaining + untouched remaining
	result := make([]byte, 0, len(b)+64)
	result = append(result, preface...)

	result = append(result, replacement...)
	if len(newRemaining) > 0 {
		result = append(result, newRemaining...)
	}

	result = append(result, remaining...)
	result = append(result, b[len(b):]...) // any trailing bytes (none expected, but safe)

	written, err := c.Conn.Write(result)
	if err != nil {
		return written, err
	}

	c.prefaceWritten = true

	return len(b), nil
}

// buildSettingsFrame constructs an HTTP/2 SETTINGS frame with browser-specific values.
func (c *h2framedConn) buildSettingsFrame() []byte {
	var payload bytes.Buffer

	if c.settings.HeaderTableSize > 0 {
		writeSettingEntry(&payload, 0x1, c.settings.HeaderTableSize)
	}

	if c.settings.EnablePush > 0 || c.settings.MaxConcurrentStreams > 0 ||
		c.settings.InitialWindowSize > 0 || c.settings.MaxFrameSize > 0 ||
		c.settings.MaxHeaderListSize > 0 {
		// Chrome sends ENABLE_PUSH=0
		if c.settings.EnablePush > 0 || c.settings.MaxConcurrentStreams > 0 ||
			c.settings.InitialWindowSize > 0 || c.settings.MaxFrameSize > 0 ||
			c.settings.MaxHeaderListSize > 0 {
			writeSettingEntry(&payload, 0x2, c.settings.EnablePush)
		}

		if c.settings.MaxConcurrentStreams > 0 {
			writeSettingEntry(&payload, 0x3, c.settings.MaxConcurrentStreams)
		}

		if c.settings.InitialWindowSize > 0 {
			writeSettingEntry(&payload, 0x4, c.settings.InitialWindowSize)
		}

		if c.settings.MaxFrameSize > 0 {
			writeSettingEntry(&payload, 0x5, c.settings.MaxFrameSize)
		}

		if c.settings.MaxHeaderListSize > 0 {
			writeSettingEntry(&payload, 0x6, c.settings.MaxHeaderListSize)
		}
	}

	frame := make([]byte, 9+payload.Len())
	// Length (3 bytes)
	frame[0] = byte(payload.Len() >> 16) //nolint:gosec
	frame[1] = byte(payload.Len() >> 8)  //nolint:gosec
	frame[2] = byte(payload.Len())       //nolint:gosec
	// Type: SETTINGS (0x4)
	frame[3] = 0x4
	// Flags: none
	frame[4] = 0x0
	// Stream ID: 0
	frame[5] = 0x0
	frame[6] = 0x0
	frame[7] = 0x0
	frame[8] = 0x0

	copy(frame[9:], payload.Bytes())

	return frame
}

// buildPriorityFrame constructs a PRIORITY frame matching the browser's pattern.
func (c *h2framedConn) buildPriorityFrame(original []byte) []byte {
	if len(original) < 9 {
		return nil
	}

	// PRIORITY frame: 5-byte payload (stream dependency + weight)
	// Reconstruct with browser-specific values
	payload := make([]byte, 5)

	streamDep := c.settings.PriorityStreamDep
	if streamDep == 0 {
		streamDep = 0
	}

	binary.BigEndian.PutUint32(payload[0:4], streamDep&0x7FFFFFFF)

	if c.settings.PriorityExclusive {
		payload[0] |= 0x80
	}

	payload[4] = c.settings.PriorityWeight

	frame := make([]byte, 9+5)
	// Length: 5
	frame[0] = 0x0
	frame[1] = 0x0
	frame[2] = 0x5
	// Type: PRIORITY (0x2)
	frame[3] = 0x2
	// Flags: 0
	frame[4] = 0x0
	// Stream ID from original
	copy(frame[5:9], original[5:9])
	copy(frame[9:], payload)

	return frame
}

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

// H2FramedTransport wraps an *http.Transport to apply HTTP/2 frame impersonation.
// When DialTLSContext is called, the returned connection is wrapped in [h2framedConn]
// so that the initial SETTINGS and PRIORITY frames match the target browser fingerprint.
type H2FramedTransport struct {
	*http.Transport
	settings HTTP2Settings
}

// NewH2FramedTransport creates an [H2FramedTransport] from an existing transport
// and HTTP/2 settings. The transport's DialTLSContext is replaced to wrap connections
// with browser-specific HTTP/2 frame injection.
func NewH2FramedTransport(base *http.Transport, settings HTTP2Settings) *H2FramedTransport {
	ft := &H2FramedTransport{
		Transport: base,
		settings:  settings,
	}

	if base != nil {
		prevDialTLS := base.DialTLSContext
		ft.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var (
				conn net.Conn
				err  error
			)

			if prevDialTLS != nil {
				conn, err = prevDialTLS(ctx, network, addr)
			} else {
				conn, err = (&net.Dialer{}).DialContext(ctx, network, addr)
			}

			if err != nil {
				return nil, err
			}

			return &h2framedConn{
				Conn:     conn,
				settings: settings,
			}, nil
		}
	}

	return ft
}

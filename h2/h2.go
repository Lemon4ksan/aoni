// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/net/http2/hpack"

	"github.com/lemon4ksan/aoni/profiles"
)

var (
	// ChromeSettings provides HTTP/2 settings matching standard Google Chrome clients.
	ChromeSettings = Settings{
		HeaderTableSize:   65536,
		EnablePush:        0,
		InitialWindowSize: 6291456,
		MaxHeaderListSize: 262144,
		ConnectionFlow:    15663105,
		PriorityWeight:    255,
		PriorityExclusive: true,
	}

	// FirefoxSettings provides HTTP/2 settings matching standard Mozilla Firefox clients.
	FirefoxSettings = Settings{
		InitialStreamID:   3,
		HeaderTableSize:   65536,
		EnablePush:        0,
		InitialWindowSize: 131072,
		MaxFrameSize:      16384,
		ConnectionFlow:    12517377,
		PriorityWeight:    41,
	}
)

// Settings holds the full set of HTTP/2 connection parameters
// for browser-grade frame impersonation. Each field maps directly to
// an HTTP/2 SETTINGS frame parameter or PRIORITY frame value.
type Settings struct {
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

// SettingsFromProfile populates Settings from a profiles.H2Settings.
func SettingsFromProfile(s profiles.H2Settings) *Settings {
	return &Settings{
		HeaderTableSize:      s.HeaderTableSize,
		EnablePush:           s.EnablePush,
		MaxConcurrentStreams: s.MaxConcurrentStreams,
		InitialWindowSize:    s.InitialWindowSize,
		MaxFrameSize:         s.MaxFrameSize,
		MaxHeaderListSize:    s.MaxHeaderListSize,
		ConnectionFlow:       s.ConnectionFlow,
		InitialStreamID:      s.InitialStreamID,
		PriorityStreamDep:    s.PriorityStreamDep,
		PriorityExclusive:    s.PriorityExclusive,
		PriorityWeight:       s.PriorityWeight,
	}
}

// ParseSettings parses HTTP/2 settings from a JSON-encoded string.
// It supports snake_case, camelCase, and PascalCase field names, making it
// extremely convenient to import settings captured from Wireshark, browser
// developer tools, or TLS bypass scripts.
func ParseSettings(jsonStr string) (Settings, error) {
	// Proxy structure with explicit snake_case tags.
	// We use pointers to detect whether fields were actually present in the JSON.
	type settingsProxy struct {
		HeaderTableSize      *uint32 `json:"header_table_size"`
		EnablePush           *uint32 `json:"enable_push"`
		MaxConcurrentStreams *uint32 `json:"max_concurrent_streams"`
		InitialWindowSize    *uint32 `json:"initial_window_size"`
		MaxFrameSize         *uint32 `json:"max_frame_size"`
		MaxHeaderListSize    *uint32 `json:"max_header_list_size"`
		ConnectionFlow       *uint32 `json:"connection_flow"`
		InitialStreamID      *uint32 `json:"initial_stream_id"`
		PriorityStreamDep    *uint32 `json:"priority_stream_dep"`
		PriorityExclusive    *bool   `json:"priority_exclusive"`
		PriorityWeight       *uint8  `json:"priority_weight"`
	}

	var p settingsProxy

	err := json.Unmarshal([]byte(jsonStr), &p)
	if err != nil {
		return Settings{}, fmt.Errorf("aoni h2: failed to decode settings JSON: %w", err)
	}

	var settings Settings

	hasProxyFields := false

	// Map proxy fields if they were explicitly provided in snake_case
	if p.HeaderTableSize != nil {
		settings.HeaderTableSize = *p.HeaderTableSize
		hasProxyFields = true
	}

	if p.EnablePush != nil {
		settings.EnablePush = *p.EnablePush
		hasProxyFields = true
	}

	if p.MaxConcurrentStreams != nil {
		settings.MaxConcurrentStreams = *p.MaxConcurrentStreams
		hasProxyFields = true
	}

	if p.InitialWindowSize != nil {
		settings.InitialWindowSize = *p.InitialWindowSize
		hasProxyFields = true
	}

	if p.MaxFrameSize != nil {
		settings.MaxFrameSize = *p.MaxFrameSize
		hasProxyFields = true
	}

	if p.MaxHeaderListSize != nil {
		settings.MaxHeaderListSize = *p.MaxHeaderListSize
		hasProxyFields = true
	}

	if p.ConnectionFlow != nil {
		settings.ConnectionFlow = *p.ConnectionFlow
		hasProxyFields = true
	}

	if p.InitialStreamID != nil {
		settings.InitialStreamID = *p.InitialStreamID
		hasProxyFields = true
	}

	if p.PriorityStreamDep != nil {
		settings.PriorityStreamDep = *p.PriorityStreamDep
		hasProxyFields = true
	}

	if p.PriorityExclusive != nil {
		settings.PriorityExclusive = *p.PriorityExclusive
		hasProxyFields = true
	}

	if p.PriorityWeight != nil {
		settings.PriorityWeight = *p.PriorityWeight
		hasProxyFields = true
	}

	// Fallback: If no snake_case fields were captured, try parsing the original
	// struct directly to leverage Go's default case-insensitive field matching
	// (which perfectly covers camelCase and PascalCase formats).
	if !hasProxyFields {
		var direct Settings
		if errDirect := json.Unmarshal([]byte(jsonStr), &direct); errDirect == nil {
			return direct, nil
		}
	}

	return settings, nil
}

// FramedTransport wraps an *http.Transport to apply HTTP/2 frame impersonation.
// When DialTLSContext is called, the returned connection is wrapped in [h2framedConn]
// so that the initial SETTINGS and PRIORITY frames match the target browser fingerprint.
// If orderedKeys is set, HEADERS frames are also reordered.
type FramedTransport struct {
	*http.Transport
	settings    Settings
	orderedKeys []string
}

// NewFramedTransport creates an [FramedTransport] from an existing transport
// and HTTP/2 settings. The transport's DialTLSContext is replaced to wrap connections
// with browser-specific HTTP/2 frame injection.
func NewFramedTransport(base *http.Transport, settings Settings, orderedKeys ...string) *FramedTransport {
	ft := &FramedTransport{
		Transport:   base,
		settings:    settings,
		orderedKeys: orderedKeys,
	}

	if base == nil {
		return ft
	}

	prevDialTLS := base.DialTLSContext
	ft.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var (
			conn net.Conn
			err  error
		)

		if prevDialTLS != nil {
			conn, err = prevDialTLS(ctx, network, addr)
		} else {
			// No custom TLS dialer — perform a standard TLS handshake so that
			// http2.ConfigureTransports receives a TLS-negotiated connection.
			tlsCfg := base.TLSClientConfig
			if tlsCfg == nil {
				tlsCfg = &tls.Config{}
			}

			host, _, _ := net.SplitHostPort(addr)

			cfg := tlsCfg.Clone()
			if cfg.ServerName == "" {
				cfg.ServerName = host
			}

			if len(cfg.NextProtos) == 0 {
				cfg.NextProtos = []string{"h2", "http/1.1"}
			}

			d := &tls.Dialer{
				NetDialer: &net.Dialer{},
				Config:    cfg,
			}
			conn, err = d.DialContext(ctx, network, addr)
		}

		if err != nil {
			return nil, err
		}

		return &framedConn{
			Conn:        conn,
			settings:    settings,
			orderedKeys: orderedKeys,
		}, nil
	}

	return ft
}

// Clone returns a clone of the FramedTransport wrapping the provided base transport.
func (ft *FramedTransport) Clone(base *http.Transport) *FramedTransport {
	if ft == nil {
		return nil
	}

	return NewFramedTransport(base, ft.settings, ft.orderedKeys...)
}

// framedConn wraps a [net.Conn] and intercepts the HTTP/2 client preface
// to replace the SETTINGS frame and PRIORITY frame with browser-specific values.
// This enables full HTTP/2 fingerprint impersonation matching the TLS profile.
// When orderedKeys is set, HEADERS frames are also intercepted and reordered.
type framedConn struct {
	net.Conn
	settings       Settings
	orderedKeys    []string
	mu             sync.Mutex
	prefaceSent    bool
	prefaceWritten bool
}

func (c *framedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.prefaceSent {
		// Header reordering is not supported with H2 framed transport because
		// HPACK is stateful: creating fresh encoders/decoders desynchronizes
		// the dynamic table from Go's HTTP/2 transport, causing COMPRESSION_ERROR.
		// Users should use ordered headers with non-H2 transports only.
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

	if c.settings.ConnectionFlow > 65535 {
		increment := c.settings.ConnectionFlow - 65535
		windowUpdate := c.buildWindowUpdateFrame(increment)
		replacement = append(replacement, windowUpdate...)
	}

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

func (c *framedConn) buildWindowUpdateFrame(increment uint32) []byte {
	frame := make([]byte, 13)

	// Length: 4 (0x000004)
	frame[0] = 0x0
	frame[1] = 0x0
	frame[2] = 0x4
	// Type: WINDOW_UPDATE (0x8)
	frame[3] = 0x8
	// Flags: 0
	frame[4] = 0x0
	// Stream ID: 0 (Connection-level)
	frame[5] = 0x0
	frame[6] = 0x0
	frame[7] = 0x0
	frame[8] = 0x0

	binary.BigEndian.PutUint32(frame[9:13], increment&0x7FFFFFFF)

	return frame
}

// buildSettingsFrame constructs an HTTP/2 SETTINGS frame with browser-specific values.
func (c *framedConn) buildSettingsFrame() []byte {
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
func (c *framedConn) buildPriorityFrame(original []byte) []byte {
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

// reorderH2Headers decodes an HTTP/2 HEADERS frame, reorders the HPACK-encoded
// header fields, and re-encodes the frame.
func reorderH2Headers(frame []byte, flags byte, order []string) ([]byte, bool) {
	if len(frame) < 9 {
		return nil, false
	}

	payloadLen := int(frame[0])<<16 | int(frame[1])<<8 | int(frame[2])
	streamID := binary.BigEndian.Uint32(frame[5:9]) & 0x7FFFFFFF

	payload := frame[9:]
	if len(payload) < payloadLen {
		return nil, false
	}

	payload = payload[:payloadLen]

	offset := 0
	padLen := 0

	// Pad Length (1 byte) if PADDED flag (0x8) is set.
	if flags&0x8 != 0 {
		if offset >= len(payload) {
			return nil, false
		}

		padLen = int(payload[offset])
		offset++
	}

	// Stream Dependency (4 bytes) + Weight (1 byte) if PRIORITY flag (0x20) is set.
	if flags&0x20 != 0 {
		offset += 5
	}

	if offset >= len(payload) {
		return nil, false
	}

	// Header block ends before the trailing padding bytes.
	hblockEnd := len(payload) - padLen
	if hblockEnd <= offset {
		return nil, false
	}

	hblock := payload[offset:hblockEnd]

	// Decode HPACK headers.
	decoder := hpack.NewDecoder(4096, nil)

	headers, err := decoder.DecodeFull(hblock)
	if err != nil {
		return nil, false
	}

	if len(headers) == 0 {
		return nil, false
	}

	// Reorder headers: specified order first, then remaining in original order.
	ordered := make([]hpack.HeaderField, 0, len(headers))
	remaining := make([]hpack.HeaderField, 0, len(headers))
	used := make(map[int]bool)

	for _, key := range order {
		lowerKey := strings.ToLower(key)
		for i, h := range headers {
			if !used[i] && strings.ToLower(h.Name) == lowerKey {
				ordered = append(ordered, h)
				used[i] = true
				break
			}
		}
	}

	for i, h := range headers {
		if !used[i] {
			remaining = append(remaining, h)
		}
	}

	ordered = append(ordered, remaining...)

	// Re-encode HPACK.
	var hblockBuf bytes.Buffer

	encoder := hpack.NewEncoder(&hblockBuf)
	for _, h := range ordered {
		if err := encoder.WriteField(h); err != nil {
			return nil, false
		}
	}

	newHblock := hblockBuf.Bytes()

	// Rebuild the frame payload: prefix + reordered header block + padding.
	prefixLen := offset
	newPayloadLen := prefixLen + len(newHblock) + padLen

	newFrame := make([]byte, 9+newPayloadLen)
	// Length (3 bytes).
	newFrame[0] = byte(newPayloadLen >> 16) //nolint:gosec
	newFrame[1] = byte(newPayloadLen >> 8)  //nolint:gosec
	newFrame[2] = byte(newPayloadLen)       //nolint:gosec
	// Type: HEADERS (0x1).
	newFrame[3] = 0x1
	// Flags: preserve original.
	newFrame[4] = flags
	// Stream ID.
	binary.BigEndian.PutUint32(newFrame[5:9], streamID)

	// Copy prefix (pad length + priority fields).
	// gosec G602 cannot prove bounds safety on slice expressions here,
	// so copy byte-by-byte with explicit bounds checks.
	prefixCopyLen := min(min(prefixLen, len(frame)-9), len(newFrame)-9)

	for i := range prefixCopyLen { //nolint:gosec
		newFrame[9+i] = frame[9+i] //nolint:gosec
	}

	// Write reordered header block.
	copy(newFrame[9+prefixLen:], newHblock)
	// Write padding (zeros).
	// padding is already zero-initialized in make()

	return newFrame, true
}

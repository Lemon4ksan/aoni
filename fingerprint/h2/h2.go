// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h2 implements HTTP/2 SETTINGS and PRIORITY frame impersonation and HPACK header field reordering.
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
	"sync"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

var h2BufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

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

// Settings holds the full set of HTTP/2 connection parameters for browser-grade frame impersonation.
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

// SettingsFromProfile populates [Settings] from a [profiles.H2Settings].
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

// ParseSettings parses HTTP/2 settings from a JSON-encoded string.
func ParseSettings(jsonStr string) (Settings, error) {
	var p settingsProxy
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return Settings{}, fmt.Errorf("aoni h2: failed to decode settings JSON: %w", err)
	}

	var settings Settings

	hasProxyFields := mapProxyFields(&p, &settings)

	if !hasProxyFields {
		var direct Settings
		if errDirect := json.Unmarshal([]byte(jsonStr), &direct); errDirect == nil {
			return direct, nil
		}
	}

	return settings, nil
}

func mapProxyFields(p *settingsProxy, s *Settings) bool {
	hasFields := false

	if p.HeaderTableSize != nil {
		s.HeaderTableSize = *p.HeaderTableSize
		hasFields = true
	}

	if p.EnablePush != nil {
		s.EnablePush = *p.EnablePush
		hasFields = true
	}

	if p.MaxConcurrentStreams != nil {
		s.MaxConcurrentStreams = *p.MaxConcurrentStreams
		hasFields = true
	}

	if p.InitialWindowSize != nil {
		s.InitialWindowSize = *p.InitialWindowSize
		hasFields = true
	}

	if p.MaxFrameSize != nil {
		s.MaxFrameSize = *p.MaxFrameSize
		hasFields = true
	}

	if p.MaxHeaderListSize != nil {
		s.MaxHeaderListSize = *p.MaxHeaderListSize
		hasFields = true
	}

	if p.ConnectionFlow != nil {
		s.ConnectionFlow = *p.ConnectionFlow
		hasFields = true
	}

	if p.InitialStreamID != nil {
		s.InitialStreamID = *p.InitialStreamID
		hasFields = true
	}

	if p.PriorityStreamDep != nil {
		s.PriorityStreamDep = *p.PriorityStreamDep
		hasFields = true
	}

	if p.PriorityExclusive != nil {
		s.PriorityExclusive = *p.PriorityExclusive
		hasFields = true
	}

	if p.PriorityWeight != nil {
		s.PriorityWeight = *p.PriorityWeight
		hasFields = true
	}

	return hasFields
}

// FramedTransport wraps an [*http.Transport] to inject browser-grade HTTP/2 frame impersonation.
type FramedTransport struct {
	*http.Transport
	settings    Settings
	orderedKeys []string
}

// NewFramedTransport creates a [FramedTransport] wrapping base with custom HTTP/2 settings and header ordering rules.
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

			d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
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

// Clone creates an independent copy of [FramedTransport] wrapping the provided base transport.
func (ft *FramedTransport) Clone(base *http.Transport) *FramedTransport {
	if ft == nil {
		return nil
	}

	return NewFramedTransport(base, ft.settings, ft.orderedKeys...)
}

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
		return c.Conn.Write(b)
	}

	const h2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	if len(b) < 24 || !bytes.Equal(b[:24], []byte(h2Preface)) {
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
	if c.settings.ConnectionFlow > 65535 {
		increment := c.settings.ConnectionFlow - 65535
		windowUpdate := c.buildWindowUpdateFrame(increment)
		replacement = append(replacement, windowUpdate...)
	}

	remaining := settingsFrame[9+payloadLen:]

	var newRemaining []byte

	if len(remaining) >= 9 && remaining[3] == 0x2 {
		frameLen := 9 + int(remaining[0])<<16 | int(remaining[1])<<8 | int(remaining[2])
		if len(remaining) >= frameLen {
			newRemaining = c.buildPriorityFrame(remaining[:frameLen])
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

func (c *framedConn) buildWindowUpdateFrame(increment uint32) []byte {
	frame := make([]byte, 13)
	frame[0], frame[1], frame[2] = 0x0, 0x0, 0x4
	frame[3] = 0x8 // WINDOW_UPDATE
	frame[4] = 0x0

	binary.BigEndian.PutUint32(frame[9:13], increment&0x7FFFFFFF)

	return frame
}

func (c *framedConn) buildSettingsFrame() []byte {
	var payload bytes.Buffer

	if c.settings.HeaderTableSize > 0 {
		writeSettingEntry(&payload, 0x1, c.settings.HeaderTableSize)
	}

	if c.settings.EnablePush > 0 || c.settings.InitialWindowSize > 0 {
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

	frame := make([]byte, 9+payload.Len())
	frame[0] = byte(payload.Len() >> 16) //nolint:gosec
	frame[1] = byte(payload.Len() >> 8)  //nolint:gosec
	frame[2] = byte(payload.Len())       //nolint:gosec
	frame[3] = 0x4                       // SETTINGS

	copy(frame[9:], payload.Bytes())

	return frame
}

func (c *framedConn) buildPriorityFrame(original []byte) []byte {
	if len(original) < 9 {
		return nil
	}

	payload := make([]byte, 5)
	streamDep := c.settings.PriorityStreamDep

	binary.BigEndian.PutUint32(payload[0:4], streamDep&0x7FFFFFFF)

	if c.settings.PriorityExclusive {
		payload[0] |= 0x80
	}

	payload[4] = c.settings.PriorityWeight

	frame := make([]byte, 14)
	frame[0], frame[1], frame[2] = 0x0, 0x0, 0x5
	frame[3] = 0x2 // PRIORITY
	frame[4] = 0x0

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

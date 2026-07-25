// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h2 implements HTTP/2 SETTINGS and PRIORITY frame impersonation and HPACK header field reordering.
package h2

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"

	"golang.org/x/net/http2"

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

	h2Transport http2.Transport
	h2Conns     map[string]*http2.ClientConn
	mu          sync.Mutex
}

// NewFramedTransport creates a [FramedTransport] wrapping base with custom HTTP/2 settings and header ordering rules.
func NewFramedTransport(base *http.Transport, settings Settings, orderedKeys ...string) *FramedTransport {
	ft := &FramedTransport{
		Transport:   base,
		settings:    settings,
		orderedKeys: orderedKeys,
		h2Conns:     make(map[string]*http2.ClientConn),
	}

	if base == nil {
		return ft
	}

	_, _ = http2.ConfigureTransports(base)

	return ft
}

// RoundTrip executes an HTTP request transaction, handling uTLS ALPN negotiation for HTTP/2 vs HTTP/1.1.
func (ft *FramedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "https" {
		if ft.Transport != nil {
			return ft.Transport.RoundTrip(req)
		}

		return http.DefaultTransport.RoundTrip(req)
	}

	addr := canonicalAddr(req.URL)

	if cc := ft.getH2Conn(addr); cc != nil {
		return cc.RoundTrip(req)
	}

	conn, err := ft.dialTLS(req.Context(), addr)
	if err != nil {
		return nil, err
	}

	if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{Conn: conn})
	}

	alpn := getALPN(conn)

	if alpn == "h2" {
		framed, ok := conn.(*framedConn)
		if !ok {
			framed = &framedConn{
				Conn:        conn,
				settings:    ft.settings,
				orderedKeys: ft.orderedKeys,
			}
		}

		cc, err := ft.h2Transport.NewClientConn(framed)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni h2: failed to create h2 client conn: %w", err)
		}

		ft.saveH2Conn(addr, cc)

		return cc.RoundTrip(req)
	}

	return http1RoundTrip(req, conn)
}

func (ft *FramedTransport) getH2Conn(addr string) *http2.ClientConn {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	cc, ok := ft.h2Conns[addr]
	if !ok {
		return nil
	}

	if !cc.CanTakeNewRequest() {
		delete(ft.h2Conns, addr)

		_ = cc.Close()

		return nil
	}

	return cc
}

func (ft *FramedTransport) saveH2Conn(addr string, cc *http2.ClientConn) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.h2Conns == nil {
		ft.h2Conns = make(map[string]*http2.ClientConn)
	}

	ft.h2Conns[addr] = cc
}

func (ft *FramedTransport) dialTLS(ctx context.Context, addr string) (net.Conn, error) {
	if ft.Transport != nil && ft.DialTLSContext != nil {
		return ft.DialTLSContext(ctx, "tcp", addr)
	}

	tlsCfg := &tls.Config{
		NextProtos: []string{"h2", "http/1.1"},
	}
	if ft.Transport != nil && ft.TLSClientConfig != nil {
		tlsCfg = ft.TLSClientConfig.Clone()
		if len(tlsCfg.NextProtos) == 0 {
			tlsCfg.NextProtos = []string{"h2", "http/1.1"}
		}
	}

	host, _, _ := net.SplitHostPort(addr)
	if tlsCfg.ServerName == "" {
		tlsCfg.ServerName = host
	}

	d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: tlsCfg}

	return d.DialContext(ctx, "tcp", addr)
}

func getALPN(conn net.Conn) string {
	if cs, ok := conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		return cs.ConnectionState().NegotiatedProtocol
	}

	return ""
}

func canonicalAddr(u *url.URL) string {
	host := u.Hostname()

	port := u.Port()
	if port == "" {
		port = "443"
	}

	return net.JoinHostPort(host, port)
}

func http1RoundTrip(req *http.Request, conn net.Conn) (*http.Response, error) {
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni h2: failed to write h1 request: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni h2: failed to read h1 response: %w", err)
	}

	resp.Body = &connCloser{ReadCloser: resp.Body, conn: conn}

	return resp, nil
}

type connCloser struct {
	io.ReadCloser
	conn net.Conn
}

func (c *connCloser) Close() error {
	err := c.ReadCloser.Close()
	_ = c.conn.Close()
	return err
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

// ConnectionState delegates to the underlying connection's ConnectionState if available.
func (c *framedConn) ConnectionState() tls.ConnectionState {
	if cs, ok := c.Conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		return cs.ConnectionState()
	}

	return tls.ConnectionState{}
}

// Handshake delegates to the underlying connection's Handshake if available.
func (c *framedConn) Handshake() error {
	if hs, ok := c.Conn.(interface{ Handshake() error }); ok {
		return hs.Handshake()
	}

	return nil
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

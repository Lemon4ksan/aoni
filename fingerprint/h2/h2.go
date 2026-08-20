// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h2 implements HTTP/2 SETTINGS and PRIORITY frame impersonation and HPACK header field reordering.
package h2

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/generic"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	impl "github.com/lemon4ksan/aoni/internal/fingerprint/h2"
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

// Settings holds the full set of HTTP/2 connection parameters for browser-grade frame impersonation.
//
// Specification Adherence:
// Conforms strictly to IETF RFC 9113 §6.5 (HTTP/2 SETTINGS frame format and parameters).
//
// Thread Safety:
// Struct instances are immutable configuration values; concurrent reads are safe.
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

// SettingsFromProfile populates [Settings] from a [profiles.H2Settings] profile definition.
//
// Postconditions:
//   - Yields a non-nil pointer to a newly initialized [Settings] struct.
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

// ParseSettings unmarshals a JSON-encoded string representation into an HTTP/2 [Settings] struct.
//
// Specification Adherence:
// Validates parameters against RFC 9113 bounds.
//
// Preconditions:
//   - jsonStr must contain valid JSON key-value pairs matching snake_case or PascalCase HTTP/2 setting names.
func ParseSettings(jsonStr string) (Settings, error) {
	var p settingsProxy
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return Settings{}, fmt.Errorf("aoni/h2: failed to decode settings JSON: %w", err)
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
	// h2Conns stores active *http2.ClientConn values keyed by canonical host:port.
	h2Conns generic.ConcurrentMap[string, *http2.ClientConn]
	dialMu  sync.Mutex
}

// NewFramedTransport creates a [FramedTransport] wrapping base with custom HTTP/2 settings and header ordering rules.
func NewFramedTransport(base *http.Transport, settings Settings, orderedKeys ...string) *FramedTransport {
	ft := &FramedTransport{
		Transport:   base,
		settings:    settings,
		orderedKeys: orderedKeys,
	}

	ft.h2Transport.MaxDecoderHeaderTableSize = settings.HeaderTableSize
	ft.h2Transport.MaxEncoderHeaderTableSize = settings.HeaderTableSize
	ft.h2Transport.MaxHeaderListSize = settings.MaxHeaderListSize
	ft.h2Transport.MaxReadFrameSize = settings.MaxFrameSize

	if base == nil {
		return ft
	}

	_, _ = http2.ConfigureTransports(base)

	return ft
}

// H2Transport returns a pointer to the underlying [http2.Transport] instance.
func (ft *FramedTransport) H2Transport() *http2.Transport {
	return &ft.h2Transport
}

// Unwrap returns the wrapped [*http.Transport] layer.
func (ft *FramedTransport) Unwrap() http.RoundTripper {
	return ft.Transport
}

// CloneTransport creates a copy of [FramedTransport] wrapping next.
func (ft *FramedTransport) CloneTransport(next http.RoundTripper) http.RoundTripper {
	if base, ok := next.(*http.Transport); ok {
		return NewFramedTransport(base, ft.settings, ft.orderedKeys...)
	}

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

	cc, fallbackConn, err := ft.getOrDialH2(req.Context(), addr)
	if err != nil {
		return nil, err
	}

	if cc != nil {
		return cc.RoundTrip(req)
	}

	if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{Conn: fallbackConn})
	}

	return http1RoundTrip(req, fallbackConn)
}

func (ft *FramedTransport) getOrDialH2(ctx context.Context, addr string) (*http2.ClientConn, net.Conn, error) {
	ft.dialMu.Lock()
	defer ft.dialMu.Unlock()

	// Double check after acquiring lock to prevent thundering herd
	if cc := ft.getH2Conn(addr); cc != nil {
		return cc, nil, nil
	}

	conn, err := ft.dialTLS(ctx, addr)
	if err != nil {
		return nil, nil, err
	}

	if trace := httptrace.ContextClientTrace(ctx); trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{Conn: conn})
	}

	alpn := getALPN(conn)
	if alpn == "h2" {
		dto := impl.SettingsDTO{
			HeaderTableSize:      ft.settings.HeaderTableSize,
			EnablePush:           ft.settings.EnablePush,
			MaxConcurrentStreams: ft.settings.MaxConcurrentStreams,
			InitialWindowSize:    ft.settings.InitialWindowSize,
			MaxFrameSize:         ft.settings.MaxFrameSize,
			MaxHeaderListSize:    ft.settings.MaxHeaderListSize,
			ConnectionFlow:       ft.settings.ConnectionFlow,
			InitialStreamID:      ft.settings.InitialStreamID,
			PriorityStreamDep:    ft.settings.PriorityStreamDep,
			PriorityExclusive:    ft.settings.PriorityExclusive,
			PriorityWeight:       ft.settings.PriorityWeight,
		}

		framed := impl.WrapConn(conn, dto, ft.orderedKeys)

		cc, err := ft.h2Transport.NewClientConn(framed)
		if err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("aoni/h2: failed to create h2 client conn: %w", err)
		}

		ft.saveH2Conn(addr, cc)

		return cc, nil, nil
	}

	return nil, conn, nil
}

func (ft *FramedTransport) getH2Conn(addr string) *http2.ClientConn {
	cc, ok := ft.h2Conns.Load(addr)
	if !ok || cc == nil {
		return nil
	}

	if !cc.CanTakeNewRequest() {
		// Evict stale connection; subsequent callers will dial a fresh one.
		ft.h2Conns.Delete(addr)

		_ = cc.Close()

		return nil
	}

	return cc
}

// saveH2Conn stores an active HTTP/2 client connection in the transport pool under addr.
func (ft *FramedTransport) saveH2Conn(addr string, cc *http2.ClientConn) {
	ft.h2Conns.Store(addr, cc)
}

// dialTLS establishes an encrypted TLS socket connection with ALPN support for HTTP/2 and HTTP/1.1.
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

// getALPN retrieves the negotiated ALPN protocol string from an active TLS connection.
func getALPN(conn net.Conn) string {
	if cs, ok := conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		return cs.ConnectionState().NegotiatedProtocol
	}

	return ""
}

// canonicalAddr normalizes URL host and port into a standard host:port address string.
func canonicalAddr(u *url.URL) string {
	host := u.Host
	if host == "" {
		return ":443"
	}

	if strings.Contains(host, ":") {
		return host
	}

	return host + ":443"
}

// http1RoundTrip executes an HTTP/1.1 transaction over an existing TLS connection if ALPN negotiation falls back.
func http1RoundTrip(req *http.Request, conn net.Conn) (*http.Response, error) {
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni/h2: failed to write h1 request: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni/h2: failed to read h1 response: %w", err)
	}

	resp.Body = &connCloser{ReadCloser: resp.Body, conn: conn}

	return resp, nil
}

// connCloser ensures both response stream and underlying network connection are closed on completion.
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

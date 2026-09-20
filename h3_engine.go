// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/net/quic"
	"github.com/lemon4ksan/mach/client/h3"
	coreh3 "github.com/lemon4ksan/mach/proto/h3"
	mach "github.com/lemon4ksan/mach/proto/http"
)

// H3Config configures the HTTP/3 execution engine and connection pool.
type H3Config struct {
	TLSConfig       *tls.Config
	Settings        *coreh3.Settings
	DialTimeout     time.Duration
	IdleConnTimeout time.Duration
	EnableDatagrams bool
}

// H3Option defines a functional option for configuring H3Engine.
type H3Option func(*H3Config)

// WithH3TLSConfig sets the TLS client configuration for HTTP/3 QUIC handshakes.
func WithH3TLSConfig(tlsConf *tls.Config) H3Option {
	return func(c *H3Config) {
		c.TLSConfig = tlsConf
	}
}

// WithH3Settings sets HTTP/3 protocol settings (RFC 9114 SETTINGS frame).
func WithH3Settings(settings *coreh3.Settings) H3Option {
	return func(c *H3Config) {
		c.Settings = settings
	}
}

// WithH3DialTimeout sets the timeout for dialing new QUIC connections.
func WithH3DialTimeout(d time.Duration) H3Option {
	return func(c *H3Config) {
		c.DialTimeout = d
	}
}

// WithH3IdleConnTimeout sets the idle timeout for pooled connections.
func WithH3IdleConnTimeout(d time.Duration) H3Option {
	return func(c *H3Config) {
		c.IdleConnTimeout = d
	}
}

// WithH3EnableDatagrams enables or disables QUIC datagram support.
func WithH3EnableDatagrams(enable bool) H3Option {
	return func(c *H3Config) {
		c.EnableDatagrams = enable
	}
}

// dialPromise coordinates concurrent dials to the same origin.
type dialPromise struct {
	done   chan struct{}
	cancel context.CancelFunc
	conn   *h3.ClientConn
	err    error
}

// H3Engine is an HTTP/3 execution engine implementing aoni.HTTPDoer, http.RoundTripper,
// io.Closer, and interface{ CloseIdleConnections() }.
type H3Engine struct {
	cfg       H3Config
	mu        sync.RWMutex
	conns     map[string]*h3.ClientConn
	dials     map[string]*dialPromise
	fixedConn *h3.ClientConn // non-nil in attached/fixed connection mode
	closed    bool
}

// NewH3Engine creates a new multiplexed HTTP/3 transport engine.
func NewH3Engine(opts ...H3Option) *H3Engine {
	cfg := H3Config{
		DialTimeout:     10 * time.Second,
		IdleConnTimeout: 90 * time.Second,
		EnableDatagrams: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.TLSConfig == nil {
		cfg.TLSConfig = &tls.Config{
			NextProtos: []string{"h3"},
		}
	} else if len(cfg.TLSConfig.NextProtos) == 0 {
		cfg.TLSConfig = cfg.TLSConfig.Clone()
		cfg.TLSConfig.NextProtos = []string{"h3"}
	}

	return &H3Engine{
		cfg:   cfg,
		conns: make(map[string]*h3.ClientConn),
		dials: make(map[string]*dialPromise),
	}
}

// NewH3EngineFromConn creates an HTTP/3 engine bound to an existing *h3.ClientConn.
func NewH3EngineFromConn(conn *h3.ClientConn) *H3Engine {
	return &H3Engine{
		fixedConn: conn,
		conns:     make(map[string]*h3.ClientConn),
		dials:     make(map[string]*dialPromise),
	}
}

// Do executes an HTTP request over HTTP/3, satisfying aoni.HTTPDoer.
func (e *H3Engine) Do(req *http.Request) (*http.Response, error) {
	return e.RoundTrip(req)
}

// RoundTrip executes an HTTP transaction over HTTP/3, satisfying http.RoundTripper.
func (e *H3Engine) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("aoni/h3: request is nil")
	}

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cc, err := e.getConn(ctx, req)
	if err != nil {
		return nil, err
	}

	fastReq := mach.AcquireRequest()
	fastResp := mach.AcquireResponse()

	if err := e.populateRequest(req, fastReq); err != nil {
		mach.ReleaseRequest(fastReq)
		mach.ReleaseResponse(fastResp)
		return nil, err
	}

	trailers, err := cc.Do(ctx, fastReq, fastResp, nil)
	if err != nil {
		mach.ReleaseRequest(fastReq)
		mach.ReleaseResponse(fastResp)

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, err
	}

	return e.buildResponse(req, fastReq, fastResp, trailers), nil
}

func (e *H3Engine) dial(ctx context.Context, addr string) (*h3.ClientConn, error) {
	tlsConf := e.cfg.TLSConfig
	if tlsConf == nil {
		tlsConf = &tls.Config{NextProtos: []string{"h3"}}
	}

	disableMTUOpt := func(c *quic.Config) {
		c.DisablePathMTUDiscovery = true
	}

	qConn, err := quic.DialAddr(ctx, addr, tlsConf, quic.WithDatagrams(e.cfg.EnableDatagrams), disableMTUOpt)
	if err != nil {
		return nil, fmt.Errorf("aoni/h3: quic dial %s failed: %w", addr, err)
	}

	cc, err := h3.NewClientConn(qConn, e.cfg.Settings)
	if err != nil {
		_ = qConn.CloseWithError(0, "failed h3 client init")
		return nil, fmt.Errorf("aoni/h3: new client conn %s failed: %w", addr, err)
	}

	return cc, nil
}

func (e *H3Engine) getConn(ctx context.Context, req *http.Request) (*h3.ClientConn, error) {
	e.mu.RLock()

	if e.closed {
		e.mu.RUnlock()
		return nil, errors.New("aoni/h3: engine closed")
	}

	if e.fixedConn != nil {
		conn := e.fixedConn
		e.mu.RUnlock()

		if conn.IsClosed() {
			return nil, errors.New("aoni/h3: fixed connection is closed")
		}

		return conn, nil
	}

	addr := getTargetAddr(req)
	if cc, ok := e.conns[addr]; ok && !cc.IsClosed() {
		e.mu.RUnlock()
		return cc, nil
	}

	e.mu.RUnlock()

	for {
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return nil, errors.New("aoni/h3: engine closed")
		}

		if cc, ok := e.conns[addr]; ok && !cc.IsClosed() {
			e.mu.Unlock()
			return cc, nil
		}

		p, inFlight := e.dials[addr]
		if inFlight {
			// Another goroutine is already dialing this origin; release lock and await completion
			e.mu.Unlock()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-p.done:
				if p.conn != nil && !p.conn.IsClosed() {
					return p.conn, nil
				}

				if ctx.Err() != nil {
					return nil, ctx.Err()
				}

				// Prior dial failed or was closed while our context remains valid; retry
				continue
			}
		}

		// We are the designated dialer for this origin
		dialTimeout := e.cfg.DialTimeout
		if dialTimeout <= 0 {
			dialTimeout = 10 * time.Second
		}

		dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)

		p = &dialPromise{
			done:   make(chan struct{}),
			cancel: dialCancel,
		}
		if e.dials == nil {
			e.dials = make(map[string]*dialPromise)
		}

		e.dials[addr] = p
		e.mu.Unlock()

		// Execute network I/O and TLS handshake without holding e.mu
		cc, err := e.dial(dialCtx, addr)

		dialCancel()

		e.mu.Lock()
		delete(e.dials, addr)

		if err != nil {
			p.err = err
			close(p.done)
			e.mu.Unlock()

			return nil, err
		}

		if e.closed {
			e.mu.Unlock()

			_ = cc.Close()
			err = errors.New("aoni/h3: engine closed")
			p.err = err
			close(p.done)

			return nil, err
		}

		e.conns[addr] = cc
		p.conn = cc
		close(p.done)
		e.mu.Unlock()

		return cc, nil
	}
}

func (e *H3Engine) populateRequest(req *http.Request, fastReq *mach.Request) error {
	fastReq.Header.SetMethod(req.Method)

	if req.URL != nil {
		fastReq.SetRequestURI(req.URL.String())
	}

	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}

	fastReq.Header.SetHost(host)

	for k, vv := range req.Header {
		if isForbiddenH3Header(k) {
			continue
		}

		if strings.EqualFold(k, "te") {
			for _, v := range vv {
				if strings.EqualFold(v, "trailers") {
					fastReq.Header.Add(k, v)
				}
			}

			continue
		}

		for _, v := range vv {
			fastReq.Header.Add(k, v)
		}
	}

	if req.Body != nil && req.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}

		_ = req.Body.Close()

		fastReq.SetBody(bodyBytes)

		if req.ContentLength >= 0 {
			fastReq.Header.SetContentLength(int(req.ContentLength))
		} else {
			fastReq.Header.SetContentLength(len(bodyBytes))
		}
	} else {
		fastReq.ResetBody()
	}

	return nil
}

func (e *H3Engine) buildResponse(
	req *http.Request,
	fastReq *mach.Request,
	fastResp *mach.Response,
	trailers map[string][]string,
) *http.Response {
	statusCode := fastResp.StatusCode()

	header := make(http.Header, fastResp.Header.Len())
	for k, v := range fastResp.Header.All() {
		header.Add(string(k), string(v))
	}

	body := fastResp.Body()
	bodyCloser := &h3BodyReadCloser{
		Reader:   bytes.NewReader(body),
		fastReq:  fastReq,
		fastResp: fastResp,
	}

	httpResp := &http.Response{
		StatusCode:    statusCode,
		Status:        fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Proto:         "HTTP/3.0",
		ProtoMajor:    3,
		ProtoMinor:    0,
		Header:        header,
		Body:          bodyCloser,
		ContentLength: int64(len(body)),
		Request:       req,
	}

	if len(trailers) > 0 {
		httpResp.Trailer = make(http.Header, len(trailers))
		for k, vv := range trailers {
			for _, v := range vv {
				httpResp.Trailer.Add(k, v)
			}
		}
	}

	return httpResp
}

// CloseIdleConnections terminates all idle HTTP/3 connections in the pool.
func (e *H3Engine) CloseIdleConnections() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for addr, cc := range e.conns {
		_ = cc.Close()

		delete(e.conns, addr)
	}
}

// Close terminates all active HTTP/3 connections and releases resources.
func (e *H3Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.closed = true
	if e.fixedConn != nil {
		_ = e.fixedConn.Close()
	}

	for addr, cc := range e.conns {
		_ = cc.Close()

		delete(e.conns, addr)
	}

	for addr, p := range e.dials {
		if p.cancel != nil {
			p.cancel()
		}

		delete(e.dials, addr)
	}

	return nil
}

type h3BodyReadCloser struct {
	io.Reader
	fastReq  *mach.Request
	fastResp *mach.Response
	once     sync.Once
}

func (b *h3BodyReadCloser) Close() error {
	b.once.Do(func() {
		if b.fastReq != nil {
			mach.ReleaseRequest(b.fastReq)
			b.fastReq = nil
		}

		if b.fastResp != nil {
			mach.ReleaseResponse(b.fastResp)
			b.fastResp = nil
		}
	})

	return nil
}

func getTargetAddr(req *http.Request) string {
	host := ""
	if req.URL != nil && req.URL.Host != "" {
		host = req.URL.Host
	} else if req.Host != "" {
		host = req.Host
	} else {
		return "127.0.0.1:443"
	}

	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}

	return net.JoinHostPort(host, "443")
}

func isForbiddenH3Header(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "keep-alive", "proxy-connection", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

// Ensure interface compliance at compile time.
var (
	_ HTTPDoer                            = (*H3Engine)(nil)
	_ http.RoundTripper                   = (*H3Engine)(nil)
	_ io.Closer                           = (*H3Engine)(nil)
	_ interface{ CloseIdleConnections() } = (*H3Engine)(nil)
)

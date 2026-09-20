// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/net/quic"
	"github.com/lemon4ksan/mach/client/h1"
	"github.com/lemon4ksan/mach/client/h2"
	"github.com/lemon4ksan/mach/client/h3"
	coreh3 "github.com/lemon4ksan/mach/proto/h3"
	machhttp "github.com/lemon4ksan/mach/proto/http"
)

// Pool coordinates multi-protocol zero-allocation connections across HTTP/1.1, HTTP/2, and HTTP/3.
type Pool struct {
	mu sync.Mutex

	// H1 pooled connections (single stream per connection)
	conns map[string][]*h1.ClientConn

	// H2 multiplexed connections (single connection per origin)
	h2Conns map[string]*h2.Conn
	h2Dials map[string]*h2DialPromise

	// H3 QUIC multiplexed connections (single connection per origin)
	h3Conns map[string]*h3.ClientConn
	h3Dials map[string]*dialPromise

	ReadTimeout                   time.Duration
	WriteTimeout                  time.Duration
	TLSConfig                     *tls.Config
	Dial                          func(string) (net.Conn, error)
	DialDualStack                 bool
	DisableHeaderNamesNormalizing bool

	// Protocol toggles and settings
	EnableH2   bool
	EnableH3   bool
	ForceH2    bool
	ForceH3    bool
	H2Opts     h2.ConnOpts
	H3Settings *coreh3.Settings
}

type h2DialPromise struct {
	done chan struct{}
	conn *h2.Conn
	err  error
}

type dialPromise struct {
	done chan struct{}
	conn *h3.ClientConn
	err  error
}

// NewPool initializes a multi-protocol connection pool.
func NewPool() *Pool {
	return &Pool{
		conns:    make(map[string][]*h1.ClientConn),
		h2Conns:  make(map[string]*h2.Conn),
		h2Dials:  make(map[string]*h2DialPromise),
		h3Conns:  make(map[string]*h3.ClientConn),
		h3Dials:  make(map[string]*dialPromise),
		EnableH2: true,
	}
}

// Do executes a request using background context.
func (p *Pool) Do(req *machhttp.Request, res *machhttp.Response) error {
	_, err := p.DoCtx(context.Background(), req, res)
	return err
}

// DoCtx executes a request under context, automatically selecting and multiplexing over H1, H2, or H3.
func (p *Pool) DoCtx(ctx context.Context, req *machhttp.Request, res *machhttp.Response) (map[string][]string, error) {
	addr := string(req.Host())
	if addr == "" {
		addr = string(req.URI().Host())
	}
	scheme := string(req.URI().Scheme())
	isTLS := strings.EqualFold(scheme, "https")

	if !strings.Contains(addr, ":") {
		if isTLS {
			addr += ":443"
		} else {
			addr += ":80"
		}
	}

	// 1. HTTP/3 QUIC path
	if p.ForceH3 || (p.EnableH3 && isTLS) {
		trailers, err := p.doH3(ctx, addr, req, res)
		if err == nil || p.ForceH3 {
			return trailers, err
		}
		// If H3 was opportunistic, fall back to TLS H2/H1 below
	}

	// 2. TLS path (HTTP/2 or HTTP/1.1 over TLS)
	if isTLS {
		return p.doTLS(ctx, addr, req, res)
	}

	// 3. Plain HTTP/1.1 path
	return nil, p.doH1(ctx, addr, req, res, nil)
}

func (p *Pool) doH3(ctx context.Context, addr string, req *machhttp.Request, res *machhttp.Response) (map[string][]string, error) {
	for {
		p.mu.Lock()
		if cc := p.h3Conns[addr]; cc != nil && !cc.IsClosed() {
			p.mu.Unlock()
			return cc.Do(ctx, req, res, nil)
		}

		dp, inFlight := p.h3Dials[addr]
		if inFlight {
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-dp.done:
				if dp.conn != nil && !dp.conn.IsClosed() {
					return dp.conn.Do(ctx, req, res, nil)
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
		}

		dp = &dialPromise{
			done: make(chan struct{}),
		}
		if p.h3Dials == nil {
			p.h3Dials = make(map[string]*dialPromise)
		}
		p.h3Dials[addr] = dp
		p.mu.Unlock()

		tlsConf := p.getTLSConfig(addr, []string{"h3"})
		disableMTUOpt := func(c *quic.Config) { c.DisablePathMTUDiscovery = true }

		qConn, err := quic.DialAddr(ctx, addr, tlsConf, quic.WithDatagrams(true), disableMTUOpt)
		var newCC *h3.ClientConn
		if err == nil {
			newCC, err = h3.NewClientConn(qConn, p.H3Settings)
			if err != nil {
				_ = qConn.CloseWithError(0, "h3 client init failed")
			}
		}

		p.mu.Lock()
		delete(p.h3Dials, addr)
		dp.conn = newCC
		dp.err = err
		close(dp.done)

		if err == nil && newCC != nil {
			p.h3Conns[addr] = newCC
			p.mu.Unlock()
			return newCC.Do(ctx, req, res, nil)
		}
		p.mu.Unlock()

		return nil, err
	}
}

func (p *Pool) doTLS(ctx context.Context, addr string, req *machhttp.Request, res *machhttp.Response) (map[string][]string, error) {
	for {
		// Check existing multiplexed H2 connection
		if p.EnableH2 || p.ForceH2 {
			p.mu.Lock()
			if h2c := p.h2Conns[addr]; h2c != nil && !h2c.Closed() && h2c.CanOpenStream() {
				p.mu.Unlock()
				return nil, h2c.Do(ctx, req, res)
			}

			dp, inFlight := p.h2Dials[addr]
			if inFlight {
				p.mu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-dp.done:
					if dp.conn != nil && !dp.conn.Closed() && dp.conn.CanOpenStream() {
						return nil, dp.conn.Do(ctx, req, res)
					}
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					continue
				}
			}

			dp = &h2DialPromise{
				done: make(chan struct{}),
			}
			if p.h2Dials == nil {
				p.h2Dials = make(map[string]*h2DialPromise)
			}
			p.h2Dials[addr] = dp
			p.mu.Unlock()

			// Dial fresh TLS connection with ALPN h2
			nextProtos := []string{"h2", "http/1.1"}
			tlsConf := p.getTLSConfig(addr, nextProtos)
			rawConn, err := p.dialTCP(ctx, addr)
			if err != nil {
				p.mu.Lock()
				delete(p.h2Dials, addr)
				dp.err = err
				close(dp.done)
				p.mu.Unlock()
				return nil, err
			}

			tlsConn := tls.Client(rawConn, tlsConf)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = rawConn.Close()
				p.mu.Lock()
				delete(p.h2Dials, addr)
				dp.err = err
				close(dp.done)
				p.mu.Unlock()
				return nil, err
			}

			negotiated := tlsConn.ConnectionState().NegotiatedProtocol
			if negotiated == "h2" || p.ForceH2 {
				h2Conn := h2.NewConn(tlsConn, p.H2Opts)
				if err := h2Conn.Handshake(); err != nil {
					_ = tlsConn.Close()
					p.mu.Lock()
					delete(p.h2Dials, addr)
					dp.err = err
					close(dp.done)
					p.mu.Unlock()
					return nil, fmt.Errorf("transport: h2 handshake failed: %w", err)
				}

				p.mu.Lock()
				p.h2Conns[addr] = h2Conn
				delete(p.h2Dials, addr)
				dp.conn = h2Conn
				close(dp.done)
				p.mu.Unlock()

				return nil, h2Conn.Do(ctx, req, res)
			}

			// Negotiated HTTP/1.1 over TLS fallback
			p.mu.Lock()
			delete(p.h2Dials, addr)
			dp.err = nil
			close(dp.done)
			p.mu.Unlock()

			return nil, p.doH1(ctx, addr, req, res, tlsConn)
		}

		// Plain TLS H1 (when H2 is disabled)
		tlsConf := p.getTLSConfig(addr, []string{"http/1.1"})
		rawConn, err := p.dialTCP(ctx, addr)
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Client(rawConn, tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		return nil, p.doH1(ctx, addr, req, res, tlsConn)
	}
}

func (p *Pool) doH1(ctx context.Context, addr string, req *machhttp.Request, res *machhttp.Response, existingConn net.Conn) error {
	var cc *h1.ClientConn

	if existingConn != nil {
		cc = h1.NewClientConn(existingConn)
	} else {
		p.mu.Lock()
		if list := p.conns[addr]; len(list) > 0 {
			cc = list[len(list)-1]
			p.conns[addr] = list[:len(list)-1]
		}
		p.mu.Unlock()

		if cc == nil {
			c, err := p.dialTCP(ctx, addr)
			if err != nil {
				return err
			}
			cc = h1.NewClientConn(c)
		}
	}

	err := cc.Do(ctx, req, res)
	if err != nil {
		_ = cc.Close()
		return err
	}

	if !res.ConnectionClose() {
		p.mu.Lock()
		p.conns[addr] = append(p.conns[addr], cc)
		p.mu.Unlock()
	} else {
		_ = cc.Close()
	}

	return nil
}

func (p *Pool) dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func (p *Pool) getTLSConfig(addr string, nextProtos []string) *tls.Config {
	var base *tls.Config
	if p.TLSConfig != nil {
		base = p.TLSConfig.Clone()
	} else {
		base = &tls.Config{}
	}

	if base.ServerName == "" {
		h, _, err := net.SplitHostPort(addr)
		if err == nil {
			base.ServerName = h
		} else {
			base.ServerName = addr
		}
	}

	if len(nextProtos) > 0 {
		base.NextProtos = nextProtos
	}

	return base
}

// DoPipeline executes requests sequentially over HTTP/1.1 connections.
func (p *Pool) DoPipeline(reqs []*machhttp.Request, resps []*machhttp.Response) error {
	for i := range reqs {
		if err := p.Do(reqs[i], resps[i]); err != nil {
			return err
		}
	}
	return nil
}

// CloseIdleConnections closes all idle connections across H1, H2, and H3 pools.
func (p *Pool) CloseIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Close H1 connections
	for _, list := range p.conns {
		for _, cc := range list {
			_ = cc.Close()
		}
	}
	p.conns = make(map[string][]*h1.ClientConn)

	// 2. Close H2 connections
	for _, h2c := range p.h2Conns {
		_ = h2c.Close()
	}
	p.h2Conns = make(map[string]*h2.Conn)
	p.h2Dials = make(map[string]*h2DialPromise)

	// 3. Close H3 connections
	for _, h3c := range p.h3Conns {
		_ = h3c.Close()
	}
	p.h3Conns = make(map[string]*h3.ClientConn)
	p.h3Dials = make(map[string]*dialPromise)
}

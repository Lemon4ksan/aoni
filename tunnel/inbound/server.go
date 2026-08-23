// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inbound provides a high-performance, mixed SOCKS5 and HTTP/HTTPS inbound proxy server.
package inbound

import (
	"bufio"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
)

// Server represents a high-performance, mixed SOCKS5 and HTTP/HTTPS inbound proxy server.
type Server struct {
	Addr          string
	Engine        aoni.RequestDoer
	CA            *ca.CA
	RootCACert    *tls.Certificate
	Auth          func(username, password string) bool
	EnableMITM    bool
	certCache     sync.Map
	sharedLeafKey crypto.PrivateKey

	listener net.Listener
	mu       sync.RWMutex
	closed   atomic.Bool
	done     chan struct{}
	conns    sync.WaitGroup
}

// NewServer creates a new inbound proxy Server with optional functional settings.
func NewServer(addr string, opts ...Option) (*Server, error) {
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	srv := &Server{
		Addr:          addr,
		done:          make(chan struct{}),
		sharedLeafKey: leafKey,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(srv); err != nil {
			return nil, err
		}
	}

	return srv, nil
}

// ListenAndServe listens on srv.Addr (defaults to "127.0.0.1:1080") and serves incoming proxy requests.
func (srv *Server) ListenAndServe(ctx context.Context) error {
	addr := srv.Addr
	if addr == "" {
		addr = "127.0.0.1:1080"
	}

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("aoni inbound: listen failed on %s: %w", addr, err)
	}

	return srv.Serve(ctx, ln)
}

// Serve accepts incoming connections on ln and dispatches SOCKS5/HTTP protocol handlers.
func (srv *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv.mu.Lock()
	if srv.closed.Load() {
		srv.mu.Unlock()

		_ = ln.Close()

		return ErrServerClosed
	}

	srv.listener = ln
	srv.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if srv.closed.Load() {
				return ErrServerClosed
			}

			return err
		}

		srv.conns.Add(1)

		go func(c net.Conn) {
			defer srv.conns.Done()

			srv.handleConn(ctx, c)
		}(conn)
	}
}

func (srv *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)

	proto, err := SniffProtocol(br)
	if err != nil {
		return
	}

	switch proto {
	case ProtocolSOCKS5:
		_ = handleSOCKS5Conn(ctx, srv, conn, br)
	case ProtocolHTTP:
		_ = handleHTTPProxyConn(ctx, srv, conn, br)
	default:
		_ = handleHTTPProxyConn(ctx, srv, conn, br)
	}
}

// Close immediately terminates listeners and active proxy connections.
func (srv *Server) Close() error {
	if srv.closed.CompareAndSwap(false, true) {
		srv.mu.Lock()
		if srv.listener != nil {
			_ = srv.listener.Close()
		}

		srv.mu.Unlock()

		close(srv.done)
	}

	return nil
}

// Shutdown gracefully stops the server by closing listeners and waiting for active connections to finish.
func (srv *Server) Shutdown(ctx context.Context) error {
	_ = srv.Close()

	c := make(chan struct{})
	go func() {
		srv.conns.Wait()
		close(c)
	}()

	select {
	case <-c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

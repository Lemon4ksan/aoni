// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/lemon4ksan/foundation/net/quic"
	h3client "github.com/lemon4ksan/mach/client/h3"
	machh3server "github.com/lemon4ksan/mach/server/h3"
)

// Server represents a generic multi-protocol in-process test server.
type Server interface {
	Protocol() string
	URL() string
	Addr() string
	Port() int
	TLSConfig() *tls.Config
	Close() error
}

// TLSConfigPair holds generated test TLS certificates and client CA pools.
type TLSConfigPair struct {
	TLSCert   tls.Certificate
	X509Cert  *x509.Certificate
	CertPool  *x509.CertPool
	ServerTLS *tls.Config
	ClientTLS *tls.Config
}

// GenerateTestTLSPair creates an ephemeral in-memory ECDSA P-256 TLS certificate.
func GenerateTestTLSPair(t testing.TB, nextProtos ...string) *TLSConfigPair {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA test key: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed to generate serial number: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Aoni In-Process Test Server"},
			CommonName:   "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		DNSNames:              []string{"localhost", "127.0.0.1"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create test certificate: %v", err)
	}

	parsedCert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("failed to parse generated certificate: %v", err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(parsedCert)

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   nextProtos,
	}

	clientTLS := &tls.Config{
		RootCAs:            certPool,
		ServerName:         "127.0.0.1",
		NextProtos:         nextProtos,
		InsecureSkipVerify: false,
	}

	return &TLSConfigPair{
		TLSCert:   tlsCert,
		X509Cert:  parsedCert,
		CertPool:  certPool,
		ServerTLS: serverTLS,
		ClientTLS: clientTLS,
	}
}

// -----------------------------------------------------------------------------
// TCP Ephemeral Port Quarantining Allocator
// -----------------------------------------------------------------------------

var (
	allocatedPortsMu     sync.Mutex
	recentlyUsedTCPPorts = make(map[int]time.Time)
)

const portReuseCooldown = 3 * time.Minute

// listenTCPAllocUnique allocates a TCP listener on laddr guaranteeing that the assigned
// port has not been used within the recent cooldown window. This eliminates Windows
// WSAEADDRINUSE (10048) connectex collisions caused by TIME_WAIT sockets from prior tests.
func listenTCPAllocUnique(network, laddr string) (net.Listener, error) {
	allocatedPortsMu.Lock()
	defer allocatedPortsMu.Unlock()

	var tempHold []net.Listener
	defer func() {
		for _, l := range tempHold {
			_ = l.Close()
		}
	}()

	for attempt := 0; attempt < 100; attempt++ {
		ln, err := net.Listen(network, laddr)
		if err != nil {
			return nil, err
		}

		tcpAddr, ok := ln.Addr().(*net.TCPAddr)
		if !ok {
			return ln, nil
		}
		port := tcpAddr.Port
		lastUsed, exists := recentlyUsedTCPPorts[port]
		if !exists || time.Since(lastUsed) > portReuseCooldown {
			recentlyUsedTCPPorts[port] = time.Now()
			return ln, nil
		}

		// Port was recently used; hold listener open so OS does not re-allocate it in next attempt
		tempHold = append(tempHold, ln)
	}

	// Fallback if 100 attempts exhausted
	return net.Listen(network, laddr)
}

var defaultDialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 30 * time.Second,
}

// isTransientConnectError returns true if err indicates a transient Winsock port collision
// or kernel TIME_WAIT address contention (WSAEADDRINUSE / 10048).
func isTransientConnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == 10048 || errno == syscall.EADDRINUSE) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connectex") ||
		strings.Contains(s, "wsaeaddrinuse") ||
		strings.Contains(s, "only one usage of each socket address") ||
		strings.Contains(s, "address already in use") ||
		strings.Contains(s, "10048")
}

// retryDialTCP dials a TCP connection with retries on transient connectex / WSAEADDRINUSE errors.
func retryDialTCP(ctx context.Context, network, addr string, customDial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	dialFn := customDial
	if dialFn == nil {
		dialFn = defaultDialer.DialContext
	}

	backoff := 5 * time.Millisecond
	for attempt := 0; attempt < 8; attempt++ {
		conn, err := dialFn(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		if !isTransientConnectError(err) || ctx.Err() != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 50*time.Millisecond {
			backoff = 50 * time.Millisecond
		}
	}
	return dialFn(ctx, network, addr)
}

// retryDialTLS dials a TLS connection with retries on transient connectex / WSAEADDRINUSE errors.
func retryDialTLS(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	dialCfg := cfg
	if dialCfg == nil {
		dialCfg = &tls.Config{}
	} else if dialCfg.ServerName == "" {
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr == nil {
			dialCfg = cfg.Clone()
			dialCfg.ServerName = host
		}
	}

	backoff := 5 * time.Millisecond
	for attempt := 0; attempt < 8; attempt++ {
		rawConn, err := defaultDialer.DialContext(ctx, network, addr)
		if err != nil {
			if !isTransientConnectError(err) || ctx.Err() != nil {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 50*time.Millisecond {
				backoff = 50 * time.Millisecond
			}
			continue
		}

		tlsConn := tls.Client(rawConn, dialCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			if !isTransientConnectError(err) || ctx.Err() != nil {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 50*time.Millisecond {
				backoff = 50 * time.Millisecond
			}
			continue
		}
		return tlsConn, nil
	}

	rawConn, err := defaultDialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(rawConn, dialCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// -----------------------------------------------------------------------------
// HTTP/1.1 In-Process Test Server
// -----------------------------------------------------------------------------

// H1TestServer manages an in-process HTTP/1.1 test server bound to 127.0.0.1:0.
type H1TestServer struct {
	server   *httptest.Server
	listener net.Listener
	addr     string
	port     int
}

// NewH1Server starts an in-process HTTP/1.1 test server on 127.0.0.1:0.
func NewH1Server(t testing.TB, handler http.Handler) *H1TestServer {
	t.Helper()

	ln, err := listenTCPAllocUnique("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind H1 listener on 127.0.0.1:0: %v", err)
	}

	ts := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler},
	}
	ts.Start()

	if tr, ok := ts.Client().Transport.(*http.Transport); ok {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return retryDialTCP(ctx, network, addr, nil)
		}
	}

	tcpAddr := ln.Addr().(*net.TCPAddr)
	s := &H1TestServer{
		server:   ts,
		listener: ln,
		addr:     ln.Addr().String(),
		port:     tcpAddr.Port,
	}

	t.Cleanup(func() { _ = s.Close() })
	return s
}

func (s *H1TestServer) Protocol() string       { return "HTTP/1.1" }
func (s *H1TestServer) URL() string            { return s.server.URL }
func (s *H1TestServer) Addr() string           { return s.addr }
func (s *H1TestServer) Port() int              { return s.port }
func (s *H1TestServer) TLSConfig() *tls.Config { return nil }
func (s *H1TestServer) Client() *http.Client {
	c := s.server.Client()
	if tr, ok := c.Transport.(*http.Transport); ok && tr.DialContext == nil {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return retryDialTCP(ctx, network, addr, nil)
		}
	}
	return c
}
func (s *H1TestServer) Close() error {
	s.server.Close()
	return nil
}

// -----------------------------------------------------------------------------
// HTTP/2 In-Process TLS Test Server
// -----------------------------------------------------------------------------

// H2TestServer manages an in-process HTTP/2 TLS server bound to 127.0.0.1:0.
type H2TestServer struct {
	server   *httptest.Server
	listener net.Listener
	tlsPair  *TLSConfigPair
	addr     string
	port     int
}

// NewH2Server starts an in-process HTTP/2 test server on 127.0.0.1:0 with ALPN "h2".
func NewH2Server(t testing.TB, handler http.Handler) *H2TestServer {
	t.Helper()

	tlsPair := GenerateTestTLSPair(t, "h2", "http/1.1")

	ln, err := listenTCPAllocUnique("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind H2 listener on 127.0.0.1:0: %v", err)
	}

	ts := httptest.NewUnstartedServer(handler)
	ts.Listener = ln
	ts.TLS = tlsPair.ServerTLS
	_ = http2.ConfigureServer(ts.Config, &http2.Server{})
	ts.StartTLS()

	if tr, ok := ts.Client().Transport.(*http.Transport); ok {
		tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return retryDialTLS(ctx, network, addr, tlsPair.ClientTLS)
		}
	}

	tcpAddr := ln.Addr().(*net.TCPAddr)
	s := &H2TestServer{
		server:   ts,
		listener: ln,
		tlsPair:  tlsPair,
		addr:     ln.Addr().String(),
		port:     tcpAddr.Port,
	}

	t.Cleanup(func() { _ = s.Close() })
	return s
}

func (s *H2TestServer) Protocol() string       { return "HTTP/2" }
func (s *H2TestServer) URL() string            { return s.server.URL }
func (s *H2TestServer) Addr() string           { return s.addr }
func (s *H2TestServer) Port() int              { return s.port }
func (s *H2TestServer) TLSConfig() *tls.Config { return s.tlsPair.ClientTLS.Clone() }
func (s *H2TestServer) Client() *http.Client {
	tlsCfg := s.TLSConfig()
	tr := &http2.Transport{
		TLSClientConfig: tlsCfg,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			dialCfg := cfg
			if dialCfg == nil {
				dialCfg = tlsCfg
			}
			return retryDialTLS(ctx, network, addr, dialCfg)
		},
	}
	return &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}
}
func (s *H2TestServer) Close() error {
	s.server.Close()
	return nil
}

// -----------------------------------------------------------------------------
// HTTP/3 In-Process QUIC Test Server
// -----------------------------------------------------------------------------

// H3TestServer manages an in-process HTTP/3 server over QUIC bound to 127.0.0.1:0.
type H3TestServer struct {
	listener  *quic.Listener
	tlsPair   *TLSConfigPair
	addr      string
	port      int
	url       string
	h3Handler machh3server.ServerHandlerFunc

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	isClosed  atomic.Bool

	mu          sync.Mutex
	activeConns map[*quic.Conn]*machh3server.ServerConn
}

// NewH3Server starts an in-process HTTP/3 test server on 127.0.0.1:0.
// The handler can be either mach/server/h3.ServerHandlerFunc or http.Handler.
func NewH3Server(t testing.TB, handler any) *H3TestServer {
	t.Helper()

	tlsPair := GenerateTestTLSPair(t, "h3")

	var h3Handler machh3server.ServerHandlerFunc
	switch h := handler.(type) {
	case machh3server.ServerHandlerFunc:
		h3Handler = h
	case func(req *machh3server.ServerRequest, res *machh3server.ServerResponse) error:
		h3Handler = h
	case http.Handler:
		h3Handler = AdaptHTTPHandlerToH3(h)
	case func(w http.ResponseWriter, r *http.Request):
		h3Handler = AdaptHTTPHandlerToH3(http.HandlerFunc(h))
	default:
		t.Fatalf("unsupported H3 handler type: %T", handler)
	}

	disableMTUOpt := func(c *quic.Config) {
		c.DisablePathMTUDiscovery = true
	}

	ln, err := quic.ListenAddr("127.0.0.1:0", tlsPair.ServerTLS, quic.WithDatagrams(true), disableMTUOpt)
	if err != nil {
		t.Fatalf("failed to bind H3 QUIC listener on 127.0.0.1:0: %v", err)
	}

	udpAddr := ln.Addr().(*net.UDPAddr)
	ctx, cancel := context.WithCancel(context.Background())

	s := &H3TestServer{
		listener:    ln,
		tlsPair:     tlsPair,
		addr:        ln.Addr().String(),
		port:        udpAddr.Port,
		url:         fmt.Sprintf("https://127.0.0.1:%d", udpAddr.Port),
		h3Handler:   h3Handler,
		ctx:         ctx,
		cancel:      cancel,
		activeConns: make(map[*quic.Conn]*machh3server.ServerConn),
	}

	s.wg.Add(1)
	go s.acceptLoop()

	t.Cleanup(func() { _ = s.Close() })
	return s
}

func (s *H3TestServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept(s.ctx)
		if err != nil {
			return
		}

		sc := machh3server.NewServerConn(conn, s.h3Handler)
		s.registerConn(conn, sc)

		s.wg.Add(1)
		go func(c *quic.Conn, serverConn *machh3server.ServerConn) {
			defer s.wg.Done()
			defer s.unregisterConn(c)
			_ = serverConn.Serve()
		}(conn, sc)
	}
}

func (s *H3TestServer) registerConn(c *quic.Conn, sc *machh3server.ServerConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isClosed.Load() {
		s.activeConns[c] = sc
	} else {
		_ = c.CloseWithError(0x0100, "server closed")
	}
}

func (s *H3TestServer) unregisterConn(c *quic.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeConns, c)
}

func (s *H3TestServer) Protocol() string       { return "HTTP/3" }
func (s *H3TestServer) URL() string            { return s.url }
func (s *H3TestServer) Addr() string           { return s.addr }
func (s *H3TestServer) Port() int              { return s.port }
func (s *H3TestServer) TLSConfig() *tls.Config { return s.tlsPair.ClientTLS.Clone() }

// DialQUICConn establishes an active client QUIC connection to the server.
func (s *H3TestServer) DialQUICConn(ctx context.Context) (*quic.Conn, error) {
	disableMTUOpt := func(c *quic.Config) {
		c.DisablePathMTUDiscovery = true
	}
	return quic.DialAddr(ctx, s.addr, s.TLSConfig(), quic.WithDatagrams(true), disableMTUOpt)
}

// DialClientConn establishes an active HTTP/3 ClientConn session to the server.
func (s *H3TestServer) DialClientConn(ctx context.Context) (*h3client.ClientConn, error) {
	conn, err := s.DialQUICConn(ctx)
	if err != nil {
		return nil, err
	}
	return h3client.NewClientConn(conn, nil)
}

// Close gracefully stops the H3 server, closing listeners and active sessions.
func (s *H3TestServer) Close() error {
	s.closeOnce.Do(func() {
		s.isClosed.Store(true)
		s.cancel()

		// Snapshot active connections under lock to avoid holding the mutex during I/O
		s.mu.Lock()
		activeConns := make([]*quic.Conn, 0, len(s.activeConns))
		activeServerConns := make([]*machh3server.ServerConn, 0, len(s.activeConns))
		for conn, sc := range s.activeConns {
			activeConns = append(activeConns, conn)
			activeServerConns = append(activeServerConns, sc)
		}
		s.activeConns = make(map[*quic.Conn]*machh3server.ServerConn)
		s.mu.Unlock()

		// Send CONNECTION_CLOSE to all active connections BEFORE closing the UDP listener
		for i, conn := range activeConns {
			_ = activeServerConns[i].Close()
			_ = conn.CloseWithError(0x0100, "h3 server closing")
		}

		// Now tear down the UDP listener and wait for acceptLoop and Serve goroutines to exit
		_ = s.listener.Close()
		s.wg.Wait()
	})
	return nil
}

// AdaptHTTPHandlerToH3 adapts a standard net/http.Handler into a mach/server/h3.ServerHandlerFunc.
func AdaptHTTPHandlerToH3(h http.Handler) machh3server.ServerHandlerFunc {
	return func(req *machh3server.ServerRequest, res *machh3server.ServerResponse) error {
		reqURL, err := url.ParseRequestURI(req.Path)
		if err != nil {
			reqURL = &url.URL{Path: req.Path}
		}

		httpReq := &http.Request{
			Method:     req.Method,
			URL:        reqURL,
			Proto:      "HTTP/3.0",
			ProtoMajor: 3,
			ProtoMinor: 0,
			Header:     make(http.Header),
			Host:       req.Authority,
			RemoteAddr: req.RemoteAddr,
		}

		if req.Ctx != nil {
			httpReq = httpReq.WithContext(req.Ctx)
		}

		if len(req.Body) > 0 {
			httpReq.Body = io.NopCloser(bytes.NewReader(req.Body))
			httpReq.ContentLength = int64(len(req.Body))
		} else {
			httpReq.Body = http.NoBody
		}

		// Copy QPACK decoded headers
		for k, v := range req.Headers.All() {
			httpReq.Header.Add(k, v)
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httpReq)

		res.StatusCode = rec.Code
		for k, vals := range rec.Header() {
			for _, v := range vals {
				res.Headers.Add(k, v)
			}
		}
		res.Body = rec.Body.Bytes()
		return nil
	}
}

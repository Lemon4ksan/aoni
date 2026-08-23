// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

func handleHTTPProxyConn(ctx context.Context, srv *Server, conn net.Conn, br *bufio.Reader) error {
	req, err := http.ReadRequest(br)
	if err != nil {
		return err
	}

	defer func() {
		if req.Body != nil {
			_ = req.Body.Close()
		}
	}()

	if srv.Auth != nil && !checkHTTPProxyAuth(srv, req) {
		return sendHTTPProxyUnauthorized(conn)
	}

	if req.Method == http.MethodConnect {
		return handleHTTPConnect(ctx, srv, conn, req)
	}

	return handlePlainHTTPProxy(ctx, srv, conn, req)
}

func checkHTTPProxyAuth(srv *Server, req *http.Request) bool {
	authHeader := req.Header.Get("Proxy-Authorization")
	if !strings.HasPrefix(authHeader, "Basic ") {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
	if err != nil {
		return false
	}

	user, pass, ok := strings.Cut(bytesconv.B2S(decoded), ":")
	if !ok {
		return false
	}

	return srv.Auth(user, pass)
}

func sendHTTPProxyUnauthorized(conn net.Conn) error {
	resp := "HTTP/1.1 407 Proxy Authentication Required\r\n" +
		"Proxy-Authenticate: Basic realm=\"aoni inbound\"\r\n" +
		"Content-Length: 0\r\n\r\n"

	_, err := conn.Write([]byte(resp))

	return err
}

func handleHTTPConnect(ctx context.Context, srv *Server, conn net.Conn, req *http.Request) error {
	host, portStr, err := net.SplitHostPort(req.Host)
	if err != nil {
		host = req.Host
		portStr = "443"
	}

	port, _ := strconv.Atoi(portStr)

	if srv.EnableMITM && (port == 443 || port == 0) {
		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return err
		}

		br := bufio.NewReader(conn)

		return handleHTTP2TLSInterception(ctx, srv, conn, br, host, port)
	}

	outboundConn, err := netdial.DialL4(ctx, "tcp", net.JoinHostPort(host, portStr), netdial.DialOptions{})
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return err
	}
	defer outboundConn.Close()

	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return err
	}

	pipeConns(conn, outboundConn)

	return nil
}

func handlePlainHTTPProxy(ctx context.Context, srv *Server, conn net.Conn, req *http.Request) error {
	engine := resolveEngine(srv)

	resp, err := aoni.NewClient(engine).Request(ctx, req.Method, req.URL.String())
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return err
	}
	defer resp.Body.Close()

	return resp.Write(conn)
}

func handleHTTP2TLSInterception(
	ctx context.Context,
	srv *Server,
	conn net.Conn,
	_ *bufio.Reader,
	targetHost string,
	targetPort int,
) error {
	tlsCert, err := generateDynamicCert(srv, targetHost)
	if err != nil {
		return fmt.Errorf("%w: generate cert: %w", ErrMITMFailed, err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
		NextProtos:   []string{"http/1.1"},
	}

	tlsConn := tls.Server(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("%w: handshake: %w", ErrMITMFailed, err)
	}

	defer tlsConn.Close()

	tlsReader := bufio.NewReader(tlsConn)
	engine := resolveEngine(srv)
	client := aoni.NewClient(engine)

	for {
		interceptReq, err := http.ReadRequest(tlsReader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}

			return err
		}

		err = processInterceptedRequest(ctx, client, tlsConn, interceptReq, targetHost, targetPort)
		if err != nil || interceptReq.Close {
			return err
		}
	}
}

func processInterceptedRequest(
	ctx context.Context,
	client *aoni.Client,
	tlsConn net.Conn,
	interceptReq *http.Request,
	targetHost string,
	targetPort int,
) error {
	defer func() {
		if interceptReq.Body != nil {
			_ = interceptReq.Body.Close()
		}
	}()

	targetURL := &url.URL{
		Scheme:   "https",
		Host:     net.JoinHostPort(targetHost, strconv.Itoa(targetPort)),
		Path:     interceptReq.URL.Path,
		RawQuery: interceptReq.URL.RawQuery,
	}

	resp, err := client.Request(ctx, interceptReq.Method, targetURL.String())
	if err != nil {
		_, _ = tlsConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return err
	}
	defer resp.Body.Close()

	return resp.Write(tlsConn)
}

func generateDynamicCert(srv *Server, host string) (*tls.Certificate, error) {
	if cached, ok := srv.certCache.Load(host); ok {
		return cached.(*tls.Certificate), nil
	}

	var (
		cert *tls.Certificate
		err  error
	)

	if srv.RootCACert != nil {
		cert, err = generateCertFromRoot(srv, srv.RootCACert, host)
	} else {
		cert, err = generateSelfSignedCert(srv, host)
	}

	if err != nil {
		return nil, err
	}

	srv.certCache.Store(host, cert)

	return cert, nil
}

func getLeafKey(srv *Server) (*ecdsa.PrivateKey, error) {
	if srv.sharedLeafKey != nil {
		if k, ok := srv.sharedLeafKey.(*ecdsa.PrivateKey); ok {
			return k, nil
		}
	}

	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func generateSelfSignedCert(srv *Server, host string) (*tls.Certificate, error) {
	privKey, err := getLeafKey(srv)
	if err != nil {
		return nil, err
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  privKey,
	}, nil
}

func generateCertFromRoot(srv *Server, root *tls.Certificate, host string) (*tls.Certificate, error) {
	privKey, err := getLeafKey(srv)
	if err != nil {
		return nil, err
	}

	parentCert, err := x509.ParseCertificate(root.Certificate[0])
	if err != nil {
		return nil, err
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, parentCert, &privKey.PublicKey, root.PrivateKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{derBytes, root.Certificate[0]},
		PrivateKey:  privKey,
	}, nil
}

func resolveEngine(srv *Server) aoni.RequestDoer {
	if srv.Engine != nil {
		return srv.Engine
	}

	return aoni.NewClient(nil)
}

func pipeConns(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); _, _ = io.Copy(c1, c2) }()
	go func() { defer wg.Done(); _, _ = io.Copy(c2, c1) }()

	wg.Wait()
}

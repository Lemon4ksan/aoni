// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni"
)

const (
	// ConnectUDPUpgradeToken specifies the RFC 9298 HTTP upgrade token.
	ConnectUDPUpgradeToken = "connect-udp"

	// DefaultUDPPathPrefix specifies the RFC 9298 default well-known path for UDP proxying.
	DefaultUDPPathPrefix = "/.well-known/masque/udp/"
)

var bufioReaderPool = generic.NewPool(func() *bufio.Reader {
	return bufio.NewReaderSize(nil, 4096)
})

// BuildUDPProxyURI constructs an RFC 9298 compliant UDP Proxy URI.
//
// Preconditions:
//   - targetHost can be an IP address or domain name.
//   - targetPort must be a valid UDP port number (1-65535).
func BuildUDPProxyURI(host string, port int, targetHost string, targetPort int) string {
	return fmt.Sprintf("https://%s:%d%s%s/%d/", host, port, DefaultUDPPathPrefix, targetHost, targetPort)
}

// DialUDPProxy establishes a CONNECT-UDP proxying tunnel over HTTP/1.1 Upgrade or Extended CONNECT.
func DialUDPProxy(
	ctx context.Context,
	dialer aoni.WebSocketDialer,
	targetURL string,
	mods ...aoni.RequestModifier,
) (net.Conn, *http.Response, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidURITemplate, err)
	}

	host := parsed.Hostname()

	port := parsed.Port()
	if port == "" {
		port = "443"
	}

	addr := net.JoinHostPort(host, port)

	conn, err := dialer.DialTLSForWS(ctx, addr)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	req.Header.Set("Host", parsed.Host)
	req.Header.Set("Upgrade", ConnectUDPUpgradeToken)
	req.Header.Set("Connection", "Upgrade")

	for _, m := range mods {
		m.ApplyStd(req)
	}

	resp, err := performCONNECTUDPHandshake(ctx, conn, req)
	if err != nil {
		_ = conn.Close()
		return nil, resp, err
	}

	return conn, resp, nil
}

// performCONNECTUDPHandshake executes the HTTP upgrade request and validates the 101/200 response headers.
func performCONNECTUDPHandshake(
	ctx context.Context,
	conn net.Conn,
	req *http.Request,
) (*http.Response, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("aoni/masque: write udp request: %w", err)
	}

	br := bufioReaderPool.Get()
	if br == nil {
		br = bufio.NewReaderSize(conn, 4096)
	} else {
		br.Reset(conn)
	}

	defer func() {
		br.Reset(nil)
		bufioReaderPool.Put(br)
	}()

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("aoni/masque: read udp response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		return resp, ErrHandshakeFailed
	}

	return resp, nil
}

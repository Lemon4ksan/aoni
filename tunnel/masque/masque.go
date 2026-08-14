// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package masque provides utilities for bridging TUN adapters to MASQUE connect-ip sessions.
package masque

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/requestutil"
)

const (
	// DefaultMASQUEPathPrefix specifies the RFC 9484 default well-known path for IP proxying.
	DefaultMASQUEPathPrefix = "/.well-known/masque/ip/"

	// ConnectIPUpgradeToken specifies the RFC 9484 HTTP upgrade token.
	ConnectIPUpgradeToken = "connect-ip"
)

// BuildIPProxyURI constructs an RFC 9484 compliant IP Proxy URI using single-pass zero-allocation string builder.
// Target can be an IP address/prefix, hostname, or "*" wildcard; ipproto can be an IP protocol number or "*" wildcard.
func BuildIPProxyURI(host string, port int, target, ipproto string) string {
	cleanTarget := strings.TrimSpace(target)
	if cleanTarget == "" {
		cleanTarget = "*"
	}

	cleanProto := strings.TrimSpace(ipproto)
	if cleanProto == "" {
		cleanProto = "*"
	}

	var portBuf [10]byte

	portBytes := strconv.AppendInt(portBuf[:0], int64(port), 10)

	var sb strings.Builder
	sb.Grow(8 +
		len(host) + 1 +
		len(portBytes) +
		len(DefaultMASQUEPathPrefix) +
		len(cleanTarget)*3 + 1 +
		len(cleanProto) + 2,
	)

	sb.WriteString("https://")
	sb.WriteString(host)
	sb.WriteByte(':')
	sb.Write(portBytes)
	sb.WriteString(DefaultMASQUEPathPrefix)

	for i := 0; i < len(cleanTarget); i++ {
		b := cleanTarget[i]
		switch b {
		case ':':
			sb.WriteString("%3A")
		case '/':
			sb.WriteString("%2F")
		default:
			sb.WriteByte(b)
		}
	}

	sb.WriteByte('/')
	sb.WriteString(cleanProto)
	sb.WriteString("/")

	return sb.String()
}

// DialIPProxy establishes an IP tunneling connection over HTTP/1.1 or HTTP/2 Extended CONNECT.
func DialIPProxy(
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
	req.Header.Set("Upgrade", ConnectIPUpgradeToken)
	req.Header.Set("Connection", "Upgrade")

	for _, m := range mods {
		m.ApplyStd(req)
	}

	resp, err := performCONNECTIPHandshake(ctx, conn, req)
	if err != nil {
		_ = conn.Close()
		return nil, resp, err
	}

	return conn, resp, nil
}

// performCONNECTIPHandshake transmits the HTTP upgrade request and validates the 101/200 response headers.
func performCONNECTIPHandshake(
	ctx context.Context,
	conn net.Conn,
	req *http.Request,
) (*http.Response, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("aoni/masque: write request: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("aoni/masque: read response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		return resp, ErrHandshakeFailed
	}

	if resp.StatusCode == http.StatusSwitchingProtocols {
		if !tokenContainsValue(resp.Header, "Upgrade", ConnectIPUpgradeToken) ||
			!tokenContainsValue(resp.Header, "Connection", "upgrade") {
			return resp, ErrHandshakeFailed
		}
	}

	return resp, nil
}

// tokenContainsValue reports whether any header value for name matches target token.
func tokenContainsValue(header http.Header, name, value string) bool {
	return requestutil.HeaderContainsToken(header, name, value)
}

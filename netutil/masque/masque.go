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
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

const (
	// DefaultMASQUEPathPrefix specifies the RFC 9484 default well-known path for IP proxying.
	DefaultMASQUEPathPrefix = "/.well-known/masque/ip/"

	// ConnectIPUpgradeToken specifies the RFC 9484 HTTP upgrade token.
	ConnectIPUpgradeToken = "connect-ip"
)

// BuildIPProxyURI constructs an RFC 9484 compliant IP Proxy URI.
//
// Preconditions:
//   - target can be an IP address/prefix, hostname, or "*" wildcard.
//   - ipproto can be an IP protocol number (e.g. "17" for UDP, "6" for TCP) or "*" wildcard.
func BuildIPProxyURI(host string, port int, target, ipproto string) string {
	cleanTarget := strings.TrimSpace(target)
	if cleanTarget == "" {
		cleanTarget = "*"
	}

	cleanProto := strings.TrimSpace(ipproto)
	if cleanProto == "" {
		cleanProto = "*"
	}

	// Percent-encode colons for IPv6 literal targets per RFC 9484 Section 4.6
	if strings.Contains(cleanTarget, ":") {
		cleanTarget = strings.ReplaceAll(cleanTarget, ":", "%3A")
	}

	if strings.Contains(cleanTarget, "/") {
		cleanTarget = strings.ReplaceAll(cleanTarget, "/", "%2F")
	}

	return fmt.Sprintf("https://%s:%d%s%s/%s/", host, port, DefaultMASQUEPathPrefix, cleanTarget, cleanProto)
}

// DialIPProxy establishes an IP tunneling connection over HTTP/1.1 or HTTP/2 Extended CONNECT.
func DialIPProxy(
	ctx context.Context,
	dialer aoni.WSDialer,
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

	stdReq := aoni.NewStdRequest(req)
	for _, m := range mods {
		if m != nil {
			m(stdReq)
		}
	}

	resp, err := performCONNECTIPHandshake(ctx, conn, req)
	if err != nil {
		_ = conn.Close()
		return nil, resp, err
	}

	return conn, resp, nil
}

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
		return nil, fmt.Errorf("aoni masque: write request: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("aoni masque: read response: %w", err)
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

func tokenContainsValue(header http.Header, name, value string) bool {
	for _, s := range header[name] {
		for token := range strings.SplitSeq(s, ",") {
			if bytesconv.EqualFoldASCII(strings.TrimSpace(token), value) {
				return true
			}
		}
	}

	return false
}

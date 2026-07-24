// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"net"
	"strings"

	"golang.org/x/net/idna"
)

// CleanHost normalizes a host string for network resolution, HTTP headers, and TLS SNI.
//
// Postconditions:
//   - Strips IPv6 Zone IDs per RFC 6874 (e.g., "fe80::1%eth0" -> "fe80::1", "[fe80::1%eth0]" -> "[fe80::1]").
//   - Converts Internationalized Domain Names (IDN) to ASCII Punycode (e.g., "президент.рф" -> "xn--...").
func CleanHost(host string) string {
	if host == "" {
		return ""
	}

	// Handle bracketed IPv6 addresses (e.g., "[fe80::1%eth0]" -> "[fe80::1]")
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		inner := host[1 : len(host)-1]
		if zoneIdx := strings.IndexByte(inner, '%'); zoneIdx != -1 {
			inner = inner[:zoneIdx]
		}

		return "[" + inner + "]"
	}

	// Strip IPv6 Zone ID for unbracketed addresses (e.g., "fe80::1%eth0" -> "fe80::1")
	if zoneIdx := strings.IndexByte(host, '%'); zoneIdx != -1 {
		host = host[:zoneIdx]
	}

	if net.ParseIP(host) != nil {
		return host
	}

	if asciiHost, err := idna.Lookup.ToASCII(host); err == nil {
		return asciiHost
	}

	return host
}

// CleanHostPort splits addr into host and port, normalizes the host via [CleanHost],
// and returns the sanitized host and port components.
func CleanHostPort(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return CleanHost(addr), ""
	}

	return CleanHost(h), p
}

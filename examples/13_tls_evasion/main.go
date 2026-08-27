// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Full TLS evasion stack.
//
// Demonstrates combining all anti-detection features:
// WithTLSFingerprint, WithFragmentation, WithHostRewrite,
// WithDoTResolver, WithForceHTTP1, and WithOrderedHeaders.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/dns"
	"github.com/lemon4ksan/aoni/option"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
		// Chrome TLS fingerprint to match real browser behavior
		option.WithTLSFingerprint(aoni.BrowserChrome),
		// Fragment TLS ClientHello into smaller chunks
		option.WithFragmentation(aoni.FragmentConfig{
			ChunkSize: 50,
			MaxDelay:  5 * time.Millisecond,
		}),
		// Rewrite Host headers for domain fronting
		option.WithHostRewrite(map[string]string{
			"httpbin.org": "httpbin.org",
		}),
		// Use DNS-over-TLS resolver to prevent ISP DNS snooping
		option.WithDNSResolver(dns.NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")),
		// Force HTTP/1.1 to avoid HTTP/2 fingerprinting
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		}),
	)

	// Make a request with forced HTTP/1.1 and ordered headers
	res, err := client.GetTo[Response](ctx, "/ip",
		mod.WithForceHTTP1(),
		mod.WithOrderedHeaders([]string{
			fheader.Host,
			fheader.Connection,
			fheader.CacheControl,
			fheader.UpgradeInsecureRequests,
			fheader.UserAgent,
			fheader.Accept,
			fheader.SecFetchSite,
			fheader.SecFetchMode,
			fheader.SecFetchUser,
			fheader.SecFetchDest,
			fheader.AcceptEncoding,
			fheader.AcceptLanguage,
			fheader.Cookie,
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("TLS evasion request successful: %s\n", res.Origin)

	// Alternatively, force HTTP/2 for multiplexing
	_, _ = client.GetTo[Response](ctx, "/ip",
		mod.WithForceHTTP2(),
		mod.WithALPN(aoni.AlpnH2, aoni.AlpnHTTP),
	)

	fmt.Println("HTTP/2 with custom ALPN also supported")
}

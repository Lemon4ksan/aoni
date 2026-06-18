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

	"github.com/lemon4ksan/aoni"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		// Chrome TLS fingerprint to match real browser behavior
		WithTLSFingerprint(aoni.BrowserChrome).
		// Fragment TLS ClientHello into smaller chunks
		WithFragmentation(aoni.FragmentConfig{
			ChunkSize: 50,
			MaxDelay:  5 * time.Millisecond,
		}).
		// Rewrite Host headers for domain fronting
		WithHostRewrite(map[string]string{
			"httpbin.org": "httpbin.org",
		}).
		// Use DNS-over-TLS resolver to prevent ISP DNS snooping
		WithDoT("1.1.1.1", "cloudflare-dns.com").
		// Force HTTP/1.1 to avoid HTTP/2 fingerprinting
		WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		})

	// Make a request with forced HTTP/1.1 and ordered headers
	res, err := aoni.GetJSON[Response](ctx, client, "/ip",
		aoni.WithForceHTTP1(),
		aoni.WithOrderedHeaders([]string{
			"Host",
			"Connection",
			"Cache-Control",
			"Upgrade-Insecure-Requests",
			"User-Agent",
			"Accept",
			"Sec-Fetch-Site",
			"Sec-Fetch-Mode",
			"Sec-Fetch-User",
			"Sec-Fetch-Dest",
			"Accept-Encoding",
			"Accept-Language",
			"Cookie",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("TLS evasion request successful: %s\n", res.Origin)

	// Alternatively, force HTTP/2 for multiplexing
	_, _ = aoni.GetJSON[Response](ctx, client, "/ip",
		aoni.WithForceHTTP2(),
		aoni.WithALPN([]string{"h2", "http/1.1"}),
	)

	fmt.Println("HTTP/2 with custom ALPN also supported")
}

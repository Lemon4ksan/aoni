// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Proxy rotation with sticky sessions and retry middleware.
//
// Demonstrates NewProxyClient, NewProxyRotator, WithStickySessions,
// ProxyRetryCondition, and RetryMiddleware for resilient proxy rotation.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni"
)

type IPResponse struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create proxy clients from proxy URLs
	proxyURL1, _ := url.Parse("socks5://proxy1.example.com:1080")
	proxyURL2, _ := url.Parse("socks5://proxy2.example.com:1080")

	client1 := aoni.NewClient(nil).HTTP()
	client2 := aoni.NewClient(nil).HTTP()

	// Create a proxy rotator
	rotator, err := aoni.NewProxyRotator(
		aoni.ProxyRotatorConfig{
			MaxFails:   3,
			RetryAfter: 30 * time.Second,
		},
		aoni.ClientWithProxy{Client: client1, ProxyURL: proxyURL1.String()},
		aoni.ClientWithProxy{Client: client2, ProxyURL: proxyURL2.String()},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer rotator.Close()

	// Enable sticky sessions based on request host
	rotator.WithStickySessions(func(req *http.Request) string {
		return req.URL.Host
	})

	// Wrap with retry middleware that retries on proxy failures
	doer := aoni.Chain(
		rotator,
		aoni.RetryMiddleware(
			aoni.RetryOptions{MaxRetries: 3, Backoff: 1 * time.Second},
			aoni.ProxyRetryCondition(rotator),
		),
	)

	client := aoni.NewClient(doer).
		WithBaseURL("https://httpbin.org")

	// Make requests that will be load-balanced across proxies
	for i := range 3 {
		res, err := aoni.GetJSON[IPResponse](ctx, client, "/ip")
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}

		fmt.Printf("Request %d: proxied via %s\n", i, res.Origin)
	}
}

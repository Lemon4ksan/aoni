// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package aoni provides a lightweight, generic, and middleware-driven wrapper
around net/http with built-in support for proxy rotation, load balancing,
and robust request modification.

The package focuses on eliminating repetitive HTTP boilerplate by leveraging Go
generics for response decoding and utilizing a declarative "RequestModifier" pattern
for customizing individual requests.

# Key Features

  - Generics-First: Automatic JSON, XML, YAML, and Protobuf decoding via [GetJSON], [PostJSON], etc.
  - Request Modifiers: Chainable functions like [WithHeader], [WithMultipart], or [WithErrorModel]
    that modify outgoing requests in a thread-safe manner.
  - Proxy Rotation & Sticky Sessions: Distribute traffic across an unhealthy-aware proxy pool with
    optional session persistence (e.g. cookie or header based).
  - Load Balancing: Built-in [LoadBalancer] supporting Round-Robin, Random, and Weighted Round-Robin
    strategies with automatic background health checks.
  - Retry Engine: Highly configurable [RetryMiddleware] supporting custom retry conditions, exponential
    backoff, and randomized jitter.
  - Error Model Mapping: Extract structured error JSON into custom Go structs automatically when status
    codes are non-2xx.
  - Progress Tracking: Real-time upload and download progress reporting via simple progress callbacks.
  - Auto-UTF8 Translation: Automatic translation of non-UTF8 charsets (e.g., Windows-1251, Shift-JIS)
    based on Content-Type declarations.
  - Latency Hedging: Minimize long-tail latencies by optionally spawning parallel backup requests to
    multiple backends or proxies.
  - Network Tracing: Diagnostic tracing of DNS, TCP connect, and TLS handshake times, alongside equivalent
    cURL command generation.

# Basic Usage Example

	package main

	import (
		"context"
		"fmt"
		"net/http"
		"time"

		"github.com/lemon4ksan/g-man/pkg/aoni"
	)

	type TimeResponse struct {
		Time string `json:"time"`
	}

	func main() {
		ctx := context.Background()
		httpClient := &http.Client{Timeout: 10 * time.Second}
		client := aoni.NewClient(httpClient)

		// Perform a generic, type-safe GET request
		res, err := aoni.GetJSON[TimeResponse](ctx, client, "https://time.jsontest.com", nil)
		if err != nil {
			fmt.Println("Request failed:", err)
			return
		}
		fmt.Println("Server time:", res.Time)
	}
*/
package aoni

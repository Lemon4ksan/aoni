// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package aoni is a generic, middleware-driven HTTP client wrapper around
the standard net/http package. It is designed to simplify robust API
interactions, scraping, proxy management, and load balancing.

The package reduces repetitive HTTP boilerplate by leveraging Go generics
for response decoding and a declarative [RequestModifier] pattern
to configure individual requests.

# Architecture Overview

  - [Client]: The central, immutable client wrapping [HTTPDoer] with a fluent API.
  - [LoadBalancer]: A router that distributes requests across multiple backend servers
    supporting sequential, random, and weighted selection with automated health checking.
  - [ProxyRotator]: A proxy routing manager with support for sticky sessions, proxy pool
    rotation, and automatic health monitoring.
  - [Decoder]: An interface defining custom response deserialization strategies.

# Core Features

  - Generics-First API: Automatic decoding of response payloads into custom types
    via helper functions like [GetJSON], [PostJSON], and [PutJSON].
  - Extensible Request Modifiers: Chainable modifiers such as [WithHeader], [WithJSONBody],
    [WithMultipart], and [WithErrorModel] to customize outgoing requests.
  - Connection Resilience: Configurable [RetryMiddleware] supporting exponential backoff,
    randomized jitter, and custom retry conditions, alongside [CircuitBreakerMiddleware].
  - SSRF Protection: Optional SSRF protection via dialer-level IP filtering.
  - Latency Hedging: Secondary request spawning to minimize tail latencies under high load.
  - Content Handling: Automatic UTF-8 transcoding for non-UTF-8 character sets and
    unwrapping of multi-format compressions (gzip, brotli, zstd).
  - Transfer Progress: Up-to-the-byte progress tracking for uploads and downloads.
  - Diagnostic Tracing: Request timing tracking of DNS, TCP, and TLS metrics using [Trace].

# Basic Usage Example

	package main

	import (
		"context"
		"fmt"
		"log"
		"time"
		"github.com/lemon4ksan/g-man/pkg/aoni"
	)

	type TimeResponse struct {
		Time string `json:"time"`
	}

	func main() {
		ctx := context.Background()

		// Initialize client with fluent base URL and timeout configurations
		client := aoni.NewClient(nil).
			WithBaseURL("https://time.jsontest.com").
			WithTimeout(5 * time.Second)

		// Perform a generic, type-safe GET request
		res, err := aoni.GetJSON[TimeResponse](ctx, client, "/time")
		if err != nil {
			log.Fatalf("Request failed: %v", err)
		}

		fmt.Println("Server time:", res.Time)
	}
*/
package aoni

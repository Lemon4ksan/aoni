// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aoni provides a middleware-driven HTTP and WebSocket client built on
// top of [net/http]. It is designed for unreliable networks, aggressive
// firewalls, rotating proxy pools, and environments where deep-packet
// inspection must be evaded.
//
// Standard HTTP clients are built for stable internal services. aoni fills the
// gap when connections are unstable, proxies fail frequently, or transport
// fingerprints must match a specific browser.
//
// # Request Pipeline
//
// Every outgoing request passes through four stages:
//  1. [RequestModifier] chain - declarative header, query, and body setup.
//  2. [Middleware] chain - rate limiting, retries, circuit breaking, hedging.
//  3. Transport layer - TLS fingerprinting (uTLS), HTTP/3, proxy rotation,
//     TCP/IP spoofing.
//  4. Response decoding - automatic decompression (gzip, brotli, zstd),
//     charset transcoding, and structured binding via [Decoder].
//
// # Core Types
//
//   - [Client] is the central immutable HTTP client. Configuration is done via
//     functional options passed to [NewClient] (e.g. [WithClientBaseURL],
//     [WithClientTimeout]). Per-request overrides are applied via
//     [RequestModifier] values (e.g. [WithHeader], [WithQuery]).
//   - [LoadBalancer] distributes requests across multiple [Client] instances
//     with health checking and automatic failover.
//   - [ProxyRotator] manages a pool of proxy clients with sticky sessions,
//     fault tracking, and recovery.
//   - [Decoder] controls how response bodies are deserialized. Built-in
//     implementations: [JSONDecoder], [XMLDecoder], [YAMLDecoder], [RawDecoder].
//   - [ProxyIsolatedCookieJar] stores cookies separately per proxy URL to
//     prevent session leakage across different exit nodes.
//
// # Subpackages
//
//   - [github.com/lemon4ksan/aoni/ws] - WebSocket dialing over uTLS and
//     HTTP/2 Extended CONNECT ([RFC 8441]).
//   - [github.com/lemon4ksan/aoni/socketio] - Socket.IO v5 / Engine.IO v4
//     client with automatic reconnection and namespace multiplexing.
//   - [github.com/lemon4ksan/aoni/inspector] - Traffic inspector for
//     capturing and replaying HTTP exchanges.
//   - [github.com/lemon4ksan/aoni/ja4] - Pure-Go JA4/JA4H fingerprint
//     computation.
//   - [github.com/lemon4ksan/aoni/p0f] - TCP/IP stack fingerprint signatures.
//
// # Fingerprint Evasion
//
// aoni can make outbound connections appear as specific browsers:
//
//   - [WithClientTLSFingerprint] selects a uTLS ClientHello profile
//     (Chrome, Firefox, Safari) for JA3/JA4 matching.
//   - [WithClientP0fSignature] sets TTL, Don't Fragment, and TCP window
//     size to mimic an OS-level network stack.
//   - [WithClientModifiers] combined with [WithOrderedHeaders] controls the
//     HTTP/1.1 header serialization order. For HTTP/2, [FramedTransport]
//     reorders HPACK-encoded HEADERS frames.
//   - [WithClientFramedTransport] injects browser-specific SETTINGS and
//     PRIORITY frames into the HTTP/2 connection preface.
//
// # Resilience
//
//   - Hedging ([WithHedging]) sends a second request after a delay
//     if the first has not completed, cutting tail latency.
//   - [AdaptiveLimiter] dynamically adjusts concurrency based on observed
//     RTT, similar to the Vegas TCP congestion algorithm.
//   - [RetryMiddleware] retries failed requests with exponential backoff
//     and respects Retry-After headers.
//   - [CircuitBreaker] tracks per-host failures and stops sending requests
//     until the host recovers.
//
// # Network Protocols
//
//   - HTTP/3 over QUIC via [WithClientHTTP3].
//   - DNS-over-HTTPS ([DoHResolver]) and DNS-over-TLS ([DoTResolver])
//     bypass local ISP DNS interception.
//   - WebSocket via uTLS with HTTP/2 Extended CONNECT: see [github.com/lemon4ksan/aoni/ws].
//
// # Basic Usage
//
//	client := aoni.NewClient(nil,
//		aoni.WithClientBaseURL("https://api.example.com"),
//		aoni.WithClientTimeout(10*time.Second),
//	)
//
//	// 1. Get structured data with generics:
//	user, err := aoni.GetTo[User](ctx, client, "/users/123")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// 2. Or execute raw requests directly via convenience methods:
//	resp, err := client.Get(ctx, "/raw-data")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer resp.Body.Close()
//
// The full example directory contains runnable programs for each feature.
package aoni

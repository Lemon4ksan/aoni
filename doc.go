// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aoni provides a unified, high-performance Internet Protocol engine for Go.
//
// It consolidates IETF RFC standards, W3C specifications, and Chromium-grade network resilience
// mechanisms into a single, profile-driven zero-allocation architecture.
//
// # Dual Engines
//
// aoni provides two execution engines under a unified conceptual framework:
//   - [Client] - 100% net/http compatibility, full middleware chain support, and seamless ecosystem integration.
//   - [github.com/lemon4ksan/aoni/fast.Client] - Native fasthttp + H2/H3 engine built for extreme throughput
//     and zero heap allocations under parallel I/O.
//
// # Request Pipeline
//
// Every outgoing request passes through five stages:
//  1. RequestModifiers ([mod]) - declarative header, query, context, and body setup.
//  2. Middleware chain ([middleware]) - rate limiting, retries, circuit breaking, speculative hedging.
//  3. Execution Engine ([aoni.Client] or [fast.Client]) - protocol dispatch, pool janitors, Alt-Svc cache.
//  4. Transport layer ([internal/transport]) - uTLS browser fingerprinting, HTTP/3 QUIC, proxy rotation,
//     Happy Eyeballs v3 protocol racing, p0f OS stack spoofing.
//  5. Response decoding ([codec]) - automatic decompression (gzip, brotli, zstd),
//     charset transcoding, and structured binding via generic decoders.
//
// # Domain Architecture & Subpackages
//
//   - [github.com/lemon4ksan/aoni/option] - Functional client configuration options (e.g. WithProxy, WithTimeout, WithChrome).
//   - [github.com/lemon4ksan/aoni/mod] - Per-request modifiers (e.g. WithJSONBody, WithHeader, WithQuery).
//   - [github.com/lemon4ksan/aoni/fluent] - Chainable Request Builder API (e.g. fluent.FetchTo[T]).
//   - [github.com/lemon4ksan/aoni/request] - Generic single-line execution helpers (e.g. request.GetTo[T]).
//   - [github.com/lemon4ksan/aoni/fast] - Ultra-high-throughput fasthttp + H2/H3 client facade.
//   - [github.com/lemon4ksan/aoni/grpc] - Native gRPC client (Invoke, ServerStream, DynamicInvoker).
//   - [github.com/lemon4ksan/aoni/cookie] - Proxy-isolated cookie jars (ProxyIsolatedJar, SQLStorage, JSONFileStorage).
//   - [github.com/lemon4ksan/aoni/codec] - Unified response body decoders and struct-to-values encoders.
//   - [github.com/lemon4ksan/aoni/fingerprint] - TLS/JA4/p0f evasion, browser profiles, and personas.
//   - [github.com/lemon4ksan/aoni/resiliency] - Response caching, load balancing, circuit breakers, WAF challenge solvers.
//   - [github.com/lemon4ksan/aoni/realtime] - WebSockets over H2 Extended CONNECT (RFC 8441), SSE, and NDJSON streams.
//   - [github.com/lemon4ksan/aoni/telemetry] - HAR generators, EWMA latency trackers, embedded web inspector dashboard.
//   - [github.com/lemon4ksan/aoni/tunnel] - MASQUE HTTP CONNECT-UDP tunnels and TUN adapter bindings.
//   - [github.com/lemon4ksan/aoni/x] - Supplementary protocols and vendor database connectors (Socket.IO, GeoIP).
//
// # The "aoni v1" Compatibility & Forever-Frozen Core Manifesto
//
// "Code written against aoni v1.0.0 is guaranteed to compile and execute without modifications
// on any v1.x version 5, 10, and 20 years from now. The entire core is permanently frozen.
// All experiments, proprietary protocols, and shifting vendor specifications live strictly in aoni/x/... packages."
//
// # Three Tiers of Usage (Zero-Friction DX)
//
// aoni is designed for maximum simplicity for newcomers while supporting extreme customization for high-load systems:
//
//  1. Single-Line Zero Config (Complete Beginner):
//     Fetch and decode JSON/XML payloads into a typed struct with zero boilerplate or client setup:
//
//     user, err := request.GetTo[User](ctx, nil, "https://api.github.com/users/octocat")
//
//  2. Production-Grade Stealth Client (Standard App):
//     Configure TLS browser impersonation, base URLs, timeouts, and proxy rotators:
//
//     client := aoni.NewClient(option.WithChrome(), option.WithTimeout(10*time.Second))
//     user, err := request.GetTo[User](ctx, client, "/users/123")
//
//  3. Extreme RPS Engine (High-Throughput 1.5M RPS):
//     Use [github.com/lemon4ksan/aoni/fast.Client] for zero-allocation performance:
//
//     fastClient := fast.NewClient()
//     resp, err := fastClient.Get(ctx, "/users/123")
//     defer resp.Close()
//
// # Memory Management & Error Handling
//
//   - **Fast Engine Memory**: Calls via [github.com/lemon4ksan/aoni/fast] return pooled responses. Callers MUST invoke `resp.Close()` to release pooled memory.
//
//   - **Error Handling**: Non-2xx HTTP responses return an [*APIError] containing the status code and raw response body:
//
//     var apiErr *aoni.APIError
//     if errors.As(err, &apiErr) {
//     log.Printf("HTTP Error %d: %s", apiErr.StatusCode, string(apiErr.Body))
//     }
//
// # Basic Usage
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"time"
//
//		"github.com/lemon4ksan/aoni"
//		"github.com/lemon4ksan/aoni/option"
//		"github.com/lemon4ksan/aoni/request"
//	)
//
//	type User struct {
//		ID   int    `json:"id"`
//		Name string `json:"name"`
//	}
//
//	func main() {
//		// Zero-config 1-line GET request
//		user, err := request.GetTo[User](context.Background(), nil, "https://api.github.com/users/octocat")
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		fmt.Printf("User: %s (ID: %d)\n", user.Name, user.ID)
//	}
package aoni

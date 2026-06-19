// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aoni is a generic, middleware-driven HTTP and WebSocket client wrapper
// around the standard net/http package, specifically engineered for chaotic,
// unstable, and highly defensive network environments.
//
// It operates under the philosophy: "In networks, chaos is the default. Let aoni
// be your ice-cold anchor." While standard clients are optimized for predictable
// internal cloud microservices, aoni serves as tactical "off-road armor" for
// uncooperative targets—handling aggressive firewalls, rotating unstable proxy pools,
// and evading deep-packet inspection (DPI).
//
// # Pipeline Philosophy
//
// HTTP requests in aoni are modeled as fluid pipeline streams processed in four distinct phases:
// 1. RequestModifiers: Declarative request decoration (Headers, URL Variables, Decoders, Body).
// 2. Middlewares: Interception and control (Rate limiters, Concurrency, Circuit Breakers, Retries).
// 3. Transport Layer: Multi-protocol execution (uTLS, HTTP/3, p0f Spoofing, ProxyRotator, LoadBalancer).
// 4. Generic Decoders: Output binding with automated UTF-8 transcoding and multi-format decompression (Gzip, Brotli, Zstd).
//
// # Architecture Overview
//
//   - [Client]: The central, immutable client wrapping [HTTPDoer] with a fluent API.
//     Every configuration call clones the client, maintaining thread-safety.
//   - [LoadBalancer]: A failover router distributing requests across backends with health checking,
//     prewarming, and structured slog-compatible event logging.
//   - [ProxyRotator]: A proxy pool router supporting sticky sessions, proxy connection fault metrics,
//     and automatic failover recovery with detailed logs.
//   - [Decoder]: An interface defining response deserialization strategies (JSON, XML, YAML, Raw).
//   - [ProxyIsolatedCookieJar]: An isolated cookie sandbox grouping cookie storage by active proxy URL
//     to prevent session leakage and fingerprint anomalies.
//
// # Deep Evasion & Fingerprinting
//
//   - JA4+ Tracing: Real-time fingerprinting tracking TLS client (JA4) and HTTP request (JA4H)
//     structures during live transactions.
//   - uTLS Integration: Custom TLS ClientHello emulation matching specific browser engines (Chrome, Firefox, Safari).
//   - TCP/IP p0f Spoofing: Low-level socket control mimicking target OS network stacks (TTL, Don't Fragment, Window Size).
//   - Header Ordering: Custom HTTP/1.1 header sequence preservation to match browser-specific HTTP signatures.
//   - Custom HTTP/2 Settings: Fine-tuning of h2 parameters (MaxHeaderListSize, InitialWindowSize) to prevent transport mismatches.
//
// # Resilience & Chaos Control
//
//   - Latency Hedging: Dual concurrent request dispatch to mitigate tail (p99) latencies under high load.
//   - Vegas Adaptive Concurrency: Dynamic concurrency limits based on Vegas TCP-style round-trip time (RTT) queuing.
//   - Circuit Breakers: Independent host circuit-health tracking preventing cascading downstream blockages.
//   - Robust Retry Engine: Retries with exponential backoff, custom jitter, and out-of-the-box policies
//     for transient errors, gateway issues, and 429 Rate Limits (respecting Retry-After).
//
// # Modern Network Protocols
//
//   - HTTP/3 (QUIC): Built-in transport support for UDP-based connections via http3.Transport.
//   - DNS-over-HTTPS & DNS-over-TLS: Native DoH/DoT resolvers bypassing local ISP DNS interception.
//   - In-Memory DNS Caching: Custom TTL-based DNS caching avoiding redundant DNS query overhead.
//   - WebSocket over H2/uTLS: Native WebSocket dialing using uTLS/JA4 fingerprinting and
//     modern HTTP/2 Extended CONNECT (RFC 8441) tunnels.
//
// # Automation Session Sync
//
//   - Headless Browser Sync: Native ExportCookies and ImportCookies functions to translate Cookie Jars
//     into flat, serialization-ready structures for Playwright or Chromedp.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"time"
//		"github.com/lemon4ksan/aoni"
//	)
//
//	type TimeResponse struct {
//		Time string `json:"time"`
//	}
//
//	func main() {
//		ctx := context.Background()
//
//		// Initialize client with fluent base URL and timeout configurations
//		client := aoni.NewClient(nil).
//			WithBaseURL("https://time.jsontest.com").
//			WithTimeout(5 * time.Second)
//
//		// Perform a generic, type-safe GET request
//		res, err := aoni.GetJSON[TimeResponse](ctx, client, "/time")
//		if err != nil {
//			log.Fatalf("Request failed: %v", err)
//		}
//
//		fmt.Println("Server time:", res.Time)
//	}
package aoni

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package ws implements a resilient, browser-grade WebSocket client built on
// top of the uTLS/JA4 network evasion pipeline of the aoni engine.
//
// Unlike traditional WebSocket libraries built for stable internal infrastructures,
// this package is optimized to navigate restrictive network boundaries, deep-packet
// inspection (DPI) firewalls, and complex rotating proxy environments.
//
// # Handshake Fingerprint Evasion
//
// Outbound encrypted WebSocket handshakes (wss://) automatically inherit the target
// browser profile variants (Chrome, Firefox, Safari) configured on the [aoni.Client].
// Handshakes are executed with custom uTLS ClientHello specifications, TLS session ticket
// insulation, and case-sensitive HTTP header sequence serialization.
//
// # HTTP/2 Extended CONNECT (RFC 8441)
//
// The package automatically negotiates Application-Layer Protocol Negotiation (ALPN)
// during TLS. If the server advertises support for HTTP/2 and the CONNECT protocol,
// the client automatically bootstraps the WebSocket over a single, multiplexed HTTP/2
// stream using Extended CONNECT rather than falling back to classic HTTP/1.1 Upgrade
// pathways. This significantly reduces socket and handshake overhead on the backend.
//
// # Transport & Telemetry Integration
//
// All dialing functions natively route through the client's configured networking
// layers, bringing full support for SOCKS5/HTTP Connect proxy failover, source IP
// pool rotation, Happy Eyeballs stagger delay, and SSRF guards.
// Using [aoni.TraceJA4] as a request modifier enables capturing both the low-level TLS (JA4)
// and HTTP handshake (JA4H) fingerprints negotiated during connection setup.
//
// # Connection Contract (Conn)
//
// The connection returned by [DialWebSocket] and [DialWebSocketWithConfig] satisfies the
// [Conn] interface, which extends standard [net.Conn]. This ensures complete compatibility
// with Go's standard networking abstractions while adding direct methods like [Conn.ReadMessage]
// and [Conn.WriteMessage] for WebSocket text/binary frame operations.
//
// # Example
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
//		"github.com/lemon4ksan/aoni/mod"
//		"github.com/lemon4ksan/aoni/option"
//		"github.com/lemon4ksan/aoni/ws"
//	)
//
//	func main() {
//		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//		defer cancel()
//
//		// Initialize a browser-grade client with Chrome TLS fingerpints and proxy routing
//		client := aoni.NewClient(nil,
//			option.WithTLSFingerprint(aoni.BrowserChrome),
//			option.WithProxyString("socks5://127.0.0.1:1080"),
//		)
//
//		// Dial the WebSocket target using uTLS Chrome hello specs
//		conn, resp, err := ws.DialWebSocket(ctx, client, "wss://echo.websocket.org",
//			mod.WithHeader("Origin", "https://echo.websocket.org"),
//		)
//		if err != nil {
//			log.Fatalf("WebSocket connection failed: %v", err)
//		}
//		defer conn.Close()
//
//		fmt.Println("Handshake status:", resp.Status)
//
//		// Send data over standard net.Conn contract
//		payload := []byte("hello from secure socket")
//		_, _ = conn.Write(payload)
//
//		buf := make([]byte, 1024)
//		n, _ := conn.Read(buf)
//		fmt.Printf("Received echo: %s\n", string(buf[:n]))
//	}
package ws

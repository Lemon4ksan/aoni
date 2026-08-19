// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socketio provides support for working with Socket.IO v5 and Engine.IO v4 protocols.
//
// It is designed for high-throughput, low-latency client connections over WebSockets,
// featuring event-driven architectures, structured namespace multiplexing, bidirectional acknowledgments,
// and automatic binary segment reconstructions.
//
// # Evasion & Security Invariants
//
// Unlike naive Socket.IO client libraries that fail under DPI or anti-bot blocks,
// the socketio package routes connection handshakes through the parent [aoni.Client]'s uTLS
// and HTTP/2 extended connect pipelines. This ensures that:
//
//  1. Transport Fingerprints: The initial WebSocket handshake carries the exact JA3/JA4,
//     HTTP/2 settings, and user agent rotation profile matching the designated browser emulation.
//  2. Proxy-Isolated Session Caches: TLS session ticket caches are automatically isolated
//     and partitioned per proxy server, preventing exit-node correlation tracking.
//
// # Connection Resilience
//
// Connection states are managed by a strictly typed Finite State Machine (FSM).
// Reconnection loops integrate adaptive exponential backoff algorithms with randomized jitter,
// and automatically resume namespace states and session IDs upon network recoveries.
//
// # Basic Usage
//
//	sio, err := socketio.DialSocketIO(ctx, client, "wss://realtime.example.com/socket.io/", socketio.Config{
//		Namespace: "/chat",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer sio.Close()
//
//	sio.On("message", func(args []json.RawMessage) {
//		fmt.Println("Received:", string(args[0]))
//	})
//
//	err = sio.Emit("message", "hello from client")
package socketio

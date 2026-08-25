// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package webtransport implements the next-generation W3C and IETF WebTransport over HTTP/3 protocol
// (RFC 9114, RFC 9297, RFC 9220, RFC 9221, and draft-ietf-webtrans-http3-16).
//
// WebTransport is the official successor to WebSockets for real-time bidirectional communication over
// the web. Unlike traditional WebSockets running over single TCP or HTTP/2 streams that suffer from
// Head-of-Line Blocking, WebTransport leverages QUIC and HTTP/3 to multiplex:
//
// # Unreliable Datagrams (RFC 9221 & RFC 9297 §2.1 / draft-16 §4.5)
//
// Out-of-order, fire-and-forget datagram transmission without retransmissions or HoL blocking, ideal for
// live audio/video streaming, gaming, telemetry, and real-time AI voice streaming.
//
// # Multiple Multiplexed Streams (RFC 9114 & draft-16 §4.2/§4.3)
//
// Parallel bidirectional (WT_STREAM 0x41) and unidirectional (0x54) streams multiplexed within a single session.
// Packet loss on one stream does not stall or delay data transmission on other streams.
//
// # Extended CONNECT Bootstrapping (RFC 9220 & draft-16 §3.2)
//
// WebTransport sessions are established using HTTP/3 Extended CONNECT with pseudo-header ':protocol = webtransport-h3'
// and SETTINGS negotiation ('SETTINGS_ENABLE_CONNECT_PROTOCOL = 0x08', 'SETTINGS_H3_DATAGRAM = 0x33',
// 'SETTINGS_WT_ENABLED = 0x2c7cf000', 'SETTINGS_WT_INITIAL_MAX_DATA = 0x2b61').
//
// # Capsule Protocol (RFC 9297 §3.2 & draft-16 §4.7, §5.6, §6)
//
// In-band session management via bidirectional Capsule frames:
//   - CLOSE_WEBTRANSPORT_SESSION (0x2843): communicates 32-bit application error codes and UTF-8 diagnostic messages (max 1024 bytes).
//   - DRAIN_WEBTRANSPORT_SESSION (0x78ae): signals graceful drainage without opening new streams.
//   - WT_MAX_STREAMS (0x190b4d3f / 0x190b4d40) & WT_MAX_DATA (0x190b4d3d): session-level flow control.
//
// # Zero-Allocation Architecture
//
// All stream framing, datagram Quarter-Stream-ID encapsulation, and capsule codecs operate in 0 B/op
// zero-allocation mode using pooled stack and slab buffers.
//
// # Example Usage
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"time"
//
//		"github.com/lemon4ksan/aoni/realtime/webtransport"
//	)
//
//	func main() {
//		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//		defer cancel()
//
//		sess, err := webtransport.Dial(ctx, "https://echo.webtransport.day:443/echo")
//		if err != nil {
//			log.Fatalf("WebTransport dial failed: %v", err)
//		}
//		defer sess.Close()
//
//		// Send an unreliable datagram
//		if err := sess.SendDatagram([]byte("realtime telemetry payload")); err != nil {
//			log.Printf("Send datagram failed: %v", err)
//		}
//
//		// Open a parallel bidirectional stream
//		stream, err := sess.OpenStreamSync(ctx)
//		if err != nil {
//			log.Fatalf("OpenStream failed: %v", err)
//		}
//		defer stream.Close()
//
//		_, _ = stream.Write([]byte("hello over multiplexed webtransport stream"))
//	}
package webtransport

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides an ultra-high-performance multi-protocol HTTP client facade.
//
// Built directly on top of fasthttp, uTLS, H2, and H3 engines, it is optimized for high-concurrency
// throughput workloads (1.5M+ RPS) with strict zero heap allocations in hot paths.
//
// # Key Features
//
//   - [Client]: Ultra-fast multi-protocol client wrapping fasthttp and internal H2/H3 engines.
//   - Zero-Allocation Hot Path: Uses pooled request/response objects ([NewRequest], [NewResponse])
//     and ring buffer dispatchers to eliminate garbage collector pressure.
//   - Chromium-Grade Evasion: Natively integrates uTLS ClientHello specifications, p0f OS stack spoofing,
//     and Happy Eyeballs v3 protocol racing.
//
// # Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//
//		"github.com/lemon4ksan/aoni/fast"
//	)
//
//	func main() {
//		client := fast.NewClient()
//		defer client.Close()
//
//		req := fast.NewRequest(nil)
//		defer req.Release()
//
//		req.SetURL("https://httpbin.org/get")
//
//		resp, err := client.Do(req)
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer resp.Close()
//
//		fmt.Printf("Status: %d, Body: %s\n", resp.StatusCode(), resp.BodyString())
//	}
package fast

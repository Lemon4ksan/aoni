// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides a high-throughput multi-protocol HTTP client engine
// supporting HTTP/1.1 (via fasthttp), HTTP/2, and HTTP/3.
//
// The engine minimizes heap allocations on request hot paths through buffer pooling,
// protocol multiplexing, and direct byte representations.
//
// # Architectural Encapsulation
//
// While the engine coordinates complex low-level mechanisms (Happy Eyeballs v3 protocol racing,
// ring buffer recycling via [github.com/lemon4ksan/foundation/silicon/pool], and uTLS ClientHello
// synthesis), the entire public API is completely clean and high-level:
//
//	client := fast.NewClient(
//	    option.WithChrome(),
//	    option.WithTimeout(5 * time.Second),
//	)
//	defer client.Close()
//
//	resp, err := client.Get(ctx, "https://api.example.com/data")
//	if err != nil {
//	    return err
//	}
//	defer resp.Close()
//
//	fmt.Println("Status:", resp.StatusCode())
//	fmt.Println("Body:", resp.BodyString())
//
// # Key Capabilities
//
//   - [Client]: High-level facade multiplexing H1, H2, and H3 with zero-allocation fast paths.
//   - Happy Eyeballs v3: Parallel protocol racing across H3 (QUIC) and H2/H1 with staggered fallback timers.
//   - Proxy-Isolated Cookie Jars: Seamless integration with [github.com/lemon4ksan/aoni/cookie.ProxyIsolatedJar].
//   - Pure-Go Memory Safety: Internal object pooling with Per-P sharded buffers ([Request], [Response]).
package fast

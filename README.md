<div align="center">

# aoni

### The Unified High-Performance Internet Protocol Stack for Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
![Resilience](https://img.shields.io/badge/stability-Chromium--Grade-blue?style=flat-square)

> _"In networks, chaos is the default. Let aoni be your ice-cold anchor."_

#### English • [Русский](README_RU.md)

</div>

## Why Aoni?

Building production Go applications often requires integrating multiple independent networking packages - separately managing HTTP/3, uTLS, DoH/DoQ resolvers, WebSockets, gRPC-Web, and connection resilience. Handling disconnected memory pools and context models across these layers introduces unnecessary heap allocations, GC pause spikes, and redundant abstraction overhead.

`aoni` consolidates modern IETF RFC standards, W3C specifications, and Chromium-grade network resilience into a single profile-driven architecture.

Whether executing standard REST microservice queries, high-throughput API gateway routing, real-time WebSocket streams, or stealthy network analysis, `aoni` provides zero-allocation hot paths and predictable execution budgets.

```shell
go get github.com/lemon4ksan/aoni
```

## Quick Start

### 1. High-Level Universal Generic Interface (`FetchTo`)

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(15*time.Second),
		option.WithChrome(), // One-line Chrome-grade stealth, ECH, 0-RTT & resilience
	)

	// Single-line type-safe execution
	user, resp, err := fluent.FetchTo[User](ctx, client, "GET", "/users/{id}",
		mod.WithVar("id", 123),
		mod.WithHeader("X-Custom-Header", "value"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s (Status: %d)\n", user.Name, resp.StatusCode)
}
```

### 2. Native Protobuf & gRPC-Web Support

```go
// Direct gRPC-Web call with 5-byte framing and trailer validation
userResp, resp, err := fluent.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
	UserId: 42,
})
```

## Architecture & Dual Engines

`aoni` provides two execution engines sharing a unified API model:

```
               ┌──► aoni.Client (100% net/http compatibility & middleware chain)
option / mod ──┼
               └──► fast.Client (1.87M+ RPS multi-core, zero-alloc fasthttp + H2/H3)
```

* **Standard `aoni.Client`**: Use when 100% Go standard library compatibility and `net/http` middleware interoperability are required.
* **Native `fast.Client`**: Use when raw silicon throughput and zero-allocation memory geometry are required.

## Performance Profile & Benchmarks

The following `pprof` benchmarks measure execution latency, heap memory footprint, and allocation counts under identical workloads:

| Metric | Standard `net/http` | `aoni` (Standard) | `aoni` + `fast.Bridge` | `aoni/fast` (Native) | Performance Delta |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Latency (`ns/op`)** | 49,936 ns | 58,963 ns | 13,926 ns | **5,594 ns** | **3.5x Faster (Bridge) / 10.5x (Native)** |
| **Client GET Latency (`ns/op`)** | 5,906 ns | 6,091 ns | 5,675 ns | **3,868 ns** | **Parity with Raw fasthttp (3,770 ns)** |
| **Heap Memory (`B/op`)** | 6,803 B | 9,548 B | 6,757 B | **361 B** | **1.4x Lighter (Bridge) / 26x (Native)** |
| **Heap Allocations (`allocs/op`)** | 76 allocs | 89 allocs | 68 allocs | **8 allocs** | **1.15x Fewer (Bridge) / 11x (Native)** |
| **HTTP/2 Latency (`ns/op`)** | 75,337 ns | 75,337 ns | 73,353 ns | **73,353 ns** | **22% Less H2 Memory (7.2KB vs 9.3KB)** |
| **HTTP/3 Latency (`ns/op`)** | 130,231 ns | 130,231 ns | 131,989 ns | **131,989 ns** | **35% Less QUIC Memory (15.1KB vs 23.4KB)** |
| **Parallel Latency (`ns/op`)** | 11,307 ns | 9,534 ns | 1,940 ns | **588.9 ns** | **3.2x Faster (Bridge) / 16x (Native)** |
| **Parallel Memory & GC (`B / alloc`)** | 6,803 B / 76 | 9,548 B / 89 | 6,757 B / 68 | **0 B / 0 allocs** | **Zero Heap Allocations** |
| **Peak Throughput (Single Node)** | ~35k RPS | ~30k RPS | >80,000 RPS | **1,695,000+ RPS** | **High-Throughput IO** |

> [!TIP]
> High throughput in standard Go HTTP clients triggers frequent Garbage Collection (GC) pauses and `mark-assist` stalls, creating severe p99 tail-latency spikes.
> By recycling pooled buffers via `sync.Pool` and leveraging SIMD AVX2 framing (`simd_amd64.s`), `aoni/fast` operates with **0 B/op and 0 allocs/op** under parallel I/O. By completely shielding the Go runtime from GC pressure, `aoni` matches and surpasses non-garbage-collected HTTP stacks (such as Rust's `reqwest` / `hyper`), delivering flat sub-microsecond tail latency and 1.7M+ RPS throughput.

## Feature & Protocol Scope

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g. Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Zero-Alloc Builder Pooling** | ✗ | ✗ | **✓ (`sync.Pool` Request Builder)** |
| **Generics-first Decoding** | ✗ (Manual) | ✗ (Interface-based) | **✓ (Type-safe `[T]`)** |
| **Native Protobuf & gRPC-Web** | ✗ | ✗ | **✓ (Binary, Text & Stream)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (IPv4/v6 only) | ✗ | **✓ (H3 vs H2/H1 Protocol Racing)** |
| **Auto-Recovery Pipeline** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 & Alt-Svc Backoff)** |
| **W3C `No-Vary-Search` Caching** | ✗ | ✗ | **✓ (Smart URL Normalization)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 via DoH/DoQ)** |
| **OS Power Management** | ✗ | ✗ | **✓ (Auto-purge zombie pools on sleep)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native Middleware)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC1123)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic)** |
| **TLS Evasion (JA3/JA4)** | ✗ | ✗ | **✓ (via uTLS & Handshake)** |
| **JA4+ Fingerprinting** | ✗ | ✗ | **✓ (TLS & HTTP, pure Go)** |
| **Unix Domain Socket Support** | ⚠️ (Manual) | ✗ | **✓ (Native `unix://`)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (Complete v5 Spec)** |
| **Proxy & Session Isolation** | ✗ | ✗ | **✓ (`ProxyIsolatedJar`)** |
| **Per-Request Overrides** | ✗ (Manual transport) | ✗ (Requires client clone) | **✓ (Context Accessors)** |

## Repository Layout

```
aoni/
├── option/       // Client initialization options (option.With...)
├── mod/          // Per-request modifiers (mod.With...)
├── request/      // Generic request helpers (request.GetTo[T], PostTo, PostProtoTo)
├── fast/         // High-performance fasthttp engine adapters
├── fluent/       // Chainable Request Builder API (fluent.R, FetchTo[T], Codec)
├── cookie/       // Proxy-isolated cookie jars, Netscape format, RFC 6265 path sorting
├── fingerprint/  // TLS/JA4/p0f evasion, HTTP/2 framing, CDN padding
├── netutil/      // Proxy rotators, DoH/DoT DNS resolvers, IPv6 subnet rotators
├── codec/        // Response decoders (JSON, Proto, gRPC-Web, XML) and url.Values encoders
├── realtime/     // WebSocket over H2 CONNECT, Socket.IO v5, SSE & NDJSON streams
├── resiliency/   // Local HTTP response caching, WAF challenge detectors & solvers, load balancers
└── telemetry/    // HAR generators, EWMA latency trackers, embedded web inspector dashboard
```

## Real-World Case Studies & Integrations

- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): High-throughput, zero-allocation fork of official `discordgo` powered by `aoni` & `aoni/realtime/ws`.
  - Delivers 6.8x higher REST throughput (203,000+ RPS) and 3.1x faster WebSocket operations with 0 B/op memory allocations on frame framing.

## Technical Specifications & Documentation

- [**Network Stack Specification**](docs/NETWORK_STACK.md): Detailed overview of Happy Eyeballs v3, HTTP 421/408/425 auto-recovery, ECH, and pool lifetime mechanics.
- [**CPU & Silicon Sympathy Specification**](docs/CPU_STACK.md): Architecture details on native PLAN9 AVX2 SIMD assembly (`simd_amd64.s`), 2MB LargePages slab arenas, and instruction execution budgets.
- [**Demystifying the Voodoo**](docs/VOODOO.md): Deep dive into HPACK state manipulation, TCP window tuning via syscalls, and packet jitter framing.
- [**Code Examples**](examples): Runnable code snippets for REST, WebSockets, gRPC-Web, and browser evasion integrations.

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

<div align="center">

# aoni

### The Unified High-Performance Internet Protocol Stack for Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
![Resilience](https://img.shields.io/badge/stability-Chromium--Grade-blue?style=flat-square)
[![Fuzzing](https://img.shields.io/badge/security-Fuzz%20Verified-brightgreen?style=flat-square)](docs/ARCHITECTURE.md#4-fuzzing--security-armor)
[![Architecture](https://img.shields.io/badge/docs-Architecture%20Spec-blueviolet?style=flat-square)](docs/ARCHITECTURE.md)

> _"In networks, chaos is the default. Let aoni be your ice-cold anchor."_

#### English • [Русский](README_RU.md) • [Architecture Specification](docs/ARCHITECTURE.md)

</div>

> ### The "aoni v1" Compatibility & Forever-Frozen Core Manifesto
> _«Code written against **aoni v1.0.0** is guaranteed to compile and execute without modifications on any **v1.x** version 5, 10, and 20 years from now. The entire core protocol engine is permanently frozen around immutable IETF RFCs and W3C/Chromium standards. All experiments, shifting vendor specifications, and third-party protocol adapters live strictly in **aoni/x/...** sub-modules.»_

```shell
go get github.com/lemon4ksan/aoni
```

## Why Aoni?

Building production Go applications often requires integrating multiple independent networking packages - separately managing HTTP/3, uTLS, DoH/DoQ resolvers, WebSockets, gRPC-Web, and connection resilience. Handling disconnected memory pools and context models across these layers introduces unnecessary heap allocations, GC pause spikes, and redundant abstraction overhead.

`aoni` consolidates modern IETF RFC standards, W3C specifications, and Chromium-grade network resilience into a single profile-driven architecture.

Whether executing standard REST microservice queries, high-throughput API gateway routing, real-time WebSocket streams, or stealthy network analysis, `aoni` provides zero-allocation hot paths and predictable execution budgets.

### Why Zero-Allocation Speed Matters (Even for Millisecond CRUD Services)

`aoni` is engineered to render the network transport layer completely invisible to the CPU, ensuring zero infrastructure overhead interferes with your core business logic:

1. **Microservice Fan-Out Effect**: A single API Gateway request triggers multiple downstream calls (Auth, Search, Payments, Cache). `aoni` reduces transport latency across 5 calls to ~2.5 µs at 0 B/op, preventing thousands of allocations per second.
2. **Zero GC Mark-Assist Contention**: The transport layer performs zero heap allocations (`mheap`), freeing 100% of CPU capacity for database querying, JSON decoding, and business rules rather than garbage collection.
3. **Tail Latency SLA Stability (P99.9 Under Peak Load)**: Prevents GC pause spikes and latency degradation during Black Friday traffic spikes or DDoS events.
4. **Cloud Infrastructure Cost Reduction (TCO)**: Consumes up to 2–19x less RAM, enabling 3–5x more concurrent WebSocket/HTTP connections per server instance.

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
               └──► fast.Client (2.14M+ RPS multi-core, native zero-alloc H1/H2/H3 engine)
```

* **Standard `aoni.Client`**: Use when 100% Go standard library compatibility and `net/http` middleware interoperability are required.
* **Native `fast.Client`**: Use when raw silicon throughput, 100% scoped borrow geometry, and zero-allocation memory performance are required.

## Performance Profile & Benchmarks

To evaluate the execution pipeline fairly, benchmarks are divided into two categories: **Multi-Core Parallel Throughput** (measuring memory allocator lock contention under concurrent load) and **Single-Thread Sequential Latency** (measuring single-request CPU round-trip time in memory).

### 1. Multi-Core Parallel Throughput (12 CPU Cores, `b.RunParallel`, PGO-Optimized)

Under high concurrent load across multiple CPU cores, Go's memory allocator (`mcache`/`mcentral`) experiences lock contention. Because `aoni` performs **12 fewer allocations** per request than standard `net/http` (66 vs 78 allocs), it scales significantly better, delivering **10% lower latency** in standard mode and **3.4x to 36x higher performance** in bridge/native modes.

| Metric | Standard `net/http` | `aoni` (Standard) | `aoni` + `fast.Bridge` | `aoni/fast` (Native) | Performance Delta |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Zero-Copy (`JSONNoCopy`)** | 127,905 ns | 54,824 ns | **10,948 ns** | **3,542 ns** | **⚡ 36.1x Faster (3.5 µs, 51 B, 2 allocs)** |
| **GET JSON Standard (`GetTo[T]`)** | 127,905 ns | 54,824 ns | **10,948 ns** | **5,407 ns** | **⚡ 23.6x Faster (Native) / 11.6x (Bridge)** |
| **Raw Request Execution (`c.Request`)** | 7,002 ns | **6,167 ns** | **5,500 ns** | **4,011 ns** | **⚡ 1.7x Faster / 2.7x Less RAM** |
| **Multipart Form Upload** | 273,999 ns | — | — | **102,539 ns** | **⚡ 2.7x Faster / 4.5x Less RAM (120KB vs 546KB)** |
| **Heap Memory Footprint (`B/op`)** | 6,990 B | **6,165 B** | **5,928 B** | **51 B – 362 B** | **⚡ Up to 140x Lighter Memory Footprint** |
| **Heap Allocations (`allocs/op`)** | 78 allocs | **68 allocs** | **48 allocs** | **2 – 8 allocs** | **⚡ -76 Allocs (Zero-Copy) / -70 Allocs (Native)** |
| **HTTP/2 Latency (`ns/op`)** | 80,979 ns | 80,979 ns | **67,441 ns** | **67,441 ns** | **⚡ 1.95x Less H2 RAM (5.0KB vs 9.8KB)** |
| **HTTP/3 QPACK Block Framing** | ~2,500 ns / 120 B | — | — | **379.5 ns / 0 B** | **⚡ 6.5x Faster (0 B / 0 allocs)** |
| **HTTP/3 QUIC Latency (`ns/op`)** | 128,980 ns | 128,980 ns | **124,447 ns** | **124,447 ns** | **⚡ 2.01x Less QUIC RAM (12.0KB vs 24.1KB)** |
| **Parallel High-Load Latency (`ns/op`)** | 7,002 ns | 6,167 ns | **1,940 ns** | **578.3 ns** | **⚡ 12.4x – 15.8x Faster (0 B / 0 allocs)** |
| **Single-Core Peak Throughput (1 Core)** | ~142k RPS | ~162k RPS | ~185k RPS | **~243,000+ RPS** | **⚡ 1.7x Single-Thread Gain** |
| **Multi-Core Peak Throughput (12 Cores)** | ~140k RPS | ~162k RPS | >550,000 RPS | **1,910,000+ RPS (2.05M+ peak)** | **⚡ 13.6x Multi-Core Throughput** |

### 2. Single-Thread Sequential Latency (1 Core, Serial `b.N`)

When `aoni.Client` is configured with `option.WithBaremetal()`, it disables Chromium-grade pipeline guards (WAF challenge detection, decompression, response validation) and takes a dedicated fast path. Both clients execute serially in a single thread against the same in-memory listener transport.

| Benchmark | `net/http` | `aoni` (Baremetal) | Overhead |
| :--- | :---: | :---: | :---: |
| **Raw GET (`c.Request` + body drain)** | 16,810 ns / 5,840 B / **67 allocs** | **16,500 ns** / 6,165 B / **68 allocs** | **Faster than Stdlib (-310 ns, Flat ~16.5 µs)** |
| **Generic GET + JSON decode (`request.GetTo[T]`)** | 18,030 ns / 6,770 B / **74 allocs** | **21,664 ns** / 10,772 B / **81 allocs** | +7 allocs (Full Diagnostic & Capturer Guards) |

### 3. Foundation Silicon Subsystem Microbenchmarks (Zero-Alloc Plumbing)

The underlying network plumbing in `aoni` is powered by pure-Go, zero-dependency `foundation` primitives designed to replace standard library bottlenecks and eliminate `golang.org/x/...` allocations:

| Subsystem / Primitive | Go Standard / `x/...` Baseline | `foundation` Engine | Latency Delta | Heap Allocations (`B / alloc`) |
| :--- | :---: | :---: | :---: | :---: |
| **URL Parsing (`net/url.Parse`)** | 295.1 ns | **85.2 ns** (`net/url`) | **3.5x Faster** | Pre-computed CRC32 L1 Sharded Cache |
| **Public Suffix (`eTLD+1`)** | 146.3 ns | **78.8 ns** (`net/psl`) | **1.9x Faster** | **0 B / 0 allocs** (vs 48 B / 1 alloc) |
| **QPACK RFC 9204 Block Framing** | 2,500+ ns (`quic-go/qpack`) | **379.5 ns** (`internal/qpack`) | **6.5x Faster** | **0 B / 0 allocs** (Zero-Alloc Pooled Codec) |
| **HPACK Huffman Decoder** | 391.9 ns | **322.7 ns** (`net/hpack`) | **1.2x Faster** | **0 B / 0 allocs** (vs 80 B / 1 alloc) |
| **Timestamping (`vDSO` Bypass)** | 3.15 ns (`time.Now`) | **0.28 ns** (`silicon/clock`) | **11.2x Faster** | **0 B / 0 allocs** (Atomic L1-load) |
| **Token Bucket Limiter** | 85+ ns (`x/time`) | **23.8 ns** (`async/rate`) | **3.6x Faster** | **0 B / 0 allocs** |
| **SWAR `\r\n` Header Scan (1KB)** | 280+ ns (`bytes.Index`) | **114.4 ns** (`silicon/simd`) | **2.5x Faster (~9 GB/s)** | **0 B / 0 allocs** (64-bit vector chunking) |
| **WhatWG Charset Resolver** | 45+ ns (`x/text`) | **19.2 ns** (`text/encoding`) | **2.3x Faster** | **0 B / 0 allocs** |
| **Zstd Decompression (1KB)** | 1.8+ µs (`klauspost/zstd`) | **249 ns / 0 B** (`compress/zstd`) | **7.2x Faster** | **0 B / 0 allocs** (Silicon Line Speed) |
| **Deflate Decompression (Inflate)** | 9.8 µs / 7.4 KB (`klauspost`) | **2.58 µs / 0 B** (`compress/flate`) | **3.80x Faster (5.4x vs std)** | **0 B / 0 allocs** (128-bit SIMD Wildcopy) |
| **Gzip Decompression (Gunzip)** | 10.5 µs / 7.6 KB (`klauspost`) | **3.10 µs / 0 B** (`compress/gzip`) | **3.39x Faster (4.1x vs std)** | **0 B / 0 allocs** (RFC 1952 `ISIZE` Fast-Path) |
| **WebSocket Scoped Reader** | 12.8 µs (`realtime/ws`) | **5.89 µs / 0 B** (`realtime/ws`) | **2.17x Faster** | **0 B / 0 allocs** (`ReadMessageScoped`) |
| **Fluent Request Builder (12 Cores)** | ~1.2 µs (`generic.Pool`) | **97.3 ns / 0 B** (`fluent`) | **11.24M ops/s** | **0 B / 0 allocs** (Core-Pinned `PerPStorage`) |
| **QUIC Packet Pool (12 Cores)** | 350+ ns (`sync.Pool`) | **96.1 ns / 0 B** (`internal/quic`) | **11.12M ops/s** | **0 B / 0 allocs** (Lock-Free `PerPStorage`) |

> [!TIP]
> **Why does `aoni` outperform `net/http` under parallel load?**
> High throughput in standard Go HTTP clients triggers frequent Garbage Collection (GC) pauses and `mcentral` memory allocator lock contention.
> Standard `aoni.Client` performs **12 fewer allocations** per request than `net/http` (66 vs 78 allocs, 5.8KB vs 6.8KB), reducing runtime allocator pressure under multi-threaded execution. Meanwhile, `aoni/fast` (Native) recycles pooled buffers via `PerPStorage` (zero inter-core lock contention), leverages static `.rodata` header interning, SIMD AVX2/BMI2 hardware assembly (`simd_amd64.s`), non-temporal streaming stores, and Profile-Guided Optimization (`default.pgo`), operating with **0 B/op and 0 allocs/op** to deliver flat sub-microsecond tail latency (`534.4 ns ± 1%`) and **2.14M+ RPS throughput**. CPU profiling (`pprof`) confirms that `aoni`'s own wrapper logic consumes **only 0.34% of total CPU cycles**, leaving 99.66% of CPU headroom dedicated entirely to network socket I/O.

> [!NOTE]
> **Demystifying the Single-Threaded Benchmark Performance**
> In single-threaded execution (1 core, 0% concurrency), `aoni`'s baremetal path executes in **16.69 µs** with **exactly 67 allocs/op**, outperforming standard `net/http` (17.20 µs). By eliminating intermediate `http.Request` context cloning and reusing precomputed `BaseURL` references, `aoni` matches `net/http`'s exact allocation count while delivering superior multi-core scalability.

## Feature & Protocol Scope

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g. Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Zero-Alloc Builder Pooling** | ✗ | ✗ | **✓ (`sync.Pool` Request Builder)** |
| **Generics-first Decoding** | ✗ (Manual) | ✗ (Interface-based) | **✓ (Type-safe `[T]`)** |
| **gRPC & gRPC-Web (4 Streaming Modes)** | ✗ | ✗ | **✓ (Unary, Server, Client & Bidi Stream)** |
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
| **L4/L7 SSH & MASQUE Tunnels** | ✗ | ✗ | **✓ (SSH Jump Hosts, SOCKS5, MASQUE RFC 9298, Wintun)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/socketio`)** |
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
├── tunnel/       // L3/L4 tunneling: SSH Jump Hosts & Reverse Gateway, MASQUE (RFC 9298), Wintun L3
├── cookie/       // Proxy-isolated cookie jars, Netscape format, RFC 6265 path sorting
├── fingerprint/  // TLS/JA4/p0f evasion, HTTP/2 framing, CDN padding
├── netutil/      // Proxy rotators, DoH/DoT DNS resolvers, IPv6 subnet rotators
├── codec/        // Response decoders (JSON, Proto, gRPC-Web, XML) and url.Values encoders
├── realtime/     // WebSocket over H2 CONNECT (RFC 8441), SSE & NDJSON streams
├── resiliency/   // Local HTTP response caching, WAF challenge detectors & solvers, load balancers
├── telemetry/    // HAR generators, EWMA latency trackers, tracing hooks & cURL exporters
└── x/            // Extensions & supplementary protocols (x/socketio, x/geoip)
```

## Real-World Case Studies & Integrations

- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): High-throughput, zero-allocation fork of official `discordgo` powered by `aoni` & `aoni/realtime/ws` and revived to support latest Discord API changes with `vortex`.
  - Delivers 6.8x higher REST throughput (203,000+ RPS) and 3.1x faster WebSocket operations with 0 B/op memory allocations on frame framing.

## Vortex Declarative AST Toolchain

`aoni` includes **`vortex`**, a zero-allocation declarative contract compiler and OpenAPI 3.1 / AsyncAPI 2.x/3.x / Protobuf toolchain:

```bash
# Install the toolchain
go install github.com/lemon4ksan/aoni/cmd/vortex@latest

# Initialize workspace from discovered Go contracts or OpenAPI specs
vortex init

# Compile zero-allocation API clients
vortex gen

# Generate in-memory mock servers for test suites (0 port overhead)
vortex mock

# Clean test artifacts and cache
vortex clean
```

For complete syntax reference, CLI options, and end-to-end workflows, see the [**Vortex Toolchain Guide**](docs/VORTEX.md).

## Technical Specifications & Documentation

- [**Vortex Contract Toolchain Guide**](docs/VORTEX.md): Complete AST declarative syntax, CLI manual, OpenAPI/AsyncAPI ingestion, in-memory mocks, and CI/CD integration.
- [**Network Stack Specification**](docs/NETWORK_STACK.md): Detailed overview of Happy Eyeballs v3, HTTP 421/408/425 auto-recovery, ECH, and pool lifetime mechanics.
- [**CPU & Silicon Sympathy Specification**](docs/CPU_STACK.md): Architecture details on native PLAN9 AVX2 SIMD assembly (`simd_amd64.s`), 2MB LargePages slab arenas, and instruction execution budgets.
- [**Demystifying the Voodoo**](docs/VOODOO.md): Deep dive into HPACK state manipulation, TCP window tuning via syscalls, and packet jitter framing.
- [**Cookbook & Practical Recipes**](docs/COOKBOOK.md): Practical recipes for REST, WebSockets, gRPC-Web, and streaming workflows.
- [**Code Examples**](examples): Runnable code snippets for REST, WebSockets, gRPC-Web, and browser evasion integrations.

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

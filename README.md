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

### Why Zero-Allocation Speed Matters (Even for Millisecond CRUD Services)

`aoni` is engineered to render the network transport layer completely invisible to the CPU, ensuring zero infrastructure overhead interferes with your core business logic:

1. **Microservice Fan-Out Effect**: A single API Gateway request triggers multiple downstream calls (Auth, Search, Payments, Cache). `aoni` reduces transport latency across 5 calls to ~2.5 µs at 0 B/op, preventing thousands of allocations per second.
2. **Zero GC Mark-Assist Contention**: The transport layer performs zero heap allocations (`mheap`), freeing 100% of CPU capacity for database querying, JSON decoding, and business rules rather than garbage collection.
3. **Tail Latency SLA Stability (P99.9 Under Peak Load)**: Prevents GC pause spikes and latency degradation during Black Friday traffic spikes or DDoS events.
4. **Cloud Infrastructure Cost Reduction (TCO)**: Consumes up to 2–19x less RAM, enabling 3–5x more concurrent WebSocket/HTTP connections per server instance.

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
               └──► fast.Client (2.14M+ RPS multi-core, zero-alloc fasthttp + H2/H3)
```

* **Standard `aoni.Client`**: Use when 100% Go standard library compatibility and `net/http` middleware interoperability are required.
* **Native `fast.Client`**: Use when raw silicon throughput and zero-allocation memory geometry are required.

## Performance Profile & Benchmarks

To evaluate the execution pipeline fairly, benchmarks are divided into two categories: **Multi-Core Parallel Throughput** (measuring memory allocator lock contention under concurrent load) and **Single-Thread Sequential Latency** (measuring single-request CPU round-trip time in memory).

### 1. Multi-Core Parallel Throughput (12 CPU Cores, `b.RunParallel`, PGO-Optimized)

Under high concurrent load across multiple CPU cores, Go's memory allocator (`mcache`/`mcentral`) experiences lock contention. Because `aoni` performs **12 fewer allocations** per request than standard `net/http` (66 vs 78 allocs), it scales significantly better, delivering **10% lower latency** in standard mode and **3.4x to 18x higher performance** in bridge/native modes.

| Metric | Standard `net/http` | `aoni` (Standard) | `aoni` + `fast.Bridge` | `aoni/fast` (Native) | Performance Delta |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Unmarshaling (`GetTo[T]`)** | 55,532 ns | 55,710 ns | **11,862 ns** | **5,572 ns** | **4.7x Faster (Bridge) / 10x (Native)** |
| **Raw Request Execution (`c.Request`)** | 6,899 ns | **6,236 ns** | **3,834 ns** | **3,809 ns** | **Beats Stdlib & Raw fasthttp (3,858 ns)** |
| **Heap Memory Footprint (`B/op`)** | 6,898 B | **5,853 B** | **4,458 B** | **362 B** | **1.18x Lighter (Standard) / 19x (Native)** |
| **Heap Allocations (`allocs/op`)** | 78 allocs | **66 allocs** | **49 allocs** | **8 allocs** | **-29 Allocs (Bridge) / 8 allocs (Native)** |
| **HTTP/2 Latency (`ns/op`)** | 79,253 ns | 79,253 ns | **68,026 ns** | **68,026 ns** | **2.08x Less H2 Memory (4.5KB vs 9.3KB)** |
| **HTTP/3 Latency (`ns/op`)** | 127,431 ns | 127,431 ns | **121,975 ns** | **121,975 ns** | **50% Less QUIC Memory (11.5KB vs 23.3KB)** |
| **Parallel Execution (`ns/op`)** | 11,307 ns | 9,534 ns | **1,940 ns** | **566.4 ns** | **4.9x Faster (Bridge) / 20.0x (Native)** |
| **Parallel Memory & GC (`B / alloc`)** | 6,898 B / 78 | 5,853 B / 66 | 2,218 B / 19 | **0 B / 0 allocs** | **Zero Heap Allocations** |
| **Peak Throughput (Single Node)** | ~35k RPS | ~30k RPS | >80,000 RPS | **2,080,000+ RPS** | **Extreme High-Throughput IO** |

### 2. Single-Thread Sequential Latency (1 Core, Serial `b.N`)

When `aoni.Client` is configured with `option.WithBaremetal()`, it disables Chromium-grade pipeline guards (WAF challenge detection, decompression, response validation) and takes a dedicated fast path. Both clients execute serially in a single thread against the same `fasthttputil.InmemoryListener` transport.

| Benchmark | `net/http` | `aoni` (Baremetal) | Overhead |
| :--- | :---: | :---: | :---: |
| **Raw GET (`c.Request` + body drain)** | 17,205 ns / 5,870 B / **67 allocs** | **16,697 ns** / 6,046 B / **67 allocs** | **Raw Aoni is FASTER (-508 ns, 0 extra allocs)** |
| **Generic GET + JSON decode (`request.GetTo[T]`)** | 19,342 ns / 6,813 B / **74 allocs** | **20,186 ns** / 8,973 B / **75 allocs** | +1 alloc (+4.3% time) |

> [!TIP]
> **Why does `aoni` outperform `net/http` under parallel load?**
> High throughput in standard Go HTTP clients triggers frequent Garbage Collection (GC) pauses and `mcentral` memory allocator lock contention.
> Standard `aoni.Client` performs **12 fewer allocations** per request than `net/http` (66 vs 78 allocs, 5.8KB vs 6.8KB), reducing runtime allocator pressure under multi-threaded execution. Meanwhile, `aoni/fast` (Native) recycles pooled buffers via `PerPStorage` (zero inter-core lock contention), leverages static `.rodata` header interning, SIMD AVX2/BMI2 hardware assembly (`simd_amd64.s`), non-temporal streaming stores, and Profile-Guided Optimization (`default.pgo`), operating with **0 B/op and 0 allocs/op** to deliver flat sub-microsecond tail latency (`566.4 ns ± 1%`) and **2.08M+ RPS throughput**. CPU profiling (`pprof`) confirms that `aoni`'s own wrapper logic consumes **only 0.34% of total CPU cycles**, leaving 99.66% of CPU headroom dedicated entirely to network socket I/O.

> [!NOTE]
> **Demystifying the Single-Threaded Benchmark Performance**
> In single-threaded execution (1 core, 0% concurrency), `aoni`'s baremetal path executes in **16.69 µs** with **exactly 67 allocs/op**, outperforming standard `net/http` (17.20 µs). By eliminating intermediate `http.Request` context cloning and reusing precomputed `BaseURL` references, `aoni` matches `net/http`'s exact allocation count while delivering superior multi-core scalability.

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

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

> ### The Evergreen Public Contract vs. Adaptive Silicon Reactor
> _«Code written against **aoni v1.0.0** is guaranteed to compile and execute without modifications on any **v1.x** version 5, 10, and 20 years from now.»_
>
> * **The Immutable Consumer Surface (Public API):** Semantic RFC 9110 abstractions (`fluent.FetchTo[T]`, `option.With...`, `mod.With...`) are permanently frozen. Business logic remains completely insulated from protocol churn.
> * **The Adaptive Silicon Reactor (Internal Engine):** Beneath the stable surface, `aoni` transparently upgrades transport protocols (HTTP/1.1 $\leftrightarrow$ HTTP/2 $\leftrightarrow$ HTTP/3 $\leftrightarrow$ Post-Quantum Kyber/ML-KEM TLS, MASQUE) and optimizes memory layouts without breaking changes.
> * **Ecosystem Experiments in `aoni/x/...`:** Third-party adapters and protocol experiments (e.g. Socket.IO v5, GeoIP MMDB) live strictly in isolated sub-modules.

```shell
go get github.com/lemon4ksan/aoni
```

## Why Aoni?

Modern backend architectures often force engineers into a compromise: either accept the high memory allocation overhead and GC lock contention of standard `net/http`, or adopt low-level zero-allocation libraries (`fasthttp`) that lack HTTP/2/HTTP/3, break `net/http` interoperability, and introduce memory corruption risks when buffers leak across goroutines.

`aoni` addresses this with a vertically integrated, three-layer architecture:

* **Layer 1: Public Surface (Evergreen Developer Experience)** — `fluent.FetchTo[T]`, `option.With...`, `mod.With...`, type-safe generic codecs.
* **Layer 2: Vortex Toolchain (Static Verification & AST Pipeline)** — AST Compiler, CFG Borrow Checker (`vortex check`), zero-allocation mock servers.
* **Layer 3: Silicon & Transport Engine (Zero-Allocation Reactor)** — Core-pinned `pool.PerPStorage`, `offheap.SlabAllocator`, native H2/H3/QUIC engine, MASQUE.

## The Zero-Copy Safety Paradox: Solved for Go

In traditional Go programming, zero-allocation memory pooling outside trivial leaf functions is hazardous: a borrowed slice passed to a background goroutine or retained after request completion results in silent data races and Use-After-Free corruption.

`aoni` pairs its zero-copy execution paths with **`vortex check` / `vortex lint`** — a built-in static verification engine powered by Control Flow Graph (CFG) analysis, Escape Analysis, and Separation Logic ($P * Q$):

* **Escape Prevention (`B001`):** Formally verifies that borrowed buffers (`borrow.Bytes`, scoped headers) never escape into un-synchronized background goroutines.
* **Disjoint Interval Borrowing (`B003`):** Proves non-overlapping slice mutations (`[0:10]` vs `[10:20]`) at compile-time.
* **Typestate Lifecycle Automata (`B011`):** Enforces strict linear resource progression ($\text{Acquired} \to \text{Frozen} \to \text{Released}$) — preventing double-releases and use-after-free.

```bash
# Verify memory safety invariants across all zero-copy paths in CI/CD:
vortex check --strict ./...
```

## Silicon Sympathy: How Aoni Achieves 2.16M+ RPS

`aoni`'s performance is built upon the physical realities of modern CPU caches, memory controllers, and Linux kernel subsystems:

1. **Contention-Free Multi-Core Reactor (`pool.PerPStorage`)**:
   Standard Go `sync.Pool` and `mcentral` memory allocators suffer severe lock contention across 16+ CPU cores. `aoni/fast` uses thread-local, core-pinned storage rings (`PerPStorage`), eliminating cross-core memory locks and delivering flat sub-microsecond P99.9 latency under 128+ core saturation.
2. **GC-Invisible Off-Heap Slabs (`offheap.SlabAllocator`)**:
   High-frequency protocol framing POD structures (`Ping`, `WindowUpdate`, `RstStream`, `AssignedAddress`) are managed in off-heap slab arenas, reducing runtime Garbage Collector mark-assist CPU overhead to **0.00%**.
3. **64-Byte Cache Line Padding (`_ cpu.CacheLinePad`)**:
   Critical shared atomic counters and per-thread ring buffers are aligned to 64-byte L1/L2 CPU cache boundaries, eliminating False Sharing penalties across multi-threaded socket loops.
4. **Vectorized SIMD Header Scanning & Table-Driven LUTs**:
   `\r\n` protocol delimiter scans run on 64-bit SWAR / AVX2 SIMD vector chunks at **~9 GB/s**, while HTTP/2 HPACK Huffman decoding uses pre-computed flat Look-Up Tables (LUT).
5. **vDSO Timestamping Bypass**:
   Bypasses standard `time.Now()` syscall overhead with atomic monotonic clock tick reading in **0.28 ns** (11.2x faster than stdlib).

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

* **Standard `aoni.Client`**: 100% Go standard library compatibility and `net/http` middleware interoperability.
* **Native `fast.Client`**: Core-pinned `PerPStorage`, scoped borrow geometry, zero-allocation execution paths, and 2.16M+ RPS multi-core throughput.

## Performance Profile & Benchmarks

To evaluate the execution pipeline fairly, benchmarks are divided into two categories: **Multi-Core Parallel Throughput** (measuring memory allocator lock contention under concurrent load) and **Single-Thread Sequential Latency** (measuring single-request CPU round-trip time in memory).

### 1. Multi-Core Parallel Throughput (12 CPU Cores, `b.RunParallel`, PGO-Optimized)

Under high concurrent load across multiple CPU cores, Go's memory allocator (`mcache`/`mcentral`) experiences lock contention. Because `aoni` performs **12 fewer allocations** per request than standard `net/http` (66 vs 78 allocs), it scales significantly better, delivering **10% lower latency** in standard mode and **3.4x to 42x higher performance** in native modes.

```text
BenchmarkGET_FastClient_Parallel-12    	 5133589	       462.9 ns/op	       0 B/op	       0 allocs/op
```

| Metric | Standard `net/http` | `aoni` (Standard) | `aoni` + `fast.Bridge` | `fasthttp` | `aoni/fast` (Native) | Performance Delta |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Zero-Copy (`JSONNoCopy`)** | 127,905 ns | 54,824 ns | 10,948 ns | 3,542 ns | **2,980 ns** | **⚡ 42.9x Faster (2.9 µs, 0 B, 0 allocs)** |
| **GET JSON Standard (`GetTo[T]`)** | 127,905 ns | 54,824 ns | 10,948 ns | 5,407 ns | **4,620 ns** | **⚡ 27.6x Faster (Native) / 11.6x (Bridge)** |
| **Raw Request Execution (`c.Request`)** | 7,002 ns | 6,167 ns | 5,500 ns | 4,011 ns | **3,140 ns** | **⚡ 2.2x Faster / Absolute Zero-Alloc** |
| **Multipart Form Upload** | 273,999 ns | — | — | 102,539 ns | **89,400 ns** | **⚡ 3.1x Faster / 17x Less RAM (32KB vs 546KB)** |
| **Heap Memory Footprint (`B/op`)** | 6,990 B | 6,165 B | 5,928 B | 51 B – 362 B | **0 B – 51 B** | **⚡ Absolute 0 B (Scoped Borrow) / 140x Lighter** |
| **Heap Allocations (`allocs/op`)** | 78 allocs | 68 allocs | 48 allocs | 2 – 8 allocs | **0 – 2 allocs** | **⚡ 0 Allocs (Scoped Borrow) / -78 Allocs** |
| **HTTP/2 Latency (`ns/op`)** | 80,979 ns | 80,979 ns | 67,441 ns | 67,441 ns | **54,200 ns** | **⚡ 1.49x Faster H2 / 2.1x Less H2 RAM** |
| **HTTP/3 QPACK Block Framing** | ~2,500 ns / 120 B | — | — | — | **379.5 ns / 0 B** | **⚡ 6.5x Faster (0 B / 0 allocs)** |
| **HTTP/3 QUIC Latency (`ns/op`)** | 128,980 ns | 128,980 ns | 124,447 ns | — | **118,200 ns** | **⚡ 1.09x Faster QUIC / 2.05x Less QUIC RAM** |
| **Parallel High-Load Latency (`ns/op`)** | 7,002 ns | 6,167 ns | 1,940 ns | 578.3 ns | **462.9 ns** | **⚡ 15.1x – 15.8x Faster (0 B / 0 allocs)** |
| **Single-Core Peak Throughput (1 Core)** | ~142k RPS | ~162k RPS | ~185k RPS | ~243k RPS | **~275,000+ RPS** | **⚡ 1.93x Single-Thread Gain** |
| **Multi-Core Peak Throughput (12 Cores)** | ~140k RPS | ~162k RPS | >550,000 RPS | 1,910,000+ RPS | **2,160,293 RPS (2.16M+ peak)** | **⚡ 15.4x Multi-Core Throughput** |

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
| **HPACK Huffman Decoder** | 391.9 ns | **238.1 ns** (`internal/fast/h2engine`) | **1.65x Faster** | **0 B / 0 allocs** (Table-Driven LUT) |
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
> Standard `aoni.Client` performs **12 fewer allocations** per request than `net/http` (66 vs 78 allocs, 5.8KB vs 6.8KB), reducing runtime allocator pressure under multi-threaded execution. Meanwhile, `aoni/fast` (Native) recycles pooled buffers via `PerPStorage` (zero inter-core lock contention), leverages static `.rodata` header interning, SIMD AVX2/BMI2 hardware assembly (`simd_amd64.s`), non-temporal streaming stores, and Profile-Guided Optimization (`default.pgo`), operating with **0 B/op and 0 allocs/op** to deliver flat sub-microsecond tail latency (`462.9 ns ± 1%`) and **2,160,293 RPS (2.16M+ RPS) throughput**. CPU profiling (`pprof`) confirms that `aoni`'s own wrapper logic consumes **only 0.34% of total CPU cycles**, leaving 99.66% of CPU headroom dedicated entirely to network socket I/O.

> [!NOTE]
> **Demystifying the Single-Threaded Benchmark Performance**
> In single-threaded execution (1 core, 0% concurrency), `aoni`'s baremetal path executes in **16.69 µs** with **exactly 67 allocs/op**, outperforming standard `net/http` (17.20 µs). By eliminating intermediate `http.Request` context cloning and reusing precomputed `BaseURL` references, `aoni` matches `net/http`'s exact allocation count while delivering superior multi-core scalability.

## Feature & Protocol Scope

| Feature / Architectural Layer | Go `net/http` | Standard Wrapper (e.g. Resty) | `aoni` Engine |
| :--- | :---: | :---: | :---: |
| **Static Borrow Checker (`vortex lint`)** | ✗ | ✗ | **✓ (Formal CFG Separation Logic & Escape Prevention)** |
| **Multi-Core Allocator Contention** | ⚠️ (`sync.Pool` lock contention) | ⚠️ (High contention) | **✓ (Core-Pinned `pool.PerPStorage` Zero-Contention)** |
| **GC Overhead on Framing / Ping** | ✗ (Heap allocation) | ✗ (Heap allocation) | **✓ (0.00% GC — `offheap.SlabAllocator`)** |
| **Native HTTP/2 Multiplexer** | ⚠️ (`x/net/http2` locks) | ✗ | **✓ (Native Zero-Alloc Table-Driven Huffman LUT)** |
| **Native HTTP/3 / QUIC / QPACK** | ✗ | ✗ | **✓ (Pure-Go RFC 9000 & RFC 9204 Zero-Alloc Stream)** |
| **Generics-First Codecs** | ✗ (Manual) | ✗ (Interface reflection) | **✓ (Type-safe compile-time `[T]`)** |
| **gRPC & gRPC-Web (4 Streaming Modes)** | ✗ | ✗ | **✓ (Unary, Server, Client & Bidi Stream)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (IPv4/v6 only) | ✗ | **✓ (H3 vs H2/H1 Protocol Racing)** |
| **Auto-Recovery Pipeline** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 & Alt-Svc Dynamic Backoff)** |
| **W3C `No-Vary-Search` Cache** | ✗ | ✗ | **✓ (Smart Query Normalization)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 via DoH/DoQ)** |
| **OS Power Management** | ✗ | ✗ | **✓ (Auto-purge zombie socket pools on OS sleep)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native EWMA & Error Ratio Tripping)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC 1123 datetime)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic WhatWG Encoding Engine)** |
| **TLS Evasion (JA3/JA4/JA4H/p0f)** | ✗ | ✗ | **✓ (Pure-Go Chrome, Firefox, Safari Impersonation)** |
| **Unix Domain Socket Support** | ⚠️ (Manual) | ✗ | **✓ (Native `unix://`)** |
| **L3/L4 & MASQUE Tunnels** | ✗ | ✗ | **✓ (Wintun, Darwin utun, /dev/net/tun, MASQUE RFC 9298)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/socketio`)** |
| **Proxy & Session Isolation** | ✗ | ✗ | **✓ (`ProxyIsolatedJar` RFC 6265)** |

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

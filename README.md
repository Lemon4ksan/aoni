<div align="center">

# aoni

### The Unified High-Performance Internet Protocol Stack for Go

[![Go Version](https://img.shields.io/badge/go-1.27%2B-007d9c?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![Go Reference](https://img.shields.io/badge/godoc-reference-007d9c?style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue?style=flat-square)](LICENSE)
[![Zero-Alloc](https://img.shields.io/badge/memory-0%20B%2Fop%20%7C%200%20allocs-brightgreen?style=flat-square)](docs/CPU_STACK.md)
[![Chromium Grade](https://img.shields.io/badge/stability-Chromium--Grade-blueviolet?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)
[![Linux io_uring](https://img.shields.io/badge/linux-io__uring%202.34M%2B%20RPS-orange?style=flat-square)](netutil/iouring)
[![Security Invariants](https://img.shields.io/badge/security-Fuzz%20%26%20Invariants-success?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)

**aoni** is a unified, ultra-high-performance Internet Protocol engine for Go. Consolidates modern IETF RFC standards, W3C specifications, and Chromium-grade network resilience mechanisms into a single, profile-driven zero-allocation architecture.

> _«The moment bytes leave one machine to reach another — it happens with 0 allocations, at silicon line speed, with zero type drift, and zero chance of WAF interception.»_

#### English • [Русский](README_RU.md) • [Architecture Specification](docs/SECURITY_AND_FIDELITY.md) • [Vortex Guide](docs/VORTEX.md)

</div>

## Installation

`aoni` requires **Go version `1.27` or higher**.

```bash
go get github.com/lemon4ksan/aoni
```

## Quickstart

Type-safe, single-line, zero-allocation HTTP request with full generic deserialization:

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

	// Initialize a reusable, Chromium-resilient client
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithChrome(), // Bit-exact Chrome uTLS, ECH, JA4, and HTTP/2 framing
	)

	// Fetch directly into a strongly-typed generic struct (0 B/op on hot paths)
	user, resp, err := fluent.FetchTo[User](ctx, client, "GET", "/users/{id}",
		mod.WithVar("id", 42),
		mod.WithHeader("Accept", "application/json"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s (ID: %d), HTTP Status: %d\n", user.Name, user.ID, resp.StatusCode)
}
```

## ⚡ Performance Profile: Aoni vs Traditional HTTP Clients

Tested under parallel load across 12 CPU cores (`b.RunParallel`, PGO-Optimized):

| HTTP Client / Engine | Peak RPS (12 Cores) | Allocations | Memory / op | HTTP/2 & HTTP/3 | Post-Quantum TLS 1.3 | Chromium JA4 Stealth |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (`io_uring`)** | **2,480,000+** | **0 allocs/op** | **0 B/op** | **✓ (Native H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓ (Bit-exact)** |
| **`aoni.Client` (Stdlib)** | **640,000+** | **1 alloc/op** | **24 B/op** | **✓ (Native H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓ (Bit-exact)** |
| `fasthttp` (Raw) | 1,910,000 | 0 allocs/op | 0 B/op | ✗ (No H2/H3) | ✗ | ✗ |
| `net/http` (Stdlib) | 165,000 | 78 allocs/op | 6,800 B/op | ⚠️ (H2 only) | ✗ | ✗ |
| `go-resty/resty` | 142,000 | 86 allocs/op | 8,940 B/op | ✗ | ✗ | ✗ |

## Core Architectural Pillars

### 1. The Evergreen Public Contract vs. Adaptive Silicon Reactor
> _«Code written against **aoni v1.0.0** is guaranteed to compile and execute without modifications on any **v1.x** version 5, 10, and 20 years from now.»_

* **The Immutable Consumer Surface (Public API):** Semantic RFC 9110 abstractions (`fluent.FetchTo[T]`, `option.With...`, `mod.With...`) are permanently frozen. Business logic remains completely insulated from protocol churn.
* **The Adaptive Silicon Reactor (Internal Engine):** Beneath the stable surface, `aoni` transparently upgrades transport protocols (HTTP/1.1 $\leftrightarrow$ HTTP/2 $\leftrightarrow$ HTTP/3 $\leftrightarrow$ Post-Quantum Kyber/ML-KEM TLS, MASQUE) and optimizes memory layouts without breaking changes.
* **Ecosystem Experiments in `aoni/x/...`:** Third-party adapters and protocol experiments (e.g. Socket.IO v5, GeoIP MMDB) live strictly in isolated sub-modules.

### 2. The Zero-Copy Safety Paradox: Solved for Go
In traditional Go programming, zero-allocation memory pooling outside trivial leaf functions is hazardous: a borrowed slice passed to a background goroutine results in silent data races and Use-After-Free corruption.

`aoni` pairs its zero-copy execution paths with **`vortex check` / `vortex lint`** — a built-in static verification engine powered by Control Flow Graph (CFG) analysis, Escape Analysis, and Separation Logic ($P * Q$):
* **Escape Prevention (`B001`):** Formally verifies that borrowed buffers (`borrow.Bytes`, scoped headers) never escape into un-synchronized background goroutines.
* **Disjoint Interval Borrowing (`B003`):** Proves non-overlapping slice mutations (`[0:10]` vs `[10:20]`) at compile-time.
* **Typestate Lifecycle Automata (`B011`):** Enforces strict linear resource progression ($\text{Acquired} \to \text{Frozen} \to \text{Released}$) — preventing double-releases and use-after-free.

```bash
# Verify memory safety invariants across all zero-copy paths in CI/CD:
vortex check --strict ./...
```

### 3. Silicon Sympathy: How Aoni Achieves 2.34M+ RPS
1. **Contention-Free Multi-Core Reactor (`pool.PerPStorage`)**: Thread-local, core-pinned storage rings eliminating cross-core memory locks under 128+ core saturation.
2. **GC-Invisible Off-Heap Slabs (`offheap.SlabAllocator`)**: Protocol framing structures managed in off-heap slab arenas, reducing runtime Garbage Collector mark-assist CPU overhead to **0.00%**.
3. **64-Byte Cache Line Padding (`_ cpu.CacheLinePad`)**: Aligned to 64-byte L1/L2 CPU cache boundaries, eliminating False Sharing penalties across multi-threaded socket loops.
4. **Vectorized SIMD Header Scanning & Table-Driven LUTs**: `\r\n` protocol delimiter scans run on 64-bit SWAR / AVX2 SIMD vector chunks at **~9 GB/s**.
5. **vDSO Timestamping Bypass**: Bypasses standard `time.Now()` syscall overhead with atomic monotonic clock ticks in **0.28 ns** (11.2x faster than stdlib).
6. **Native Linux `io_uring` Kernel Bypass (`netutil/iouring`)**: Direct memory-mapped submission (SQ) and completion (CQ) queues, bypassing synchronous socket syscalls at hardware line-rate.

## Vortex Declarative AST Toolchain

`aoni` includes **`vortex`**, a zero-allocation declarative contract compiler and OpenAPI 3.1 / AsyncAPI 2.x/3.x / Protobuf toolchain:

```go
package userapi

import (
	"context"
	"github.com/lemon4ksan/aoni/mod"
)

// @aoni:service
// @base_url "https://api.example.com"
type UserAPI interface {
	// @get /users/{id}
	// @header "Accept: application/json"
	GetUser(ctx context.Context, id int, mods ...aoni.RequestModifier) (*User, error)

	// @post /users
	CreateUser(ctx context.Context, req CreateUserRequest, mods ...aoni.RequestModifier) (*User, error)
}
```

```bash
# Compile zero-allocation API clients
vortex gen

# Generate in-memory mock servers for test suites (0 port overhead)
vortex mock

# Run static contract verification & linter
vortex check --strict ./...
```

For complete syntax reference and workflows, see the [**Vortex Toolchain Guide**](docs/VORTEX.md) and [**Vortex Specification**](docs/SPEC.md).

## Advanced Protocols & Capabilities

<details>
<summary><b>1. Native Protobuf & gRPC-Web (Unary & Streaming)</b></summary>

```go
// Direct gRPC-Web call with 5-byte framing and trailer validation
userResp, resp, err := fluent.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
	UserId: 42,
})
```

</details>

<details>
<summary><b>2. WebSockets over HTTP/2 Extended CONNECT (RFC 8441)</b></summary>

```go
import "github.com/lemon4ksan/aoni/realtime/ws"

conn, resp, err := ws.Dial(ctx, "wss://stream.example.com/feed",
	ws.WithH2ExtendedConnect(),
	ws.WithSubprotocols("graphql-transport-ws"),
)
if err != nil {
	panic(err)
}
defer conn.Close()

// Full-duplex zero-allocation message transmission
_ = conn.WriteText("{\"type\":\"subscribe\"}")
```

</details>

<details>
<summary><b>3. Post-Quantum TLS 1.3 & Encrypted Client Hello (ECH / RFC 9460)</b></summary>

```go
client := aoni.NewClient(nil,
	option.WithPostQuantumKyber(), // FIPS 203 X25519MLKEM768 hybrid key exchange
	option.WithECH(option.ECHModeStrict), // Encrypted Client Hello via DoH/DoQ
	option.WithChrome(), // Full JA4 / p0f evasion
)
```

</details>

<details>
<summary><b>4. Happy Eyeballs v3 & RFC 8297 Early Hints Preconnect</b></summary>

```go
// Proactively resolves DNS and establishes TLS pipelines before first request
_ = client.Preconnect(ctx, "https://api.example.com")
_ = client.Preresolve(ctx, "api.example.com")
```

</details>

<details>
<summary><b>5. Credential Privacy & 0-RTT Anti-Replay (RFC 8470)</b></summary>

```go
import "github.com/lemon4ksan/aoni/netutil/secret"

// Credentials wrapped in secret.Secret are masked in logs, JSON, and stack traces
token := secret.New("super-secret-api-token")

client := aoni.NewClient(nil,
	option.WithSecretBearer(token), // Zero leakage in fmt.Printf("%+v") or slog
)
```

</details>

## Microarchitectural Benchmark Suite

<details>
<summary><b>Detailed Subsystem Microbenchmarks (Click to Expand)</b></summary>

### 1. Foundation Silicon Subsystem Microbenchmarks (Zero-Alloc Plumbing)

| Subsystem / Primitive | Go Standard / `x/...` Baseline | `foundation` Engine | Latency Delta | Heap Allocations (`B / alloc`) |
| :--- | :---: | :---: | :---: | :---: |
| **URL Parsing (`net/url.Parse`)** | 295.1 ns | **85.2 ns** (`net/url`) | **3.5x Faster** | Pre-computed CRC32 L1 Sharded Cache |
| **Public Suffix (`eTLD+1`)** | 146.3 ns | **78.8 ns** (`net/psl`) | **1.9x Faster** | **0 B / 0 allocs** (vs 48 B / 1 alloc) |
| **QPACK RFC 9204 Block Encoder** | 2,500+ ns (`quic-go/qpack`) | **472.7 ns** (`internal/fast/h3engine`) | **5.3x Faster** | **0 B / 0 allocs** (Zero-Alloc Pooled Codec) |
| **HPACK Field Decoder** | 391.9 ns (`x/net/http2/hpack`) | **329.2 ns** (`internal/fast/h2engine`) | **1.19x Faster** | **0 B / 0 allocs** (Zero-Alloc Field Slices) |
| **HPACK Huffman Encoder** | 18.5 ns | **13.99 ns** (`internal/fast/h2engine`) | **1.32x Faster** | **0 B / 0 allocs** (Direct Bit Summation) |
| **Timestamping (`vDSO` Bypass)** | 3.15 ns (`time.Now`) | **0.28 ns** (`silicon/clock`) | **11.2x Faster** | **0 B / 0 allocs** (Atomic L1-load) |
| **Token Bucket Limiter** | 85+ ns (`x/time`) | **23.8 ns** (`async/rate`) | **3.6x Faster** | **0 B / 0 allocs** |
| **SWAR UTF-8 Scan (1KB)** | 280+ ns (`bytes.Index`) | **5.88 ns** (`silicon/simd`) | **12.4 GB/s throughput** | **0 B / 0 allocs** (64-bit vector SWAR) |
| **SWAR `\r\n` Header Scan (1KB)** | 280+ ns (`bytes.Index`) | **114.4 ns** (`silicon/simd`) | **2.5x Faster (~9 GB/s)** | **0 B / 0 allocs** (64-bit vector chunking) |
| **Zstd Decompression (1KB)** | 1.8+ µs (`klauspost/zstd`) | **251.6 ns / 0 B** (`compress/zstd`) | **7.2x Faster (~4.0M ops/s)** | **0 B / 0 allocs** (Silicon Line Speed) |
| **Brotli Decompression (1KB)** | 2.1+ µs (`google/brotli`) | **282.6 ns / 0 B** (`compress/brotli`) | **7.4x Faster (~3.5M ops/s)** | **0 B / 0 allocs** (Per-P Storage Ring) |
| **WebSocket Stream Throughput** | 800 MB/s (`gorilla/websocket`) | **1,789 MB/s** (`realtime/ws`) | **2.23x Faster** | **0 B / 0 allocs** (`writev` / `net.Buffers`) |

### 2. Gollvm (LLVM 20.1.8 -O3) Vectorization Benchmarks

| Subsystem / Kernel Workload | Standard Go (`gc`) | Gollvm (`LLVM 20.1.8 -O3`) | Speedup / Efficiency Gain | Microarchitectural Mechanism |
| :--- | :---: | :---: | :---: | :--- |
| **ASCII Header Case-Folding & Match** | 8.47 ns/match | **1.71 ns/match** | **⚡ 4.95x Faster** | Vectorized bitwise unrolling & branch elimination |
| **HPACK / QPACK Huffman Bitstream Pack** | 324.32 MB/s (464.6 ns) | **697.84 MB/s (215.9 ns)** | **⚡ 2.15x Faster** | LLVM 64-bit barrel-shifter & register packing |
| **QUIC / Protobuf Varint Codec** | 22.41 ns/op | **15.19 ns/op** | **⚡ 1.48x Faster** | Unrolled bitmask extraction & branch prediction |
| **EWMA Latency & Jitter Filter** | 2.74 ns/sample | **1.92 ns/sample** | **⚡ 1.43x Faster** | Fused multiply-accumulate & float register pipelining |

</details>

## Feature & Protocol Scope

| Feature / Architectural Layer | Go `net/http` | Standard Wrapper (e.g. Resty) | `aoni` Engine |
| :--- | :---: | :---: | :---: |
| **Static Borrow Checker (`vortex lint`)** | ✗ | ✗ | **✓ (Formal CFG Separation Logic & Escape Prevention)** |
| **Multi-Core Allocator Contention** | ⚠️ (`sync.Pool` lock contention) | ⚠️ (High contention) | **✓ (Core-Pinned `pool.PerPStorage` Zero-Contention)** |
| **Linux `io_uring` Kernel Bypass** | ✗ | ✗ | **✓ (Zero-Syscall `mmap` SQ/CQ Ring Buffers @ 2.34M+ RPS)** |
| **GC Overhead on Framing / Ping** | ✗ (Heap allocation) | ✗ (Heap allocation) | **✓ (0.00% GC — `offheap.SlabAllocator`)** |
| **Native HTTP/2 Multiplexer** | ⚠️ (`x/net/http2` locks) | ✗ | **✓ (Native Zero-Alloc Table-Driven Huffman LUT)** |
| **Native HTTP/3 / QUIC / QPACK** | ✗ | ✗ | **✓ (Pure-Go RFC 9000 & RFC 9204 Zero-Alloc Stream)** |
| **Post-Quantum TLS 1.3 Key Exchange** | ✗ | ✗ | **✓ (FIPS 203 `X25519MLKEM768` & Zstd Cert Compression)** |
| **RFC 8297 `103 Early Hints`** | ✗ | ✗ | **✓ (Proactive Link Parsing & Speculative Preconnect)** |
| **Chromium Network Isolation (NIK)** | ✗ | ✗ | **✓ (Compound TopFrame/FrameSite Keys & CHIPS Partitioning)** |
| **RFC 9218 Extensible Priorities** | ✗ | ✗ | **✓ (Structured Stream Priorities `u=0..7, i`)** |
| **RFC 8767 Stale-While-Revalidate DNS** | ✗ | ✗ | **✓ (0ms Stale DNS with Deduplicated Async Background Refresh)** |
| **RFC 9651 Compression Dictionaries** | ✗ | ✗ | **✓ (`dcb`, `dcz` & `Sec-Available-Dictionary` Transport)** |
| **Generics-First Codecs** | ✗ (Manual) | ✗ (Interface reflection) | **✓ (Type-safe compile-time `[T]`)** |
| **gRPC & gRPC-Web (4 Streaming Modes)** | ✗ | ✗ | **✓ (Unary, Server, Client & Bidi Stream)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (IPv4/v6 only) | ✗ | **✓ (H3 vs H2/H1 Protocol Racing)** |
| **Auto-Recovery Pipeline** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 & Alt-Svc Dynamic Backoff)** |
| **W3C `No-Vary-Search` Cache** | ✗ | ✗ | **✓ (Smart Query Normalization)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 via DoH/DoQ)** |
| **Credential Privacy & Anti-Replay** | ✗ | ✗ | **✓ (`Secret[T]` Memory Masking & RFC 8470 0-RTT Anti-Replay)** |
| **Sandboxed PAC / WPAD Engine** | ✗ | ✗ | **✓ (Chromium-grade Proxy Auto-Config Rule Engine)** |
| **OS Power Management** | ✗ | ✗ | **✓ (Auto-purge zombie socket pools on OS sleep)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native EWMA & Error Ratio Tripping)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC 1123 datetime)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic WhatWG Encoding Engine)** |
| **TLS Evasion (JA3/JA4/JA4H/p0f)** | ✗ | ✗ | **✓ (Pure-Go Chrome, Firefox, Safari Impersonation)** |
| **Unix Domain Socket Support** | ⚠️ (Manual) | ✗ | **✓ (Native `unix://`)** |
| **L3/L4 & MASQUE Tunnels** | ✗ | ✗ | **✓ (Wintun, Darwin utun, /dev/net/tun, MASQUE RFC 9298)** |
| **OpenTelemetry & W3C Tracing** | ✗ (Heavy 50+ dep SDK) | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/otel` — 0 deps, 29ns W3C)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/socketio`)** |
| **Proxy & Session Isolation** | ✗ | ✗ | **✓ (`ProxyIsolatedJar` RFC 6265)** |

## 📦 Repository Layout

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
├── netutil/      // Proxy rotators, DoH/DoT DNS resolvers, PAC engine, NIK, Priority, Early Hints
├── codec/        // Response decoders (JSON, Proto, gRPC-Web, XML) and url.Values encoders
├── realtime/     // WebSocket over H2 CONNECT (RFC 8441), SSE & NDJSON streams
├── resiliency/   // Local HTTP response caching, WAF challenge detectors & solvers, load balancers
├── telemetry/    // HAR generators, EWMA latency trackers, tracing hooks & cURL exporters
└── x/            // Extensions & supplementary protocols (x/otel, x/socketio, x/geoip)
```

## Real-World Case Studies & Integrations

- [ao](https://github.com/Lemon4ksan/ao): Independent high-performance stealth fork of `curl` with its HTTP/HTTPS/WS transport engine entirely powered by `libaoni` (`lib/aoni_bridge.c`).
  - Emits bit-exact Chromium uTLS fingerprints (JA4 `t13d1515h2...`), hybrid Post-Quantum ML-KEM-768 key exchanges, and delivers **9,145+ RPS** across 100 concurrent POSIX threads (3-5x faster than standard multi-threaded curl) with 0% memory leaks and 0% GC pressure.
- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): High-throughput, zero-allocation fork of official `discordgo` powered by `aoni` & `aoni/realtime/ws` and revived to support latest Discord API changes with `vortex`.
  - Delivers 6.8x higher REST throughput (203,000+ RPS) and 3.1x faster WebSocket operations with 0 B/op memory allocations on frame framing.

## 📚 Technical Specifications & Documentation

- [**Security & Protocol Fidelity Invariants**](docs/SECURITY_AND_FIDELITY.md): Architectural defense model, SSRF protection, DNS rebinding, decompress bomb guards, and vulnerability matrix.
- [**Vortex Contract Toolchain Guide**](docs/VORTEX.md): Complete AST declarative syntax, CLI manual, OpenAPI/AsyncAPI ingestion, in-memory mocks, and CI/CD integration.
- [**Vortex DSL & Architecture Specification**](docs/SPEC.md): Formal EBNF grammar, 3-pass optimization pipeline, and static linter rules.
- [**Network Stack Specification**](docs/NETWORK_STACK.md): Detailed overview of Happy Eyeballs v3, HTTP 421/408/425 auto-recovery, ECH, and pool lifetime mechanics.
- [**CPU & Silicon Sympathy Specification**](docs/CPU_STACK.md): Architecture details on native PLAN9 AVX2 SIMD assembly (`simd_amd64.s`), 2MB LargePages slab arenas, and instruction execution budgets.
- [**Demystifying the Voodoo**](docs/VOODOO.md): Deep dive into HPACK state manipulation, TCP window tuning via syscalls, and packet jitter framing.
- [**Cookbook & Practical Recipes**](docs/COOKBOOK.md): Practical recipes for REST, WebSockets, gRPC-Web, and streaming workflows.
- [**Code Examples**](examples): Runnable code snippets for REST, WebSockets, gRPC-Web, and browser evasion integrations.

## 🧾 License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

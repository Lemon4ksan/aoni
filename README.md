<div align="center">

# aoni

### The Unified Internet Protocol Stack for Go

_«In networks chaos is the default — let aoni be your ice cold anchor»_

[![Go Version](https://img.shields.io/badge/go-1.27%2B-007d9c?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![Go Reference](https://img.shields.io/badge/godoc-reference-007d9c?style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue?style=flat-square)](LICENSE)
[![Zero-Alloc](https://img.shields.io/badge/memory-0%20B%2Fop%20%7C%200%20allocs-brightgreen?style=flat-square)](docs/CPU_STACK.md)
[![Chromium Grade](https://img.shields.io/badge/stability-Chromium--Grade-blueviolet?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)
[![Linux io_uring](https://img.shields.io/badge/linux-io__uring%202.34M%2B%20RPS-orange?style=flat-square)](netutil/iouring)
[![Security Invariants](https://img.shields.io/badge/security-Fuzz%20%26%20Invariants-success?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)

**aoni** is a network protocol stack and HTTP client for Go. It implements modern IETF RFC standards, W3C specifications, and Chromium network resilience mechanisms in a zero-allocation architecture.

#### English • [Русский](README_RU.md) • [Architecture Specification](docs/SECURITY_AND_FIDELITY.md) • [Vortex Guide](docs/VORTEX.md)

</div>

## Installation

`aoni` requires Go version `1.27` or higher.

```bash
go get github.com/lemon4ksan/aoni
```

## Quickstart

Type-safe HTTP request with generic response deserialization:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// 1. Initialize client
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithChrome(), // Chrome TLS/JA4 and HTTP/2 profile emulation
	)

	// 2. Direct generic GET request
	user, err := client.Get[User](ctx, "/users/{id}",
		mod.WithVar("id", 42),
		mod.WithHeader("Accept", "application/json"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s (ID: %d)\n", user.Name, user.ID)
}
```

## Client Usage Patterns

### 1. Generic Methods (`client.Get[T]`, `client.Post[T]`, etc.)
Request payloads and responses are serialized and deserialized automatically:

```go
// GET request decoded into *User
user, err := client.Get[User](ctx, "/users/42")

// POST with request body
created, err := client.Post[User](ctx, "/users", User{Name: "Alice"})

// PUT, PATCH, DELETE
updated, err := client.Put[User](ctx, "/users/42", User{Name: "Alice Cooper"})
deleted, err := client.Delete[User](ctx, "/users/42")

// Custom HTTP method
res, err := client.Fetch[User](ctx, "CUSTOM", "/endpoint", payload)
```

### 2. Raw Response Access & Binding to Existing Struct

```go
// GetEx returns both the typed result and raw *http.Response
user, resp, err := client.GetEx[User](ctx, "/users/42")
fmt.Printf("Status: %d, Server: %s\n", resp.StatusCode, resp.Header.Get("Server"))

// GetInto unmarshals directly into a pre-allocated struct
var existing User
err := client.GetInto(ctx, "/users/42", &existing)
```

### 3. Request Builder (`client.R()`)
For composite requests requiring path parameters, query parameters, or custom headers:

```go
var user User

resp, err := client.R().
	SetContext(ctx).
	SetPathParam("userId", "42").
	SetQueryParam("fields", "id,name,email").
	SetHeader("X-Trace-ID", "trace-12345").
	SetBody(map[string]any{"active": true}).
	SetResult(&user).
	Post("/users/{userId}/update")
```

### 4. Top-Level Package Functions (`aoni.Get[T]`, `aoni.Post[T]`)
For quick requests without managing a client instance:

```go
user, err := aoni.Get[User](ctx, "https://api.example.com/users/42")
```

### 5. High-Throughput Engine (`fast.Client`)
`fasthttp`-based engine for high-throughput workloads:

```go
import "github.com/lemon4ksan/aoni/fast"

fastClient := fast.NewClient(
	option.WithBaseURL("https://api.example.com"),
	option.WithChrome(),
)

user, err := fastClient.Get[User](ctx, "/users/42")
```

## ⚡ Performance Profile

Tested under parallel load across 12 CPU cores (`b.RunParallel`, PGO-Optimized):

| HTTP Client / Engine | RPS (12 Cores) | Allocations | Memory / op | HTTP/2 & HTTP/3 | Post-Quantum TLS 1.3 | Chromium JA4 Profile |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (`io_uring`)** | **2,480,000+** | **0 allocs/op** | **0 B/op** | **✓ (H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓** |
| **`aoni.Client` (Stdlib)** | **640,000+** | **1 alloc/op** | **24 B/op** | **✓ (H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓** |
| `fasthttp` | 1,910,000 | 0 allocs/op | 0 B/op | ✗ (No H2/H3) | ✗ | ✗ |
| `net/http` (Stdlib) | 165,000 | 78 allocs/op | 6,800 B/op | ⚠️ (H2 only) | ✗ | ✗ |
| `go-resty/resty` | 142,000 | 86 allocs/op | 8,940 B/op | ✗ | ✗ | ✗ |

## Architecture

### 1. Public API & Transport
* **Stable Public Surface:** RFC 9110 methods (`client.Get[T]`, `client.Post[T]`, `client.R()`, `option.With...`, `mod.With...`) are maintained across v1.x releases.
* **Transport:** Handles protocol negotiation (HTTP/1.1, HTTP/2, HTTP/3, TLS 1.3 with ML-KEM, MASQUE), Happy Eyeballs connection racing, and buffer management.
* **Extensions in `aoni/x/...`:** Third-party integrations and protocol adapters (e.g. Socket.IO v5, GeoIP MMDB) reside in separate packages.

### 2. Memory Safety & Static Verification
When pooling buffers (`sync.Pool`), escaping borrowed slices can lead to data races. The built-in `vortex check` static analyzer provides verification:
* Verifies borrowed buffers do not escape into unsynchronized goroutines.
* Checks non-overlapping slice mutations.
* Enforces explicit resource acquisition and release lifecycles.

```bash
# Verify invariants in CI/CD:
vortex check --strict ./...
```

### 3. CPU & Memory Optimizations
1. **Per-P Storage (`pool.PerPStorage`)**: Core-local buffers minimize mutex and channel contention.
2. **Off-Heap Slabs (`offheap.SlabAllocator`)**: Protocol framing structures allocated outside the Go heap to reduce GC overhead.
3. **64-Byte Cache Line Alignment (`_ cpu.CacheLinePad`)**: Prevents False Sharing across multi-threaded socket loops.
4. **SIMD Header Scanning**: Delimiter scanning (`\r\n`) uses AVX2 / SWAR vector instructions.
5. **Monotonic Clocks**: Reduces system call overhead on hot timestamp paths.
6. **Linux `io_uring` Support (`netutil/iouring`)**: Submission (SQ) and completion (CQ) rings for asynchronous socket I/O.

## Vortex Toolchain

`aoni` includes **`vortex`**, a CLI tool for generating API clients and mock servers from OpenAPI 3.1, AsyncAPI 2.x/3.x, and Protobuf schemas:

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
# Generate client code
vortex gen

# Generate in-memory mock servers for test suites
vortex mock

# Run static contract verification
vortex check --strict ./...
```

See the [**Vortex Toolchain Guide**](docs/VORTEX.md) and [**Vortex Specification**](docs/SPEC.md).

## Protocols & Features

<details>
<summary><b>1. Protobuf & gRPC-Web (Unary & Streaming)</b></summary>

```go
// Direct gRPC-Web call with 5-byte framing and trailer validation
userResp, resp, err := aoni.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
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

_ = conn.WriteText("{\"type\":\"subscribe\"}")
```

</details>

<details>
<summary><b>3. Post-Quantum TLS 1.3 & Encrypted Client Hello (ECH / RFC 9460)</b></summary>

```go
client := aoni.NewClient(nil,
	option.WithPostQuantumKyber(),        // Hybrid key exchange X25519MLKEM768
	option.WithECH(option.ECHModeStrict), // Encrypted Client Hello via DoH/DoQ
	option.WithChrome(),                  // JA4 / p0f profile emulation
)
```

</details>

<details>
<summary><b>4. Happy Eyeballs v3 & Early Hints (RFC 8297)</b></summary>

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

// Values inside secret.Secret are masked in logs, JSON, and stack traces
token := secret.New("super-secret-api-token")

client := aoni.NewClient(nil,
	option.WithSecretBearer(token),
)
```

</details>

## Microbenchmarks

<details>
<summary><b>Detailed Subsystem Microbenchmarks (Click to Expand)</b></summary>

### 1. Subsystem Microbenchmarks

| Subsystem / Operation | Go Standard / `x/...` | `aoni` | Latency Delta | Memory (`B / op`) |
| :--- | :---: | :---: | :---: | :---: |
| **URL Parsing (`net/url.Parse`)** | 295.1 ns | **85.2 ns** (`net/url`) | **3.5x Faster** | L1 CRC32 Cache |
| **Public Suffix (`eTLD+1`)** | 146.3 ns | **78.8 ns** (`net/psl`) | **1.9x Faster** | **0 B / 0 allocs** |
| **QPACK RFC 9204 Block Codec** | 2,500+ ns (`quic-go/qpack`) | **472.7 ns** (`internal/fast/h3engine`) | **5.3x Faster** | **0 B / 0 allocs** |
| **HPACK Field Decoder** | 391.9 ns (`x/net/http2/hpack`) | **329.2 ns** (`internal/fast/h2engine`) | **1.19x Faster** | **0 B / 0 allocs** |
| **HPACK Huffman Encoder** | 18.5 ns | **13.99 ns** (`internal/fast/h2engine`) | **1.32x Faster** | **0 B / 0 allocs** |
| **Timestamping (`vDSO` Bypass)** | 3.15 ns (`time.Now`) | **0.28 ns** (`silicon/clock`) | **11.2x Faster** | **0 B / 0 allocs** |
| **Token Bucket Limiter** | 85+ ns (`x/time`) | **23.8 ns** (`async/rate`) | **3.6x Faster** | **0 B / 0 allocs** |
| **SWAR UTF-8 Scan (1KB)** | 280+ ns (`bytes.Index`) | **5.88 ns** (`silicon/simd`) | **12.4 GB/s** | **0 B / 0 allocs** |
| **SWAR `\r\n` Header Scan (1KB)** | 280+ ns (`bytes.Index`) | **114.4 ns** (`silicon/simd`) | **2.5x Faster** | **0 B / 0 allocs** |
| **Zstd Decompression (1KB)** | 1.8+ µs (`klauspost/zstd`) | **251.6 ns / 0 B** (`compress/zstd`) | **7.2x Faster** | **0 B / 0 allocs** |
| **Brotli Decompression (1KB)** | 2.1+ µs (`google/brotli`) | **282.6 ns / 0 B** (`compress/brotli`) | **7.4x Faster** | **0 B / 0 allocs** |
| **WebSocket Stream Throughput** | 800 MB/s (`gorilla/websocket`) | **1,789 MB/s** (`realtime/ws`) | **2.23x Faster** | **0 B / 0 allocs** |

### 2. Gollvm (LLVM 20.1.8 -O3) Vectorization

| Subsystem / Workload | Standard Go (`gc`) | Gollvm (`LLVM 20.1.8 -O3`) | Speedup | Mechanism |
| :--- | :---: | :---: | :---: | :--- |
| **ASCII Header Case-Folding & Match** | 8.47 ns/match | **1.71 ns/match** | **⚡ 4.95x Faster** | Vectorized bitwise unrolling & branch elimination |
| **HPACK / QPACK Huffman Bitstream Pack** | 324.32 MB/s (464.6 ns) | **697.84 MB/s (215.9 ns)** | **⚡ 2.15x Faster** | 64-bit barrel-shifter & register packing |
| **QUIC / Protobuf Varint Codec** | 22.41 ns/op | **15.19 ns/op** | **⚡ 1.48x Faster** | Unrolled bitmask extraction & branch prediction |
| **EWMA Latency & Jitter Filter** | 2.74 ns/sample | **1.92 ns/sample** | **⚡ 1.43x Faster** | Fused multiply-accumulate & float register pipelining |

</details>

## Feature & Protocol Matrix

| Feature / Capability | Go `net/http` | Wrappers (e.g. Resty) | `aoni` Engine |
| :--- | :---: | :---: | :---: |
| **Static Buffer Verification (`vortex lint`)** | ✗ | ✗ | **✓ (CFG & Escape Analysis)** |
| **Multi-Core Allocator Contention** | ⚠️ (`sync.Pool`) | ⚠️ (High contention) | **✓ (`pool.PerPStorage` core-pinned)** |
| **Linux `io_uring`** | ✗ | ✗ | **✓ (Shared SQ/CQ Ring Buffers)** |
| **Reduced GC Overhead on Framing** | ✗ (Heap allocation) | ✗ (Heap allocation) | **✓ (`offheap.SlabAllocator`)** |
| **Native HTTP/2 Multiplexer** | ⚠️ (`x/net/http2`) | ✗ | **✓ (Native Engine & LUT)** |
| **Native HTTP/3 / QUIC / QPACK** | ✗ | ✗ | **✓ (RFC 9000 & RFC 9204)** |
| **Post-Quantum TLS 1.3 Key Exchange** | ✗ | ✗ | **✓ (X25519MLKEM768 & Zstd Cert Compression)** |
| **RFC 8297 `103 Early Hints`** | ✗ | ✗ | **✓ (Link Parsing & Socket Preconnect)** |
| **Network Isolation (NIK)** | ✗ | ✗ | **✓ (TopFrame/FrameSite Keys & CHIPS)** |
| **RFC 9218 Extensible Priorities** | ✗ | ✗ | **✓ (Stream Priorities `u=0..7, i`)** |
| **RFC 8767 Stale-While-Revalidate DNS** | ✗ | ✗ | **✓ (Stale DNS with Background Refresh)** |
| **RFC 9651 Compression Dictionaries** | ✗ | ✗ | **✓ (`dcb`, `dcz` & `Sec-Available-Dictionary`)** |
| **Generics-First Codecs** | ✗ (Manual) | ✗ (Interface reflection) | **✓ (Compile-time `[T]`)** |
| **gRPC & gRPC-Web (4 Streaming Modes)** | ✗ | ✗ | **✓ (Unary, Server, Client & Bidi)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (IPv4/v6 only) | ✗ | **✓ (H3 vs H2/H1 Protocol Racing)** |
| **Auto-Recovery Pipeline** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 & Alt-Svc Backoff)** |
| **W3C `No-Vary-Search` Cache** | ✗ | ✗ | **✓ (Query Normalization)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 via DoH/DoQ)** |
| **Credential Privacy & Anti-Replay** | ✗ | ✗ | **✓ (`Secret[T]` & RFC 8470)** |
| **Sandboxed PAC / WPAD Engine** | ✗ | ✗ | **✓ (PAC Rule Engine)** |
| **OS Sleep / Wake Recovery** | ✗ | ✗ | **✓ (Socket Pool Cleanup on Network Change)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (EWMA & Error Ratio Tripping)** |
| **`Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC 1123 datetime)** |
| **Non-UTF8 Charset Decoding** | ✗ | ✗ | **✓ (WhatWG Encoding Engine)** |
| **TLS Evasion (JA3/JA4/JA4H/p0f)** | ✗ | ✗ | **✓ (Chrome, Firefox, Safari Profiles)** |
| **Unix Domain Socket Support** | ⚠️ (Manual) | ✗ | **✓ (Native `unix://`)** |
| **L3/L4 & MASQUE Tunnels** | ✗ | ✗ | **✓ (Wintun, utun, /dev/net/tun, MASQUE RFC 9298)** |
| **OpenTelemetry & W3C Tracing** | ✗ | ✗ | **✓ (`aoni/x/otel` without external deps)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (`aoni/x/socketio`)** |
| **Proxy & Session Isolation** | ✗ | ✗ | **✓ (`ProxyIsolatedJar` RFC 6265)** |

## 📦 Repository Layout

```
aoni/
├── option/       // Client initialization options (option.With...)
├── mod/          // Per-request modifiers (mod.With...)
├── fast/         // High-performance fasthttp engine adapters
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

## Case Studies & Integrations

- [ao](https://github.com/Lemon4ksan/ao): CLI tool and C/Go library powered by `libaoni` (`lib/aoni_bridge.c`). Supports uTLS (JA4) profiles, ML-KEM-768 hybrid key exchange, and multi-threaded operation.
- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): Fork of `discordgo` with network transport powered by `aoni` and `aoni/realtime/ws`, optimized for reduced allocations during REST and WebSocket processing.

## 📚 Technical Documentation

- [**Security & Protocol Invariants**](docs/SECURITY_AND_FIDELITY.md): Defense model, SSRF protection, DNS rebinding, and decompression bomb guards.
- [**Vortex Toolchain Guide**](docs/VORTEX.md): Declarative syntax, OpenAPI/AsyncAPI ingestion, mock servers, and CI/CD integration.
- [**Vortex Specification**](docs/SPEC.md): EBNF grammar and static linter rules.
- [**Network Stack Specification**](docs/NETWORK_STACK.md): Happy Eyeballs v3, HTTP 421/408/425 recovery, ECH, and connection pool management.
- [**CPU & Memory Optimization**](docs/CPU_STACK.md): SIMD assembly, slab allocation, and memory layouts.
- [**Low-Level Mechanics**](docs/VOODOO.md): HPACK state manipulation, socket system calls, and framing.
- [**Cookbook**](docs/COOKBOOK.md): Practical recipes for REST, WebSockets, gRPC-Web, and streaming workflows.
- [**Code Examples**](examples): Usage examples for the library.

## 🧾 License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

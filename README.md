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

Building modern Go applications usually means assembling a fragile "Frankenstein" of 10+ unmaintained networking packages—gluing together separate libraries for HTTP/3, uTLS, DNS-over-HTTPS, WebSockets, gRPC-Web, SSH tunneling, and resilience. Each package manages its own memory pools and context models, leading to heap bloat, GC latency spikes, and flaky connection failures under load.

`aoni` eliminates this fragmentation. It is the **Unified Internet Protocol Engine for Go** - consolidating all modern IETF RFCs, W3C standards, and Chromium-grade network resilience into a single, profile-driven zero-allocation architecture.

Whether you are building standard microservice REST APIs, high-throughput gateways, real-time WebSockets, or stealthy anti-analysis tools, `aoni` delivers peak silicon performance without compromising on features, memory footprint, or reliability.

```shell
go get github.com/lemon4ksan/aoni
```

## Hard Core Performance: Proven by `pprof`

`aoni` isn't just feature-complete; it sits right at the physical execution limit of the Go runtime. Compared directly against popular HTTP libraries under identical workloads:

| Metric | Resty (`net/http`) | `aoni` (Standard) | `aoni` + `fast.Bridge` | `aoni/fast` (Native) | Advantage (`fast` vs Resty) |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Latency (`ns/op`)** | 58,393 ns | 56,669 ns | 14,127 ns | **5,703 ns** | **~10x Faster** |
| **Heap Memory (`B/op`)** | 9,113 B | 8,217 B | 2,671 B | **363 B** | **~25x Lighter** |
| **Heap Allocations (`allocs/op`)** | 91 allocs | 82 allocs | 34 allocs | **8 allocs** | **~11x Fewer Allocations** |
| **HTTP/2 Latency (`ns/op`)** | 76,519 ns | 75,958 ns | 71,200 ns | **68,164 ns** | **Faster H2 Multiplexing** |
| **HTTP/3 Latency (`ns/op`)** | 131,281 ns | 131,013 ns | 115,400 ns | **111,150 ns** | **Faster H3 QUIC Engine** |
| **Parallel Latency (`ns/op`)** | 11,307 ns | 9,534 ns | 1,940 ns | **589.9 ns** | **~19x Faster Parallel I/O** |
| **Parallel Memory & GC (`B / alloc`)** | 9,113 B / 91 | 8,217 B / 82 | 2,671 B / 34 | **0 B / 0 allocs** | **Absolute Zero Allocation** |
| **Peak Throughput (Single Node)** | ~30k RPS | ~35k RPS | >70,000 RPS | **1,695,000+ RPS** | **Peak Silicon Speed** |

Whether you are calling standard microservice REST endpoints or parsing millions of anti-bot protected pages, `aoni` gives you maximum performance without compromise.

## Unified Ergonomics

Whether you choose standard `aoni` or `aoni/fast`, you drive with the exact same comfortable steering wheel:

```
               ┌──► aoni.Client (100% net/http compatibility & middleware)
option / mod ──┼
               └──► fast.Client (1.5M+ RPS multi-core, zero-alloc fasthttp + H2/H3)
```

* **Need 100% stdlib compatibility & complex middleware?** Use `aoni`.
* **Need absolute, raw silicon throughput & zero-alloc geometry?** Use [`aoni/fast`](fast).

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

## Feature Matrix

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g., Resty) | `aoni` |
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

## Architecture & Domain Modules

```
aoni/
├── option/       // Client initialization options (option.With...)
├── mod/          // Per-request modifiers (mod.With...)
├── request/      // Generic request helpers (request.GetTo[T], PostTo, PostProtoTo)
├── fast/         // Extremely fast net/http compatible client built on top of fasthttp
├── fluent/       // Chainable Request Builder API (fluent.R, FetchTo[T], Codec)
├── cookie/       // Proxy-isolated cookie jars, Netscape format, RFC 6265 path sorting
├── fingerprint/  // TLS/JA4/p0f evasion, HTTP/2 framing, CDN padding
├── netutil/      // Proxy rotators, DoH/DoT DNS resolvers, IPv6 subnet rotators
├── codec/        // Response decoders (JSON, Proto, gRPC-Web, XML) and url.Values encoders
├── realtime/     // WebSocket over H2 CONNECT, Socket.IO v5, SSE & NDJSON streams
├── resiliency/   // Local HTTP response caching, WAF challenge detectors & solvers, load balancers
└── telemetry/    // HAR generators, EWMA latency trackers, embedded web inspector dashboard
```

## Advanced Guides

> **Curious about the network architecture & Chromium resilience?**  
> Read the complete [**Network Stack Specification**](docs/NETWORK_STACK.md) to learn how `aoni` handles Happy Eyeballs v3, 421/408/425 auto-recovery, ECH, and zero-alloc geometry.

> **Need usage examples?**  
> Check out the [examples](examples) directory for runnable code snippets and [evasion examples](examples/evasions) for Playwright/browser integrations.

> **Curious about the network physics?**  
> Read [**Demystifying the Voodoo**](docs/VOODOO.md) to understand how `aoni` manipulates HPACK states, overrides OS-level TCP window sizes via syscalls, and injects chaotic padding without breaking connections.

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

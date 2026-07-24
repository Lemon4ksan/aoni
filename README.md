<div align="center">

# aoni

### The High-Performance, Zero-Alloc Engine for Go HTTP, Protobuf & Real-Time Networks

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)

> _"Zero compromise. Zero-allocation discipline. Unrivaled network resilience."_

#### English • [Русский](README_RU.md)

</div>

## Why Aoni?

When building modern Go applications, developers often face a forced tradeoff: choose a barebones wrapper for speed, or write thousands of lines of boilerplate to handle real-world network challenges like proxy isolation, gRPC-Web, TLS fingerprinting, and WAF challenge solving.

`aoni` eliminates this compromise. It is engineered from the ground up with profile-driven zero-allocation discipline, beating standard HTTP wrappers in both memory footprint and execution speed while delivering full-stack browser-grade evasion, uTLS, JA4, and gRPC-Web capabilities.

```shell
go get github.com/lemon4ksan/aoni
```

## Hard Core Performance: Proven by `pprof`

`aoni` isn't just feature-complete; it sits right at the physical execution limit of the Go runtime. Compared directly against popular HTTP libraries under identical workloads:

| Metric (Single GET Request) | Resty | `aoni` | Advantage |
| :--- | :---: | :---: | :---: |
| **Heap Memory (`B/op`)** | 9,945 B | 9,872 B | Consumes less memory |
| **Heap Allocations (`allocs/op`)** | 97 allocs | 87 allocs | Makes 10 fewer allocations |
| **Multi-Core Parallel Throughput** | 120.6 µs/op | 62.1 µs/op | Is nearly 2x faster |
| **Request Builder Overhead (`.R()`)** | 32 B / 2 allocs | 32 B / 2 allocs | Zero-alloc `sync.Pool` parity |

Whether you are calling standard microservice REST endpoints or parsing millions of anti-bot protected pages, `aoni` gives you maximum performance without compromise.

## Unified Ergonomics

Whether you choose standard `aoni` or `aoni/fast`, you drive with the exact same comfortable steering wheel:

```
               ┌──► aoni.Client (35,000 RPS, 100% net/http compatibility)
option / mod ──┼
               └──► fast.Client (192,000 RPS, 5.9µs latency, zero-alloc)
```

* **Need 100% stdlib compatibility & complex middleware?** Use aoni.
* **Need absolute, raw silicon throughput & zero-alloc geometry?** Use [aoni/fast](fast).

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
		option.WithTLSFingerprint(aoni.BrowserChrome),
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
| **Parallel "Happy Eyeballs" Dialing** | ⚠️ (Basic) | ✗ | **✓ (RFC 8305)** |
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

> **Need usage examples?**  
> Check out the [examples](examples) directory for runnable code snippets and [evasion examples](examples/evasions) for Playwright/browser integrations.

> **Curious about the network physics?**  
> Read [**Demystifying the Voodoo**](docs/VOODOO.md) to understand how `aoni` manipulates HPACK states, overrides OS-level TCP window sizes via syscalls, and injects chaotic padding without breaking connections.

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

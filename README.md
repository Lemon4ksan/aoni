<div align="center">

# aoni

### The Ice-Cold Resilience Engine for Go HTTP & Real-Time Networks

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)

> _"In networks, chaos is the default. Let aoni be your ice-cold anchor."_

#### English • [Русский](README_RU.md)

</div>

## Why Aoni?

When integrating with unstable APIs, scraping, or navigating complex proxy networks in Go, the standard `net/http` client requires significant boilerplate to handle real-world challenges like proxy rotation, rate limits, legacy charsets, or TLS fingerprinting. 

`aoni` bridges this gap. It models HTTP requests as pipeline flows processed by declarative **RequestModifiers** and standard Go **Middlewares**, leveraging generics for type-safe response decoding. It remains unwavering under network load, just like the blue oni.

```shell
go get github.com/lemon4ksan/aoni
```

## When to Use Aoni vs. Standard Clients

`aoni` is not designed to replace `net/http` or lightweight wrappers like `resty` for standard internal microservices where raw, flat throughput over direct, reliable connections is the only concern.

* **Choose `net/http` / `resty`** for: Internal microservices, direct cloud API integrations (AWS, Stripe, Twilio), and standard high-throughput REST APIs where you control the server and network environment.
* **Choose `aoni`** for: Deep-packet inspection (DPI) evasion, scraping/crawling targets behind aggressive firewalls (Cloudflare, Akamai, Imperva), rotating proxy pools with session isolation, and real-time WebSockets / Socket.IO over HTTP/2. It is your **tactical armor** for uncooperative and chaotic network environments.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// Initialize immutable client with functional options
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(15*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	// Request, validate, and decode into T in a single call
	user, err := request.GetTo[User](ctx, client, "/users/{id}",
		mod.WithVar("id", 123),
		mod.WithHeader("X-Custom-Header", "value"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s\n", user.Name)
}
```

## Feature Matrix

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g., Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Generics-first Decoding** | ✗ (Manual) | ✗ (Interface-based) | **✓ (Type-safe `[T]`)** |
| **Parallel "Happy Eyeballs" Dialing** | ⚠️ (Basic) | ✗ | **✓ (RFC 8305)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native Middleware)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC1123)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic)** |
| **TLS Evasion (JA3/JA4)** | ✗ | ✗ | **✓ (via uTLS & Handshake)** |
| **JA4+ Fingerprinting** | ✗ | ✗ | **✓ (TLS & HTTP, pure Go)** |
| **Sub-millisecond Tracing** | ⚠️ (Verbose) | ✗ | **✓ (Single modifier)** |
| **Structured Response Unwrapping** | ✗ | ✗ | **✓ (`WithBaseResponse`)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (Complete v5 Spec)** |
| **Proxy & Session Isolation** | ✗ | ✗ | **✓ (`ProxyIsolatedJar`)** |
| **Per-Request Overrides** | ✗ (Manual transport) | ✗ (Requires client clone) | **✓ (Context Accessors)** |

## Declarative Pipeline Configuration

`aoni` features a modular request pipeline. You can configure execution stages globally via `option.WithPipeline` or override them per-request:

```go
resp, err := client.Get(ctx, "/path", mod.WithPipeline(aoni.PipelineConfig{
	RotateUA:   true,       // Rotates User-Agent & Client Hints consistently
	Decompress: true,       // Handles gzip, brotli, and zstd automatically
	Challenge:  true,       // Detects WAF challenge pages (e.g., Cloudflare)
	Validate:   true,       // Triggers registered response validators
}))
```

`aoni` uses a two-tier configuration model allowing client-wide defaults while reserving transport fine-tuning for individual requests:

```
Request-level Option -> Client-level Default -> System Environment / Transport Default
```

## Architecture & Domain Modules

```
aoni/
├── option/       // Client initialization options (option.With...)
├── mod/          // Per-request modifiers (mod.With...)
├── request/      // Generic request helpers (request.GetTo[T], PostTo, Concurrent)
├── cookie/       // Proxy-isolated cookie jars, JSON/SQL persistence, Playwright export/import
├── fingerprint/  // TLS/JA4/p0f evasion, HTTP/2 framing, CDN padding
├── netutil/      // Proxy rotators, DoH/DoT DNS resolvers, IPv6 subnet rotators
├── codec/        // Response decoders and url.Values struct encoders
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

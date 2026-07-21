<div align="center">

# ❄️ aoni

### The Ice-Cold Resilience Engine for Go HTTP & Real-Time Networks

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)

> _"In networks, chaos is the default. Let aoni be your ice-cold anchor."_

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

## Why Aoni?

When integrating with unstable APIs, scraping, or working with complex proxy networks in Go, the standard `net/http` client often requires significant boilerplate to handle real-world challenges like proxy rotation, rate limits, legacy charsets, or TLS fingerprinting. 

`aoni` bridges this gap. It models HTTP requests as pipeline flows processed by declarative **RequestModifiers** and standard Go **Middlewares**, leveraging generics for type-safe response decoding. It remains unwavering under network load, just like the blue oni.

```shell
go get github.com/lemon4ksan/aoni
```

## 🎯 When to Use Aoni vs. Standard Clients (e.g., Resty)

`aoni` is not designed to replace `net/http` or lightweight wrappers like `resty` for standard, internal corporate microservices where raw, flat throughput over direct, reliable cloud connections is the only concern.

* **Choose `net/http` / `resty`** for: Internal microservices, direct cloud API integrations (AWS/S3, Stripe, Twilio), and standard high-throughput REST APIs where you fully control the destination server and the network environment.
* **Choose `aoni`** for: Deep-packet inspection (DPI) evasion, scraping/crawling targets behind aggressive firewalls (Cloudflare, Akamai, Imperva), rotating unstable proxy networks with sticky sessions, and real-time WebSockets over HTTP/2. It is your **tactical off-road armor** for uncooperative and chaotic network environments.

## 🚀 Quick Start

To make a JSON request through a resilient proxy pool with retries and custom error parsing, standard Go requires manual loop management and verbose transport setup. With `aoni`, it is a clean, immutable, pipeline-driven flow:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// Establish global defaults. Immutable cloned client is returned.
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(15*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	// Fetch, validate, and decode in one step
	user, err := request.GetTo[User](ctx, client, "/users/{id}",
		mod.WithVar("id", 123),
		mod.WithHeader("X-Custom", "value"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s\n", user.Name)
}
```

## 📊 Feature Matrix

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g., Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Generics-first Decoding** | ✗ (Manual) | ✗ (Interface-based) | **✓ (Type-safe `[T]`)** |
| **Parallel "Happy Eyeballs" Dialing** | ⚠️ (Basic) | ✗ | **✓ (RFC 8305)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native Middleware)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC1123)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic)** |
| **TLS Evasion (JA3/JA4)** | ✗ | ✗ | **✓ (via `uTLS` & Handshake)** |
| **JA4+ Fingerprinting** | ✗ | ✗ | **✓ (TLS & HTTP, pure Go)** |
| **Sub-millisecond Tracing** | ⚠️ (Verbose) | ✗ | **✓ (Single-modifier)** |
| **Structured Response Unwrapping** | ✗ | ✗ | **✓ (`WithBaseResponse`)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (Complete v5 Spec)** |
| **Per-Request Overrides** | ✗ (Manual transport) | ✗ (Requires client clone) | **✓ (Context Accessors)** |

## 🧱 Declarative Pipeline Configuration

`aoni` features a modular request pipeline. You can configure execution stages globally via `option.WithPipeline` or override them for a single request using `WithPipeline`:

```go
resp, err := client.Get(ctx, "/path", mod.WithPipeline(aoni.PipelineConfig{
	RotateUA:   true,       // Rotates User-Agent & Client Hints consistently
	Decompress: true,       // Handles gzip, brotli, and zstd automatically
	Challenge:  true,       // Detects WAF challenge pages (e.g., Cloudflare)
	Validate:   true,       // Triggers registered response validators
}))
```

`aoni` also uses a two-tier configuration model allowing you to establish client-wide defaults while reserving the ability to fine-tune transport behavior for individual requests via context-bound `RequestModifier` options.

```
Request-level Option -> Client-level Default -> System Environment / Transport Default
```

## 🔮 Unrevel the magic

Standard HTTP clients are designed for static, cooperative networks. `aoni` operates on the fringes of network protocols to evade deep-packet inspection and bypass aggressive firewalls.

> **Need usage examples?**
> Check out [examples](examples) directory to learn how to utilize aoni and [evasion examples](examples/evasions) to see how to integrate the client with browser emulators like Playwright.

> **Curious about the network physics?**  
> Read our advanced guide: [**Demystifying the Voodoo**](docs/VOODOO.md) to understand how `aoni` manipulates HPACK states, overrides OS-level TCP window sizes via syscalls, and injects chaotic padding without breaking connections.

## ⚖️ Legal & License

This project is licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

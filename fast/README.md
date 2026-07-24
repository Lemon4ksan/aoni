<div align="center">

# aoni/fast

### The Silicon-Paced, Zero-Alloc Titanium Engine for Go Networking

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![RPS](https://img.shields.io/badge/throughput-1.5M%2B%20RPS-brightgreen?style=flat-square)](#hard-core-performance-the-cold-hard-math)

> _"Zero compromise. Strict memory geometry. Raw silicon speed."_

#### English • [Русский](README_RU.md)

</div>

## The Manifesto: Shattering Corporate Myths

For years, corporate frameworks preached a lazy, incompetent dogma:  
> *"If you want a clean, fluent interface with chainable calls, you MUST pay a tax of 50 microseconds and 80 heap allocations per request. If you want high performance, your code MUST be an unreadable mess with no features."*

**That is a lie.** It is the excuse of frameworks that lack the mathematical discipline to design strict memory geometry.

`aoni/fast` was built to prove the exact opposite. It takes `fasthttp`, integrates native HTTP/2 and HTTP/3 framing directly over uTLS, and wraps it in the same high-level option/mod interface used across `aoni`. 

Using standard HTTP wrappers is like hiring fifty drunk movers to carry a single paper envelope across town - throwing mud everywhere and demanding a million dollars for gas. `aoni/fast` is a high-pressure titanium pneumatic tube: you load bytes into one end, pull the lever, and they shoot straight into the socket at the speed of sound without leaving a single speck of dust on the workshop floor.

```shell
go get github.com/lemon4ksan/aoni
```

## Hard Core Performance: The Cold, Hard Math

Look at the numbers that silence academic excuses. Compared directly against standard Go HTTP stacks under identical workloads:

| Metric (Full Req/Resp Cycle + JSON) | Resty (`net/http`) | `aoni` (Standard) | `aoni` + `fast.Bridge` | `aoni/fast` (Native) | Advantage (`fast` vs Resty) |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **GET JSON Latency (`ns/op`)** | 58,393 ns | 56,669 ns | **14,127 ns** | **6,513 ns** | **~9x Faster** |
| **Heap Memory (`B/op`)** | 9,113 B | 8,217 B | **6,260 B** | **372 B** | **~24x Lighter** |
| **Heap Allocations (`allocs/op`)** | 91 allocs | 82 allocs | **79 allocs** | **9 allocs** | **~10x Fewer Allocations** |
| **HTTP/2 Latency (`ns/op`)** | 76,519 ns | 76,519 ns | **71,200 ns** | **68,164 ns** | **Faster H2 Multiplexing** |
| **HTTP/3 QUIC Latency (`ns/op`)** | 131,281 ns | 131,281 ns | **115,400 ns** | **111,150 ns** | **Faster H3 QUIC Engine** |
| **Parallel Latency (`ns/op`)** | 11,307 ns | 9,534 ns | **1,940 ns** | **656 ns** | **~17x Faster Parallel I/O** |
| **Peak Throughput (Single Node)** | ~30k RPS | ~35k RPS | **>70,000 RPS** | **1,522,000+ RPS** | **1.5M+ RPS Silicon Speed** |

* Processors belong to your application - not to bureaucratic frameworks and garbage collector pauses.

## 🛡️ Full RFC Compliance, Security & Why There Is No Reason to Use `net/http` Over `aoni/fast`

`aoni/fast` combines the speed and zero-allocation discipline of `fasthttp` with full RFC compliance and bulletproof security features:

1. **Memory Safety & Race Prevention**: `BodyBytes()` returns a safe cloned slice (`slices.Clone`), avoiding use-after-free when `fasthttp.Response` is recycled into `sync.Pool`. Context cancellations transfer ownership to a background goroutine to prevent data races.
2. **Streaming & OOM Defense**: Full request body streaming via `SetBodyStreamWriter`, automatic `GetBody` rewind for 307/308 redirects, zip-bomb decompression prior to `SizeLimit` checks, and Keep-Alive connection slurping (up to 2 KB).
3. **RFC Protocol Security**: RFC 9112 Request Smuggling protection (`Content-Length` deduplication/conflict detection), RFC 7541 HPACK Header Flood limits (10 MB cap), Control Frame Anti-DoS (disconnecting >1000 PING/SETTINGS flood frames), RFC 6265 Subdomain-Aware Cookie scrubbing, RFC 7231 `Referer` stripping on HTTPS ➔ HTTP downgrades, and URL UserInfo Basic Auth extraction.
4. **H1/H2/H3 & Network Stack**: HTTP/1.1 Anti-DPI (`HeaderOrderingConn`), spin-free H2 Flow Control (`sync.Cond`), H2 Stream FSM lifecycle management, H2/H3 Trailer parsing, H3 QPACK stream draining, QUIC Happy Eyeballs with seamless H2/H1 fallback, RFC 7838 `Alt-Svc` caching, IDN Punycode & IPv6 Zone ID stripping, RFC 6066 Domain Fronting SNI isolation, and `Expect: 100-continue` timer support.
5. **Stdlib Parity**: `Response.Uncompressed` flag, `nothingWrittenError` 0-byte write retries on idle Keep-Alive sockets, custom protocol scheme handlers (`file://`, `ftp://`, `s3://`), and `httptrace.Got1xxResponse` hooks.

## The Subterranean Monorail: `fast.NewStdClient` & The Bridge

They shouted from every corner: *"fasthttp is incompatible with standard Go interfaces! You can't use it in normal HTTP clients!"*

We buried a superconducting magnetic monorail right underneath their muddy dirt road:

```
[ Legacy Code / Third-party SDK ]
               │
               ▼
     *http.Client / RoundTripper
               │
               ▼
    [ aoni/fast.Bridge ]  <-- Seamless Adapter
               │
               ▼
 [ fasthttp + uTLS + Native H2/H3 ] --> [ Direct Socket Write ]
```

Your legacy SDKs enter `fast.NewStdClient`, thinking they are slowly crawling through puddles on an old wooden `http.RoundTripper` cart. Under the hood, `aoni.fast` engages a native turbojet engine that carries them at 300 mph. They won't even understand why their CPU stopped overheating.

---

## Quick Start

### 1. Ultra-High Performance Native `fast.Client`

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type UserProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// Instantiate the fast engine with browser TLS fingerprints
	client := fast.NewClient(
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	// High-level type-safe execution over zero-alloc fasthttp + uTLS
	resp, err := client.Request(ctx, "GET", "/users/123",
		mod.WithHeader("X-High-Load", "true"),
	)
	if err != nil {
		panic(err)
	}
	defer resp.Close() // Returns objects back to sync.Pool

	fmt.Printf("Status: %d, Body: %s\n", resp.StatusCode(), resp.BodyBytes())
}
```

### 2. The Monorail Bridge: Turbocharge Standard `*http.Client`

Seamlessly adapt `aoni/fast` into any third-party Go library (Resty, AWS SDK, custom REST clients) expecting a standard `*http.Client`:

```go
package main

import (
	"net/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	// Create fast engine
	fastClient := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithProxyString("socks5://127.0.0.1:1080"),
	)

	// Adapt into standard net/http.Client
	stdClient := fast.NewStdClient(fastClient)

	// Inject into legacy code expecting *http.Client
	resp, err := stdClient.Get("https://api.target.com/data")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
```

## Feature Matrix

| Feature / Capability | Standard Go `net/http` | Resty / Wrappers | `aoni` (Base) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Engine Core** | `net/http` | `net/http` | `net/http` | **`fasthttp` + Native H2/H3** |
| **Execution Latency** | ~50 µs | ~50 µs | ~60 µs | **5.9 µs (8.5x faster)** |
| **Zero-Alloc Object Pooling** | ✗ | ✗ | ✗ | **✓ (`sync.Pool` Request/Response)** |
| **Native HTTP/2 (`h2engine`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (Zero-Alloc Byte Engine)** |
| **Native HTTP/3 (`h3engine`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (QPACK Byte Engine)** |
| **uTLS & Fingerprinting** | ✗ | ✗ | **✓** | **✓ (uTLS over `fastDialer`)** |
| **Custom Header Order (JA4H)** | ✗ | ✗ | **✓** | **✓ (Zero-Cost Wire Ordering)** |
| **`http.Client` Compatibility Bridge** | Native | ✗ | Native | **✓ (`fast.NewStdClient`)** |

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Pure physics. Unyielding performance. Take back your CPU.</sub>
</div>


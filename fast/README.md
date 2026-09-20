<div align="center">

# aoni/fast

### Zero-Alloc High-Throughput Engine for Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![Throughput](https://img.shields.io/badge/throughput-193k%20H1%20%7C%2070k%20H2%20%7C%202.12M%20In--Memory-brightgreen?style=flat-square)](#empirical-benchmarks--protocol-breakdown)

> _"Strict memory geometry. Raw hardware speed."_

#### English • [Русский](README_RU.md)

</div>

## Manifesto: Against Bloatware

For years, frameworks pushed a lazy dogma:  
> *"If you want a clean, fluent interface, you MUST pay a tax of 50 microseconds and 80 heap allocations per request. If you want speed, your code MUST be an unreadable spaghetti mess on bare pointers."*

That is pure nonsense and an excuse for laziness.

`aoni/fast` takes `mach`, integrates native HTTP/2 and HTTP/3 framing directly over uTLS, and wraps it in the clean option/mod interface from `aoni`.

Using bloated HTTP wrappers is like hiring a crowd of fifty drunk movers to carry a single paper envelope across town — tearing up the dirt and burning a whole tank of gas. `aoni/fast` is a straight pneumatic tube: you load bytes into the socket, pull the lever, and they fly onto the wire without dropping a single allocated byte on the floor.

```shell
go get github.com/lemon4ksan/aoni
```

## Feature Matrix

| Feature / Capability | Standard Go `net/http` | Resty / Wrappers | `aoni` (Base) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Engine Core** | `net/http` | `net/http` | `net/http` | **`mach` + Native H2/H3** |
| **HTTP/1.1 Throughput (Parallel)** | ~17.3k RPS | ~16k RPS | ~17k RPS | **151k – 193k RPS (8.7x – 11x)** |
| **HTTP/1.1 Latency** | 57.6 µs | ~60 µs | 56 µs | **6.6 µs (8.7x faster)** |
| **HTTP/2 Throughput (TLS)** | 43.4k RPS | ~40k RPS | ~40k RPS | **60.5k – 69.3k RPS** |
| **HTTP/2 Latency (TLS)** | 23.0 µs | ~25 µs | ~24 µs | **16.5 µs (1.4x faster)** |
| **HTTP/3 QUIC Throughput** | N/A | N/A | N/A | **1,091 RPS** |
| **In-Memory Core (Zero-IO)** | N/A | N/A | N/A | **2,126,754 RPS (0.47 µs)** |
| **Zero-Alloc Object Pooling** | ✗ (73-83 allocs) | ✗ (>90 allocs) | ✗ | **✓ (`PerPStorage` Request/Response)** |
| **Native HTTP/2 (`h2`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (`mach/client/h2` Singleflight)** |
| **Native HTTP/3 (`h3`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (`mach/client/h3` + QPACK)** |
| **uTLS & Fingerprinting** | ✗ | ✗ | **✓** | **✓ (uTLS over `fastDialer`)** |
| **Custom Header Order (JA4H)** | ✗ | ✗ | **✓** | **✓** |
| **`http.Client` Compatibility Bridge** | Native | ✗ | Native | **✓ (`fast.NewStdClient`)** |

## Empirical Benchmarks & Protocol Breakdown

Measured on **Intel Core i5-12400F @ 4.4 GHz (6 Cores / 12 Threads)**, Windows amd64, using Go 1.27 (`go test -bench=BenchmarkFast_ -benchmem ./tests`). Real network loopback sockets (TCP / TLS / UDP QUIC).

### 1. Multi-Protocol Performance Comparison (Parallel, 12 Workers)

| Protocol / Client Stack | Requests / sec (**RPS**) | Latency (`ns/op`) | Memory (`B/op`) | Allocs (`allocs/op`) | Speedup vs `net/http` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (In-Memory Core)** | **2,126,754 RPS** | **0.47 µs** (470 ns) | 0 B | 0 allocs | *Pure CPU Pipeline* |
| **`aoni/fast` (HTTP/1.1 TCP)** | **151,446 – 193,000 RPS** | **6.6 µs** (6,603 ns) | **2,160 B** | **21 allocs** | **8.7x – 11.1x RPS** |
| `net/http` (HTTP/1.1 TCP) | 17,356 RPS | 57.6 µs (57,616 ns) | 11,230 B | 83 allocs | Baseline (1.0x) |
| **`aoni/fast` (HTTP/2 TLS)** | **60,569 – 69,300 RPS** | **16.5 µs** (16,510 ns) | **3,197 B** | **32 allocs** | **1.4x – 1.6x RPS** |
| `net/http` (HTTP/2 TLS) | 43,385 RPS | 23.0 µs (23,049 ns) | 8,569 B | 73 allocs | Baseline (1.0x) |
| **`aoni/fast` (HTTP/3 QUIC)** | **1,091 RPS** | **916 µs** (0.91 ms) | 130,404 B | 155 allocs | *Multiplexed UDP* |

### 2. Sequential (Single-Thread) Latency

| Protocol | Requests / sec (**RPS**) | Latency (`ns/op`) | Microarchitectural Characteristics |
| :--- | :---: | :---: | :--- |
| **HTTP/1.1 Keep-Alive** | **23,195 RPS** | 43.1 µs | Single-stream synchronous TCP roundtrip |
| **HTTP/2 TLS Multiplex** | **12,468 RPS** | 80.2 µs | TLS frame encryption + HPACK dynamic table compression |
| **HTTP/3 QUIC Multiplex** | **50.5 RPS** | 19.8 ms | Limited by QUIC `MaxAckDelay` (25 ms RFC 9000) on loopback |

### 3. Why Numbers Differ Across Protocols

* **HTTP/1.1 (Raw Throughput King on LAN / Microservices)**: Writes monolithic byte buffers directly to TCP Keep-Alive sockets. Zero framing overhead, 6.6 µs latency, saturating OS network limits at up to ~193k RPS.
* **HTTP/2 (Optimal for Web Traffic & Concurrency)**: Multiplexes concurrent requests over a single TLS connection with singleflight dialing. HPACK and TLS framing add ~10 µs, achieving ~70k RPS with 2.7x less RAM than `net/http`.
* **HTTP/3 (Engineered for Lossy & Mobile Networks)**: Every request instantiates an RFC 9114 bidirectional QUIC stream state machine (`quic.Stream`, `frameSorter`, flow control). On local loopback, QUIC timer granularities (~20ms ACK delay) dominate sequential latency, while parallel multiplexing achieves >1,000 RPS. Its architectural superpower is zero Head-of-Line blocking under packet loss and seamless Connection Migration across networks.

## Compatibility Bridge: `fast.NewStdClient`

They claimed: *"fasthttp is incompatible with standard Go interfaces! You can't use it in normal libraries!"*

You can. That is what the bridge is for:

```
[ Legacy Code / Third-Party SDK ]
                │
                ▼
      *http.Client / RoundTripper
                │
                ▼
     [ aoni/fast.Bridge ]  <-- Adapter
                │
                ▼
  [ fasthttp + uTLS + H2/H3 ] --> [ Direct Socket Write ]
```

Your code thinks it is casually rolling along on a standard `http.RoundTripper`. Under the hood, `aoni/fast` drives the socket at millions of RPS, and your CPU suddenly stops heating the room.

## 🛡️ RFC Compliance & Security Mechanisms

`aoni/fast` pairs raw speed with production safeguards:

1. **Memory Safety & Race Prevention**: `BodyBytes()` returns a cloned slice (`slices.Clone`) to prevent use-after-free when `fasthttp.Response` returns to `sync.Pool`. Context cancellations transfer ownership to a background goroutine to avoid data races.
2. **Streaming & Size Limits**: Request body streaming via `SetBodyStreamWriter`, automatic `GetBody` rewind for 307/308 redirects, decompression prior to `SizeLimit` checks, and Keep-Alive connection slurping (up to 2 KB).
3. **Protocol Security**: RFC 9112 Request Smuggling protection (`Content-Length` conflict handling), RFC 7541 HPACK Header Flood limits (10 MB cap), Control Frame Anti-DoS protection, RFC 6265 cookie scrubbing on cross-domain redirects, RFC 7231 `Referer` stripping on HTTPS ➔ HTTP downgrades, and URL UserInfo Basic Auth parsing.
4. **H1/H2/H3 Support**: HTTP/1.1 header ordering (`HeaderOrderingConn`), `sync.Cond`-based H2 flow control, H2 stream lifecycle FSM, H2/H3 trailer support, QUIC Happy Eyeballs with H2/H1 fallback, RFC 7838 `Alt-Svc` caching, IDN Punycode and IPv6 Zone ID handling, and `Expect: 100-continue` timer support.
5. **Standard Library Compatibility**: `Response.Uncompressed` flag, 0-byte write retries on idle Keep-Alive sockets, custom protocol scheme handlers, and `httptrace.Got1xxResponse` hooks.

## Quickstart

### 1. Direct `fast.Client` Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	ctx := context.Background()

	client := fast.NewClient(
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	resp, err := client.Request(ctx, "GET", "/users/123",
		mod.WithHeader("X-High-Load", "true"),
	)
	if err != nil {
		panic(err)
	}
	defer resp.Close() // Returns pooled objects

	fmt.Printf("Status: %d, Body: %s\n", resp.StatusCode(), resp.BodyBytes())
}
```

### 2. Standard `*http.Client` Adapter

```go
package main

import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	fastClient := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithProxyString("socks5://127.0.0.1:1080"),
	)

	// Adapt into standard net/http.Client
	stdClient := fast.NewStdClient(fastClient)

	resp, err := stdClient.Get("https://api.target.com/data")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
```

## License

Licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for details.

<div align="center">
  <sub>Strict memory geometry. Take back your CPU clock cycles.</sub>
</div>

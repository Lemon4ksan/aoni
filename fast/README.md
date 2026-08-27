<div align="center">

# aoni/fast

### Zero-Alloc High-Throughput Engine for Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![RPS](https://img.shields.io/badge/throughput-2.00M%2B%20RPS-brightgreen?style=flat-square)](#feature-matrix)

> _"Strict memory geometry. Raw hardware speed."_

#### English • [Русский](README_RU.md)

</div>

## Manifesto: Against Bloatware

For years, frameworks pushed a lazy dogma:  
> *"If you want a clean, fluent interface, you MUST pay a tax of 50 microseconds and 80 heap allocations per request. If you want speed, your code MUST be an unreadable spaghetti mess on bare pointers."*

That is pure nonsense and an excuse for laziness.

`aoni/fast` takes `fasthttp`, integrates native HTTP/2 and HTTP/3 framing directly over uTLS, and wraps it in the clean option/mod interface from `aoni`.

Using bloated HTTP wrappers is like hiring a crowd of fifty drunk movers to carry a single paper envelope across town — tearing up the dirt and burning a whole tank of gas. `aoni/fast` is a straight pneumatic tube: you load bytes into the socket, pull the lever, and they fly onto the wire without dropping a single allocated byte on the floor.

```shell
go get github.com/lemon4ksan/aoni
```

## Feature Matrix

| Feature / Capability | Standard Go `net/http` | Resty / Wrappers | `aoni` (Base) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Engine Core** | `net/http` | `net/http` | `net/http` | **`fasthttp` + Native H2/H3** |
| **Execution Latency** | ~50 µs | ~50 µs | ~56 µs | **5.9 µs** |
| **Zero-Alloc Object Pooling** | ✗ | ✗ | ✗ | **✓ (`sync.Pool` Request/Response)** |
| **Native HTTP/2 (`h2engine`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (Zero-Alloc Byte Engine)** |
| **Native HTTP/3 (`h3engine`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (QPACK Byte Engine)** |
| **uTLS & Fingerprinting** | ✗ | ✗ | **✓** | **✓ (uTLS over `fastDialer`)** |
| **Custom Header Order (JA4H)** | ✗ | ✗ | **✓** | **✓** |
| **`http.Client` Compatibility Bridge** | Native | ✗ | Native | **✓ (`fast.NewStdClient`)** |

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

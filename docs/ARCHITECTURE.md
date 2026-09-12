# Architecture & System Design

This document describes the layered architecture, package boundaries, and concurrency/memory invariants of the `aoni` repository.

```text
    ┌──────────────────────────────────────────────────────────────────┐
    │                      Layer 4: Public APIs                        │
    │   aoni (net/http client) • fast (fasthttp/H2/H3) • option • mod  │
    └─────────────────────────────────┬────────────────────────────────┘
                                      │
    ┌─────────────────────────────────▼────────────────────────────────┐
    │              Layer 3: Policies, Codecs & Features                │
    │   resiliency • cookie • codec • realtime • fingerprint • tunnel  │
    └─────────────────────────────────┬────────────────────────────────┘
                                      │
    ┌─────────────────────────────────▼────────────────────────────────┐
    │                Layer 2: Engines & Pipeline                       │
    │   internal/pipeline • internal/fast/h1/h2/h3 • internal/core     │
    └─────────────────────────────────┬────────────────────────────────┘
                                      │
    ┌─────────────────────────────────▼────────────────────────────────┐
    │              Layer 1: Wire Protocols & OS Details                │
    │   internal/quic • internal/qpack • internal/sys                  │
    └──────────────────────────────────────────────────────────────────┘
```

## 1. Layered Architecture

The repository is structured into four layers with strict unidirectional dependency rules: higher layers may depend on lower layers; lower layers must not depend on higher layers.

### Layer 4: Public APIs
* **`package aoni`**: Primary HTTP client facade (`aoni.Client`). Compatible with `net/http.RoundTripper`, standard contexts, and standard Go middleware.
* **`package fast`**: High-throughput engine (`fast.Client`) built on `fasthttp` with native HTTP/2 and HTTP/3 support.
* **`package option`**: Client-level configuration options (`option.WithBaseURL`, `option.WithTimeout`, `option.WithChrome`).
* **`package mod`**: Request-level modifiers (`mod.WithHeader`, `mod.WithBearer`, `mod.WithJSON`).

### Layer 3: Policies, Codecs & Extensions
* **`cookie`**: RFC 6265bis compliant cookie storage and proxy-isolated jars.
* **`codec`**: Decoders for JSON, XML, Protocol Buffers, and gRPC-Web framing.
* **`resiliency`**: Retries, backoff strategies, circuit breakers, caching (RFC 9111), and connection hedging.
* **`fingerprint`**: Browser impersonation profiles (Chrome, Firefox, Safari), JA4 hashing, and TLS ClientHello customization.
* **`realtime`**: WebSockets, Server-Sent Events (SSE), and NDJSON streaming.
* **`tunnel`**: SOCKS5, HTTP CONNECT, and MASQUE (RFC 9298) encapsulation.

### Layer 2: Execution Engines & Pipeline
* **`internal/pipeline`**: 5-stage middleware pipeline (pre-flight, mutation, transport execution, post-flight, validation).
* **`internal/fast/h1engine`**: HTTP/1.1 socket pooling and connection management.
* **`internal/fast/h2engine`**: Native HTTP/2 multiplexing with HPACK compression and framing.
* **`internal/fast/h3engine`**: HTTP/3 connection management and QPACK integration.
* **`internal/core`**: Common internal data structures (`ModifierAtom`, retry policies).

### Layer 1: Wire Protocols & OS Primitives
* **`internal/quic`**: Pure-Go implementation of RFC 9000 (QUIC) and RFC 9002 (congestion control). Encapsulated: client packages do not import QUIC directly.
* **`internal/qpack`**: RFC 9204 QPACK field compression for HTTP/3.
* **`internal/sys`**: OS thread affinity and hardware capability detection.

## 2. Subsystem Isolation & Import Boundaries

### Rule 1: QUIC Encapsulation
* `internal/quic` is a private protocol implementation.
* Neither `aoni` nor `fast` may import `internal/quic`.
* HTTP/3 configuration is handled through `internal/fast/h3engine.NewClientFromSettings()`, utilizing `fingerprint/h3.Settings`.
* Only protocol drivers (`h3engine`, `x/webtransport`, `tunnel/masque`, `netutil/dns/doq`) may import QUIC.

### Rule 2: Engine Decoupling
* `aoni.Client` (`net/http`) and `fast.Client` (`fasthttp`) are independent.
* `aoni` does not import `fast`.
* Both engines share atomic modifier primitives (`aoni.RequestModifier`) and data contracts (`aoni.Request`, `aoni.Response`, `aoni.HTTPRequester`).

### Rule 3: Client vs Request Configuration
* **Client options**: Reside in `package option`. Mutate `*aoni.Config`.
* **Request modifiers**: Reside in `package mod`. Produce `aoni.RequestModifier`.

## 3. Engine Comparison

| Characteristic | `aoni.Client` (Standard) | `fast.Client` (Fast Path) |
| :--- | :--- | :--- |
| **Engine** | `net/http` (`http.RoundTripper`) | `fasthttp` + native H2/H3 |
| **Compatibility** | Drop-in for standard library | Specialized API |
| **Protocols** | HTTP/1.1, HTTP/2 | HTTP/1.1, HTTP/2, HTTP/3 |
| **Memory Model** | Standard Go GC | Pooled buffers (`sync.Pool`) |
| **Response Body** | Stream (`io.ReadCloser`) | Stream or volatile byte buffer |

## 4. Concurrency & Memory Invariants

### Client Immutability
`Client` instances are immutable after creation:
* Public methods are safe for concurrent invocation.
* Modifying state via `client.With(opts...)` or `client.Clone()` returns a new `Client` instance with deep-copied configurations.

### RequestBuilder Lifecycle
`RequestBuilder` (`client.R()`) is pooled via `sync.Pool`:
* **Not thread-safe**: Must be created, configured, and executed within a single goroutine.
* **Single-use guard**: Once `Execute()` or `Release()` is called, `consumed = true`. Subsequent executions return `ErrBuilderConsumed`.

### Buffer Lifetime in `fast.Client`
* `fast.Response` reuses internal byte buffers across requests.
* Slices returned by `resp.BodyBytes()` or `resp.UnsafeBodyBytes()` are valid only until request lifecycle completion or `resp.Close()`.
* Persistent payloads must be copied via `bytes.Clone()` or `resp.String()`.

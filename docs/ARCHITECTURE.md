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

---

## 1. The 4-Tier Layered Architecture

The repository is structured into four distinct layers with strict unidirectional dependency rules: higher layers may depend on lower layers, but lower layers never depend on higher layers.

### Layer 4: Public APIs
The entry points consumed by application code:
- **`package aoni`**: The primary HTTP client facade (`aoni.Client`). Fully compatible with `net/http.RoundTripper`, standard contexts, and standard Go middleware.
- **`package fast`**: Dedicated high-throughput engine (`fast.Client`) built on top of `fasthttp` with native HTTP/2 and HTTP/3 support for latency-critical and high-concurrency workloads.
- **`package option`**: Client-level configuration options applied at construction (`option.WithBaseURL`, `option.WithTimeout`, `option.WithChrome`).
- **`package mod`**: Request-level modifiers applied per call (`mod.WithHeader`, `mod.WithBearer`, `mod.WithJSON`).

### Layer 3: Policies, Codecs & Extensions
Self-contained packages that provide specialized networking capabilities:
- **`cookie`**: RFC 6265bis compliant cookie storage and proxy-isolated jars (`ProxyIsolatedJar`).
- **`codec`**: Type-safe decoders for JSON, XML, Protocol Buffers, and gRPC-Web framing.
- **`resiliency`**: Retries, backoff strategies, circuit breakers, caching (RFC 9111), and connection hedging.
- **`fingerprint`**: Browser impersonation profiles (Chrome, Firefox, Safari), JA4 hashing, and TLS ClientHello customization.
- **`realtime`**: WebSockets, Server-Sent Events (SSE), and chunked NDJSON streaming.
- **`tunnel`**: SOCKS5, HTTP CONNECT, and MASQUE (RFC 9298) encapsulation.

### Layer 2: Execution Engines & Pipeline
Internal orchestration layers that coordinate requests:
- **`internal/pipeline`**: The 5-stage middleware pipeline (pre-flight, request mutation, transport execution, post-flight, response validation).
- **`internal/fast/h1engine`**: HTTP/1.1 socket pooling and connection management based on `fasthttp`.
- **`internal/fast/h2engine`**: Native HTTP/2 multiplexing with HPACK compression and framing.
- **`internal/fast/h3engine`**: HTTP/3 connection management and QPACK integration over QUIC.
- **`internal/core`**: Common internal data structures (`ModifierAtom`, retry policies).

### Layer 1: Wire Protocols & OS Primitives
Low-level protocol implementations and OS kernel interfaces:
- **`internal/quic`**: Pure-Go implementation of IETF RFC 9000 (QUIC transport) and RFC 9002 (congestion control). **Strictly encapsulated**: client packages do not import QUIC directly.
- **`internal/qpack`**: RFC 9204 QPACK field compression for HTTP/3.
- **`internal/sys`**: OS thread affinity and hardware capability detection.

---

## 2. Subsystem Isolation & Import Boundaries

To keep the codebase maintainable and prevent deep protocol details from leaking into public APIs, the following boundary rules are strictly enforced:

### Rule 1: QUIC Encapsulation
- `internal/quic` is a private protocol implementation.
- Neither `package aoni` nor `package fast` may import `internal/quic`.
- HTTP/3 client configuration is handled entirely through `internal/fast/h3engine.NewClientFromSettings()`, taking high-level settings from `fingerprint/h3.Settings`.
- Outside of `internal/quic`, only protocol drivers (`h3engine`, `x/webtransport`, `tunnel/masque`, `netutil/dns/doq`) may import QUIC.

### Rule 2: Dual Engine Decoupling
- `aoni.Client` (`net/http`) and `fast.Client` (`fasthttp`) are independent engines.
- `aoni` does not import `fast`.
- Both engines share the same atomic modifier primitive (`aoni.RequestModifier`) and data contracts (`aoni.Request`, `aoni.Response`, `aoni.HTTPRequester`).

### Rule 3: Client vs Request Configuration Separation
- **Client options** belong exclusively in `package option` (`option.With...`). They mutate `*aoni.Config`.
- **Request modifiers** belong exclusively in `package mod` (`mod.With...`). They produce `aoni.RequestModifier` (`core.ModifierAtom`).
- The root `aoni` package exports only canonical verbs, constructors, and fundamental contracts.

---

## 3. Dual Engine Comparison: Choosing the Right Client

| Characteristic | `aoni.Client` (Standard) | `fast.Client` (Fast Path) |
| :--- | :--- | :--- |
| **Underlying Engine** | `net/http` (`http.RoundTripper`) | `fasthttp` + native H2/H3 |
| **Ecosystem Compatibility** | 100% drop-in for standard library | Specialized high-concurrency API |
| **Protocols** | HTTP/1.1, HTTP/2 | HTTP/1.1, HTTP/2, HTTP/3 (Alt-Svc racing) |
| **Memory Model** | Standard Go GC | Pooled buffers (`sync.Pool`) |
| **Response Body** | Stream (`io.ReadCloser`) | Stream or volatile byte buffer |
| **Recommended Use Case** | Microservices, REST APIs, general HTTP | High-volume scraping, load testing, crawlers |

---

## 4. Concurrency & Memory Invariants

### Client Immutability
All `Client` instances (`aoni.Client` and `fast.Client`) are **strictly immutable** after creation:
- All public methods are safe for concurrent invocation by multiple goroutines.
- Modifying client state via `client.With(opts...)` or `client.Clone()` returns a completely new `Client` instance with deep-copied configuration maps and isolated state. The original client is never mutated.

### RequestBuilder Lifecycle
`RequestBuilder` (`client.R()`) is pooled via `sync.Pool` to avoid allocations:
- **Not thread-safe**: A `RequestBuilder` instance must be created, configured, and executed within a single goroutine.
- **Single-use guard**: Once `Execute()` or `Release()` is called, the builder marks itself as `consumed = true`. Any subsequent execution attempt immediately returns `ErrBuilderConsumed`, preventing use-after-free data corruption.

### Buffer Lifetime in `fast.Client`
- `fast.Response` reuses internal byte buffers across requests.
- Slices returned by `resp.BodyBytes()` or `resp.UnsafeBodyBytes()` are valid **only until the request lifecycle completes or `resp.Close()` is called**.
- If payload data needs to outlive the request, callers must copy it via `bytes.Clone()` or use `resp.String()`.

---

## 5. Directory Structure

```text
aoni/
├── client.go, config.go, builder.go, contract.go ...  // Layer 4: Root aoni client
├── option/                                            // Layer 4: Client configuration options
├── mod/                                               // Layer 4: Per-request modifiers
├── fast/                                              // Layer 4: High-throughput client engine
├── cookie/                                            // Layer 3: RFC 6265 cookie storage & jars
├── codec/                                             // Layer 3: Response payload decoders
├── resiliency/                                        // Layer 3: Retry, circuit breaker, caching
├── fingerprint/                                       // Layer 3: TLS/JA4/HTTP2 browser profiles
├── realtime/                                          // Layer 3: WebSockets, SSE, streams
├── tunnel/                                            // Layer 3: SOCKS5, SSH, MASQUE tunnels
├── netutil/                                           // Layer 3: DNS, proxy utilities, IP tools
├── grpc/                                              // Layer 3: gRPC-Web transport adapter
├── internal/                                          // Layers 1–2: Private implementation
│   ├── pipeline/                                      // Middleware execution engine
│   ├── core/                                          // Core atomic types (ModifierAtom)
│   ├── sys/                                           // Platform & OS thread affinity
│   ├── quic/                                          // RFC 9000 QUIC transport implementation
│   ├── qpack/                                         // RFC 9204 QPACK encoder/decoder
│   └── fast/                                          // Fast engine protocol handlers (H1, H2, H3)
├── x/                                                 // Experimental modules (Socket.IO, WebTransport)
└── cmd/                                               // CLI tools (vortex)
```

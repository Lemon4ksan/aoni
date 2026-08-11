# Aoni Codebase Style & Architecture Guide

This document defines the authoritative coding standards, architectural principles, package layout rules, memory allocation guidelines, API design contracts, documentation style, and versioning policy for the **`aoni`** repository.

All current code and future contributions **must** adhere strictly to this guide.

## Table of Contents

1. [Architectural Philosophy & Core Principles](#1-architectural-philosophy--core-principles)
2. [Initialization & Memory Allocation Style](#2-initialization--memory-allocation-style)
3. [Naming Conventions & Typography](#3-naming-conventions--typography)
4. [Package Layout & File Structuring Rules](#4-package-layout--file-structuring-rules)
5. [Documentation & Commenting Style](#5-documentation--commenting-style)
6. [Function Responsibility & Granularity](#6-function-responsibility--granularity)
7. [Public API Design & Ergonomics](#7-public-api-design--ergonomics)
8. [API Stability & Versioning Policy (v0 vs v1)](#8-api-stability--versioning-policy-v0-vs-v1)
9. [Codebase Inconsistency Audit & Alignment Checklist](#9-codebase-inconsistency-audit--alignment-checklist)

## 1. Architectural Philosophy & Core Principles

`aoni` is a unified, ultra-high-performance Internet Protocol engine for Go. Its design is driven by four fundamental tenets:

### 1.1 Progressive Disclosure of Complexity
Simple tasks must be simple; complex enterprise tasks must be possible without API friction. A single-line request (`request.GetTo[T]`) executes with zero overhead, while advanced network resilience (Happy Eyeballs v3, uTLS fingerprinting, WAF challenge solving, p0f OS spoofing, SSH/MASQUE tunneling) is selectively enabled via functional options without altering call site contracts.

### 1.2 Zero-Allocation Mindset in Hot Paths
Hot execution paths (`fast`, `codec`, `fluent`, `fingerprint`, `internal/pipeline`) must maintain an absolute minimum or zero heap allocation footprint under parallel I/O. Object pooling (`sync.Pool`), slice pre-allocation, buffer re-use, and avoiding reflection in runtime loops are mandatory.

### 1.3 High Readability & Pure Go Transparency
Performance optimizations must never degrade code legibility. Avoid cryptic micro-hacks, unchecked magic constants, or obscure side effects. Code abstractions must remain clean, self-describing, and maintainable.

### 1.4 Strict Standard Adherence & Chromium-Grade Resilience
All protocol implementations, HPACK/QPACK framing, HTTP status fallbacks (421 Misdirected Request, 408 Request Timeout, 425 Too Early), cookie path sorting (RFC 6265), and ECH (RFC 9460) must rigorously follow IETF RFC and W3C specifications while matching Chromium's network stack resilience.

### 1.5 Decade-Proof Pluggable Pipeline Architecture
Inspired by legendary systems software like 7-Zip, whose modular stream-and-codec pipeline has remained untouched and forward-compatible across decades of new compression algorithms, `aoni` strictly decouples request decoration (`mod`), execution middleware (`middleware`), transport framing (`transport`/`fingerprint`), and response unmarshaling (`codec`). Any future network protocol (HTTP/4, Post-Quantum TLS 1.4, novel binary RPC encodings, or custom obfuscation layers) must be able to mount into `aoni` as a pluggable pipeline stage without breaking a single line of existing public API contracts.

## 2. Initialization & Memory Allocation Style

### 2.1 Struct Initialization & Constructors
- **Constructors (`New...`)**: Exported types with non-zero defaults, internal goroutines, background janitors, or sync pools **must** provide a `New...` factory function (e.g., `aoni.NewClient`, `fluent.New`, `cookie.NewProxyIsolatedJar`). Notice how there's no stutter like in `cookie.NewCookieJar` or `client.NewClient`.
- **Struct Literals**: Use field-keyed struct initialization for all complex structs. Unkeyed struct literals are forbidden except for simple 1-2 field internal coordinate pairs or 0-field sentinel types (`type NoResponse struct{}`).
- **Default Zero Values**: Structs should be designed such that their zero-value is safe and usable whenever possible. If zero-values require hydration, the constructor or initialization guard must handle it lazily.

```go
// GOOD: Keyed fields, clear defaults, lazy initialization guard
client := &Client{
    engine: DefaultEngine(doer),
    defaults: ClientDefaults{
        MaxResponseSize: 10 * 1024 * 1024,
    },
}

// BAD: Unkeyed fields lead to fragile initializations and breaking changes
client := &Client{doer, nil, cfg}
```

### 2.2 Functional Options & Immutability
- Client configuration uses the Functional Options pattern (`option.With...`).
- Functional options **must** operate immutably on configuration DTOs (`*aoni.Config`).
- `Client` instances support deep copying (`c.Clone()`) and copy-on-write (`c.With(opts...)`), ensuring zero shared-state data races when instances are shared across goroutines.

### 2.3 Object Pooling (`sync.Pool`) Conventions
To guarantee zero-allocation hot paths without memory leaks:
1. **Mandatory Reset**: Every pooled object **must** implement a `Reset()` method that clears all pointers, slices (resliced to `[:0]`), and maps before returning to the pool.
2. **Capacity Capping**: Discard objects or buffers whose capacity grew excessively during usage (e.g., byte buffers exceeding 64 KB or 1 MB) to prevent pool-induced memory bloat.
3. **Paired Execution**: Always pair `Get()` with a deferred or explicit `Put()` / `Discard()`.

```go
func (p *RequestPool) Put(req *Request) {
    if req == nil {
        return
    }
    // Cap buffer size to avoid holding large allocations in memory
    if cap(req.bodyBuf) > 64*1024 {
        return
    }
    req.Reset()
    p.pool.Put(req)
}
```

### 2.4 Pre-allocating Slices & Maps
- Always provide capacity hints when initializing slices and maps in hot execution paths:
  `make([]T, 0, capacity)` or `make(map[K]V, capacity)`.
- Avoid slice re-allocations in loops. Use pre-calculated length hints whenever possible.

## 3. Naming Conventions & Typography

### 3.1 Variable & Receiver Naming
- **Receiver Names**: Standardized, short (1–2 letters), consistent within the package:
  - `c *Client` (or `fast *Client` in `fast` package)
  - `r *Request` or `req *Request`
  - `resp *Response`
  - `h Header` or `hdr Header`
  - `w http.ResponseWriter`
- **Package-Local Variables**: Concise and self-explanatory. Avoid single-letter variables except for standard index counters (`i`, `j`) or key-value pair variables (`k`, `v`).

### 3.2 Protocol Acronym Casing Rules
Go conventions require acronyms to maintain consistent casing (all uppercase or all lowercase). `aoni` strictly enforces standard casing across all exported and unexported identifiers:

| Acronym | Correct Casing | Incorrect Casing |
| :--- | :--- | :--- |
| HTTP | `HTTP`, `http`, `HTTPDoer` | `Http`, `httpDoer` |
| URL | `URL`, `url`, `BaseURL` | `Url`, `baseUrl` |
| TLS | `TLS`, `tls`, `TLSConfig`, `tlsConfig` | `Tls`, `TlsConfig` |
| DNS / DoH / DoQ | `DNS`, `DoH`, `DoQ` | `Dns`, `Doh`, `Doq` |
| QUIC / HTTP/3 | `QUIC`, `HTTP3`, `H3` | `Quic`, `Http3` |
| gRPC | `GRPC` or `gRPC` (in package names: `grpc`) | `Grpc` |
| HTTP/2 | `H2`, `H2Frame` | `H2frame` |
| JA3 / JA4 | `JA3`, `JA4`, `JA4H` | `Ja3`, `Ja4` |
| WAF | `WAF`, `WAFSolver` | `Waf` |
| ECH | `ECH`, `ECHConfig` | `Ech` |
| IP / IPv6 | `IP`, `IPv6`, `IPv6Subnet` | `Ip`, `Ipv6` |
| IPC | `IPC`, `IPCDialer` | `Ipc` |
| SSE / HAR | `SSE`, `HAR` | `Sse`, `Har` |

### 3.3 Function & Method Naming Verbs
- **Data Retrieval / Dispatch**: `Fetch`, `Do`, `Execute`, `Post`, `Get`, `Request`.
- **Generic Unmarshaling Methods**: Append `To` suffix for methods that unmarshal directly into a generic type `[T any]` (e.g., `GetTo[T]`, `FetchTo[T]`, `PostTo[T]`, `DecodeTo[T]`).
- **Functional Options**: Prefix with `With` (e.g., `option.WithProxy`, `option.WithTimeout`, `option.WithChrome`).
- **Per-Request Modifiers**: Prefix with `With` (e.g., `mod.WithHeader`, `mod.WithJSONBody`, `mod.WithContext`).
- **Builder Setters**: Prefix with `Set` (e.g., `req.SetHeader`, `req.SetContext`).

### 3.4 Interface Naming
- Single-method interfaces must end with `-er` (e.g., `HTTPDoer`, `RequestDoer`, `Unwrapper`, `Modifier`).
- Multi-method domain interfaces must use clear, descriptive noun phrases (e.g., `Requester`, `CookieStorage`).

### 3.5 Package Naming
- Packages must be single-word, lowercase, and self-describing (`option`, `mod`, `request`, `fluent`, `fast`, `grpc`, `cookie`, `codec`, `fingerprint`, `resiliency`, `realtime`, `telemetry`, `tunnel`, `netutil`, `internal`).
- Avoid generic package names like `util` or `helpers`. Sub-packages inside `netutil` or `resiliency` must reflect their specific domain (e.g., `netutil/proxy`, `netutil/dns`, `resiliency/circuit`).

## 4. Package Layout & File Structuring Rules

### 4.1 Package Separation & Architecture Layers

```text
aoni/
├── client.go, config.go, engine.go    // Core public client & engine contract
├── option/                            // Functional Client Options (option.With...)
├── mod/                               // Per-Request Modifiers (mod.With...)
├── request/                           // Generic execution helpers (request.GetTo[T])
├── fluent/                            // Chainable Request Builder (fluent.FetchTo[T])
├── fast/                              // High-throughput fasthttp + H2/H3 engine
├── grpc/                              // Native gRPC client & stream invoker
├── cookie/                            // Proxy-isolated cookie jars & storage
├── codec/                             // Response decoders & struct encoders
├── fingerprint/                       // TLS/JA4/p0f evasion & browser profiles
├── resiliency/                        // Caching, circuit breakers, WAF solvers
├── realtime/                          // WebSockets, Socket.IO, SSE, NDJSON
├── telemetry/                         // HAR generators, latency trackers, inspector
├── tunnel/                            // MASQUE & TUN adapters
├── netutil/                           // Network resolvers, rotators, ECH, probes
└── internal/                          // Internal non-exported engine primitives
```

### 4.2 File Layout Inside Packages
Every package must adhere to a standardized file layout:
1. `doc.go`: Package-level Godoc summary and architectural explanation.
2. `errors.go`: Package-specific sentinel errors (`Err...`) and custom error types.
3. `<feature>.go`: Domain implementation files.
4. `*_test.go`: Comprehensive unit tests, integration tests, and benchmarks.

### 4.3 File Size & Splitting Rules
- **Maximum File Length**: A single `.go` file should not exceed **600–800 lines**.
- When a file grows beyond 800 lines or mixes distinct sub-responsibilities, split it logically by feature (e.g., splitting `requests.go` into `requests.go`, `requests_fast.go`, and `requests_helpers.go`).

### 4.4 Import Grouping Order
Imports must be grouped in exactly 3 sections separated by empty lines, enforced by `gci`:
1. Standard library packages (`"context"`, `"net/http"`, `"time"`).
2. Third-party dependency packages (`"github.com/valyala/fasthttp"`, `"github.com/quic-go/quic-go"`).
3. Internal repository packages (`"github.com/lemon4ksan/aoni"`, `"github.com/lemon4ksan/aoni/option"`).

## 5. Documentation & Commenting Style

`aoni` follows **Go-Native Documentation Standards** enhanced with structural elegance. 

> [!IMPORTANT]
> Documentation must remain **100% compliant with standard `godoc` and `pkgsite`**. Go documentation prioritizes natural, readable prose.

### 5.1 Rules for Exported Identifiers
- **100% Coverage**: Every exported struct, interface, type, method, function, field, constant, variable, and sentinel error **must** have a doc comment.
- **First Sentence Rule**: The doc comment **must** begin with the name of the exported symbol being documented.

### 5.2 Doc Comment Structure
For complex types or methods, structure doc comments into clear narrative paragraphs, using Markdown formatting supported by `godoc`:

1. **Summary Line**: Starts with symbol name and describes primary behavior concisely.
2. **Discussion Paragraph**: Details architectural rationale, progressive disclosure, background mechanics, or edge cases.
3. **Structured Markdown Headers** (when necessary for complex operations):
   - `# Concurrency`: Thread-safety guarantees and goroutine considerations.
   - `# Performance`: Zero-alloc behavior, pool notes, or throughput tips.
   - `# Example`: Short, runnable code snippet demonstrating typical usage.

```go
// Client is an immutable, thread-safe, multi-protocol HTTP, WebSockets, and gRPC client facade.
// It acts as a high-level public interface hiding low-level protocol orchestration
// (uTLS fingerprints, HTTP/2-3 framing, proxy rotation, anti-DPI packet fragmentation, and p0f OS spoofing).
//
// Designed around Progressive Disclosure of Complexity: simple REST API calls execute
// with 0-alloc fast-path performance, while advanced enterprise features are available via options
// without breaking application code contracts or requiring service rewrites.
//
// # Concurrency
// Client instances are 100% thread-safe and safe for concurrent invocation across goroutines.
// Methods such as With() and Clone() return new Client instances with isolated configuration DTOs.
type Client struct {
    ...
}
```

```go
// FetchTo executes a request with method, path, and optional modifiers, unmarshaling the response into T.
//
// # Performance
// Utilizes pooled request builders to eliminate heap allocations during request construction.
func FetchTo[T any](
    ctx context.Context,
    c *aoni.Client,
    method, path string,
    mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
    ...
}
```

## 6. Function Responsibility & Granularity

### 6.1 Single Responsibility Principle (SRP)
- A function should perform exactly one logical operation.
- Keep function length manageable (target **< 60–80 lines** per function).
- If an operation requires multiple sequential execution stages (e.g., TLS setup -> HTTP/2 frame construction -> header compression), split each stage into a dedicated, unexported helper function.

### 6.2 Segregation of Hot Paths vs Cold Paths
- **Hot Paths (`fast/`, `pipeline/`, `codec/`, `fluent/`)**: Must maintain zero allocations. Never allocate temporary closures, use `fmt.Sprintf`, perform runtime reflection, or create un-buffered slices in hot execution routines.
- **Cold Paths (`option/`, `NewClient`, `Configure`)**: Configuration and setup functions may perform allocations, slice copying, and parsing to validate options and pre-compute execution configurations.

### 6.3 Resource Cleanup & Error Handling
- **Defensive Closing**: Always ensure resource streams and response bodies are closed explicitly (`defer resp.Body.Close()`) when reading responses.
- **Error Wrapping**: Wrap lower-level errors using `%w` to preserve error chains (`fmt.Errorf("aoni: failed to execute request: %w", err)`).
- **Context Awareness**: All network operations must respect `context.Context` cancellation and deadline signals.

### 6.4 Error Message Prefix Formatting Standard
All sentinel errors (`errors.New`) and formatted error strings (`fmt.Errorf`) MUST follow the unified package namespace prefix format:
- Root package (`aoni`): `"aoni: <description>"`
- Subpackages (`aoni/<submodule>`): `"aoni/<submodule>: <description>"` (e.g., `"aoni/mod: ..."`, `"aoni/fluent: ..."`, `"aoni/fast: ..."`, `"aoni/values: ..."`, `"aoni/cookie: ..."`, `"aoni/transport: ..."`, `"aoni/grpc: ..."`).
- Never use spaces (`"aoni mod:"` ❌), colons with space (`"aoni transport:"` ❌), or hyphens (`"aoni-codegen:"` ❌) in error prefixes.

## 7. Public API Design & Ergonomics

### 7.1 Dual-Engine Architecture
`aoni` provides two specialized client engines sharing a unified design philosophy:
1. **`aoni.Client`**: 100% `net/http` drop-in compatible via bridge, full middleware chain support, seamless standard library integration.
2. **`fast.Client` (`aoni/fast`)**: Native high-throughput engine built on `fasthttp` + native H2/H3 for maximum RPS and absolute zero allocations under parallel I/O.

### 7.2 Generics-First Unmarshaling Ergonomics
`aoni` uses Go generics (`[T any]`) to eliminate boilerplate unmarshaling:
- `request.GetTo[T](ctx, client, url)`
- `fluent.FetchTo[T](ctx, client, method, path)`
- `codec.DecodeTo[T](resp, target)`

### 7.3 Immutability & Thread Safety
Public API objects (such as `aoni.Client` and `aoni.Config`) must be safe for concurrent use across multiple goroutines. Any call that mutates configuration must return a newly cloned instance (`c.With(...)`, `c.Clone()`).

## 8. API Stability & Versioning Policy (v0 vs Perpetual v1)

`aoni` enforces a two-stage API lifecycle model inspired directly by Go's compatibility promise:

### 8.1 Current Stage: `v0.x` (Active Evolution & Architectural Refactoring)
- **Status**: Experimental & Rapid Refactoring phase.
- **Breaking Changes**: Breaking changes to exported APIs, signature refinements, and package reorganizations **are permitted** during this phase to eliminate legacy technical debt, guarantee zero-alloc hot paths, and align the entire codebase with this Style Guide.
- **Goal**: Rapidly achieve total consistency, peak throughput, and 100% RFC compliance before freezing the public interface.

### 8.2 Future Stage: `v1.0.0+` (Perpetual v1 Compatibility Promise)
- **Status**: Perpetual Frozen Production API.
- **Go-Style Perpetual `v1` Guarantee**: Once `v1.0.0` is released, the public API is **permanently frozen and guaranteed forever** (modeled after the Go 1 compatibility promise). There will be no breaking `v2` redesigns or major API overhauls that break user code.
- **Additive Evolution**: All future features, performance optimizations, and protocol additions must be 100% additive, non-breaking, and backwards-compatible with existing `v1` call sites.
- **Deprecation Policy**: If a symbol is superseded by a superior abstraction, it will be marked with a Go-native `// Deprecated: ...` doc comment and retained indefinitely to preserve total backward compatibility.

## 9. Codebase Inconsistency Audit & Alignment Checklist

To bring the existing codebase into 100% compliance with this Style Guide, the following audit checklist will be executed across all packages:

- [ ] **Acronym Standardization**: Audit and fix non-conforming acronym casing (e.g. rename `Url` -> `URL`, `Http` -> `HTTP`, `Ja4` -> `JA4`, `Waf` -> `WAF`).
- [ ] **Doc Comments Audit**: Ensure 100% of exported types, functions, methods, fields, and sentinel errors have Godoc comments starting with the symbol name.
- [ ] **Slice Pre-allocation**: Replace un-capacitated `make([]T, 0)` calls in hot paths with `make([]T, 0, capacity)`.
- [ ] **`sync.Pool` Reset Check**: Verify all pooled objects implement and call `Reset()` before returning to the pool.
- [ ] **File Size Audit**: Check files exceeding 600–800 lines (`client.go`, `config.go`, `option.go`, `requests.go`) and split them into modular feature files where applicable.
- [ ] **Linter Enforcement**: Ensure `make format` and `make lint` execute cleanly with zero linter warnings.

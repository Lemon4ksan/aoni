# Vortex — Zero-Allocation AST Toolchain & Sovereign Contract Engine

`vortex` is the unified declarative contract toolchain, code generator, and traffic-driven reverse engineering suite for `aoni`. It operates directly on Go Abstract Syntax Trees (AST), treating idiomatic Go interface declarations as the single source of truth for REST, WebSocket, SSE, OpenAPI 3.1, AsyncAPI 2.x/3.x, and Protocol Buffer communications.

---

## 📖 The Vortex Manifesto: The Era of Sovereign API Clients

> *"A little copying is better than a little dependency."* — Go Proverb

For decades, developers have built and maintained third-party API wrapper SDKs (in Python, Node.js, and Go), believing that writing a client library is the pinnacle of API integration. In reality, **nobody should ever write or depend on third-party API client wrappers as external libraries.**

### Why Traditional API SDKs Fail:
1. **Supply Chain Bloat & Version Lock-in**: Importing an external SDK brings hundreds of transitive dependencies, reflection overhead, and breaking upgrade cycles.
2. **Missing & Outdated Endpoints**: When an upstream service releases a new feature or model (like Gemini 3.7 Thinking or Function Calling), developers are blocked waiting for third-party maintainers to update their SDK.
3. **Excessive Heap Allocations**: Traditional SDKs generate intermediate `map[string]any`, heavy reflection structs, and unbuffered string concatenations on every request.
4. **Guesswork in Undocumented APIs**: Reverse-engineering private/internal APIs by manual guesswork leads to fragile, unmaintainable code.

### The Sovereign Model with Vortex:
**Every project must own its sovereign, zero-allocation API client generated directly into `pkg/` from AST, OpenAPI specifications, or captured network traffic.** 

With Vortex:
* **Sniffing beats guessing**: Capture real network traffic (`.har`) with `vortex traffic record` or `vortex traffic store`.
* **Zero-allocation code generation**: Compiles declarative Go interfaces into pure, machine-optimized Go (`*.gen.go`) without reflection or heap allocations.
* **100% Browser Fidelity**: Integrates L3 (TCP SYN spoofing), L4 (uTLS Chrome 120+), and L7 (HTTP/2 SETTINGS & High-Entropy Client Hints) to match real browser behavior byte-for-byte.

---

## Table of Contents

1. [Architecture & The 4 Pillars of Vortex](#1-architecture--the-4-pillars-of-vortex)
   - [Pillar 1: Declarative Go-First AST Contracts](#pillar-1-declarative-go-first-ast-contracts)
   - [Pillar 2: Traffic-Driven Reverse Engineering & Persistent Cache](#pillar-2-traffic-driven-reverse-engineering--persistent-cache)
   - [Pillar 3: Positional Tuples & JSPB Deobfuscation (`@aoni:tuple`)](#pillar-3-positional-tuples--jspb-deobfuscation-aonituple)
   - [Pillar 4: L3/L4/L7 Network & Browser Fidelity](#pillar-4-l3l4l7-network--browser-fidelity)
2. [Declarative Contract Syntax Reference](#2-declarative-contract-syntax-reference)
   - [Service Interface Annotations](#service-interface-annotations)
   - [Method Route & Protocol Annotations](#method-route--protocol-annotations)
   - [Parameter Binding Annotations](#parameter-binding-annotations)
   - [Resilience, Timeouts & Caching Annotations](#resilience-timeouts--caching-annotations)
   - [Data Transfer Object (DTO) Annotations](#data-transfer-object-dto-annotations)
   - [Heterogeneous Tuple & JSPB Annotations (`@aoni:tuple`)](#heterogeneous-tuple--jspb-annotations-aonituple-1)
   - [Two-Tier Header Architecture](#two-tier-header-architecture)
   - [Shadow Root Source Mirroring (`@aoni:mirror`)](#shadow-root-source-mirroring-aonimirror)
3. [Complete CLI Reference](#3-complete-cli-reference)
   - [Daily Core Commands (`vortex autopilot`, `vortex gen`, `vortex check`, `vortex mock`, `vortex env`, `vortex smoke`)](#daily-core-commands)
   - [Traffic Hub (`vortex traffic`, `vortex inspect`, `vortex diff`)](#traffic-hub)
   - [Specification Hub (`vortex spec`, `vortex import`, `vortex export`, `vortex proto`)](#specification-hub)
   - [AST Refactoring & VCS Hub (`vortex ast`)](#ast-refactoring--vcs-hub)
   - [Performance & Profiling Hub (`vortex perf`)](#performance--profiling-hub)
   - [Workspace Management (`vortex init`, `vortex config`, `vortex status`, `vortex clean`)](#workspace-management)
4. [Configuration Schema (`.vortex.yml`)](#4-configuration-schema-vortexyml)
5. [Real-World Case Studies](#5-real-world-case-studies)
   - [Case Study 1: Google AI Studio / Gemini 3.7 Reverse Engineering](#case-study-1-google-ai-studio--gemini-37-reverse-engineering)
   - [Case Study 2: Telegram Bot & MTProto API](#case-study-2-telegram-bot--mtproto-api)
6. [CI/CD Integration & SARIF Reporting](#6-cicd-integration--sarif-reporting)

---

## 1. Architecture & The 4 Pillars of Vortex

```text
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                   SOURCES OF TRUTH                                     │
│  Declarative Go Interface  •  Traffic Captures (.har)  •  OpenAPI 3.1  •  AsyncAPI 2/3  │
└────────────────────────────────────────────────────────────────────────────────────────┘
                                            │
                                            ▼
                              ┌───────────────────────────┐
                              │ Go AST Parser & Type Link │
                              └───────────────────────────┘
                                            │
                                            ▼
                              ┌───────────────────────────┐
                              │ Vortex Intermediate (IR)  │
                              └───────────────────────────┘
                                            │
               ┌────────────────────────────┼────────────────────────────┐
               ▼                            ▼                            ▼
     ┌───────────────────┐        ┌───────────────────┐        ┌───────────────────┐
     │ Zero-Alloc Client │        │  In-Memory Mock   │        │ Benchmark Harness │
     │   (*.gen.go)      │        │  (*_mock.gen.go)  │        │ (*_harness.gen.go)│
     └───────────────────┘        └───────────────────┘        └───────────────────┘
```

### Pillar 1: Declarative Go-First AST Contracts
* **Go interfaces as the contract**: You write clean, standard Go interfaces. Godoc annotations (`// @post`, `// @query`, `// @header`) define protocol semantics.
* **Dual-Engine Dispatch**: Generated clients automatically support both standard `net/http` (`aoni.Client`) and ultra-high-performance `fasthttp` (`fast.Client`) without breaking signatures.
* **Zero Allocations on Hot Paths**: Codecs avoid reflection by compiling optimized serializer/deserializer routines directly into Go bytecode.

### Pillar 2: Traffic-Driven Reverse Engineering & Persistent Cache
* **Persistent Session Cache (`.vortex/cache/traffic`)**: Raw `.har` archives are automatically compressed with gzip (compressing 20MB dumps down to 1MB), SHA256-deduplicated, and stored locally.
* **Credential Vaulting**: Sensitive tokens (`Bearer`, `SAPISID`, API keys) are stripped from captured traffic and isolated in `.vortex/cache/secrets.json`.
* **Interactive Terminal Inspector (`vortex inspect <target>`)**: Inspect captured requests and responses in rich TUI tables, or dump deep JSON payloads with `vortex inspect <target> --entry=N`.
* **Additive Drift Detection (`vortex diff --add`)**: Compares incoming traffic against existing Go contracts, highlighting newly discovered endpoints while suppressing ghost endpoints.
* **3-Way AST Merge (`vortex import -add`)**: Automatically reconciles existing Go code with new traffic schemas without overwriting custom edits.

### Pillar 3: Positional Tuples & JSPB Deobfuscation (`@aoni:tuple`)
* **Google RPC / Discord / Steam Protocol Support**: Reverse engineers array-based protocols where fields are indexed by position (`[0, "hello", null, [1, 2]]`).
* **Nested Path Mapping**: Map deep hierarchical indices like `aoni:"14.0.1"` directly to struct fields.
* **Bounds-Safe & Sparse Protection**: Automatically skips `null` elements or truncated arrays without panicking.

### Pillar 4: L3/L4/L7 Network & Browser Fidelity
* **L3 (p0f OS Emulation)**: Emulates real Windows/macOS/Linux TCP/IP SYN packets, Window Scale, MSS, and TTL.
* **L4 (uTLS Browser Profiles)**: Matches Chrome 120+ / Firefox ClientHello, ALPN, cipher suites, ECH, 0-RTT, and Brotli/Zstd certificate compression.
* **L7 (HTTP/2 SETTINGS & Client Hints)**: Emulates exact HTTP/2 SETTINGS frames, pseudo-header ordering (`:method`, `:authority`, `:scheme`, `:path`), High-Entropy Client Hints (`sec-ch-ua`), natural header casing, and background activity heartbeats (`waa-pa`).

---

## 2. Declarative Contract Syntax Reference

Vortex parses standard Go interface declarations decorated with structured Godoc comments.

### Service Interface Annotations

Service annotations are placed directly above the interface definition.

```go
// @aoni:service
// @version "v1.4.0"
// @source "https://api.example.com/openapi.json"
// @casing "snake_case"
// @engine "fast"
// @header "User-Agent" "my-app/1.0.0"
type UserAPI interface {
    // Methods...
}
```

| Tag | Parameter | Description |
| :--- | :--- | :--- |
| `// @aoni:service` | — | Marks the Go interface as a manageable network contract. |
| `// @version "<semver>"` | SemVer string | Declares the API version for changelog tracking and semantic diffing. |
| `// @source "<path/url>"` | File path or URL | Links the contract to an upstream OpenAPI or AsyncAPI schema. |
| `// @casing "<style>"` | `snake_case`, `camelCase`, `kebab-case` | Sets default query/header/form parameter serialization casing. |
| `// @engine "<engine>"` | `fast`, `standard` | Specifies default client runtime engine. |
| `// @header "<key>" "<val>"` | Key & Value | Defines global service-wide inherited headers. |

---

### Method Route & Protocol Annotations

Method annotations specify HTTP verbs, URL routes, and realtime streaming protocols.

```go
// @get "users/{id}"
GetUser(ctx context.Context, id uint64, mods ...aoni.RequestModifier) (*UserDTO, error)

// @post "users"
CreateUser(ctx context.Context, req *CreateUserRequest, mods ...aoni.RequestModifier) (*UserDTO, error)

// @ws "realtime/feed"
ConnectFeed(ctx context.Context, mods ...aoni.RequestModifier) (aoni.WebSocketConn, error)

// @event "userCreated"
OnUserCreated(ctx context.Context, handler func(msg *UserDTO)) (aoni.Subscription, error)
```

| Tag | Syntax | Description |
| :--- | :--- | :--- |
| `// @get "<route>"` | Route path | HTTP GET request. Route parameters are enclosed in `{name}`. |
| `// @post "<route>"` | Route path | HTTP POST request. Request body is bound from struct parameter. |
| `// @put "<route>"` | Route path | HTTP PUT request. |
| `// @delete "<route>"` | Route path | HTTP DELETE request. |
| `// @patch "<route>"` | Route path | HTTP PATCH request. |
| `// @head "<route>"` | Route path | HTTP HEAD request. |
| `// @options "<route>"` | Route path | HTTP OPTIONS request. |
| `// @ws "<route>"` | Route path | WebSocket endpoint (Extended CONNECT / RFC 8441). |
| `// @sse "<route>"` | Route path | Server-Sent Events (SSE) streaming endpoint. |
| `// @event "<name>"` | Event name | Inbound AsyncAPI / SSE message handler. |
| `// @ws:emit "<name>"` | Event name | Outbound AsyncAPI WebSocket message sender. |

---

### Parameter Binding Annotations

Vortex automatically maps method parameters based on name and Go types:

```go
type OrderAPI interface {
    // @get "orders/{order_id}"
    // @query status "status"
    // @header authToken "Authorization"
    GetOrder(
        ctx context.Context,
        orderID string,
        status string,
        authToken string,
        mods ...aoni.RequestModifier,
    ) (*OrderDTO, error)
}
```

* **Path Parameters**: Parameters matching `{name}` in the route string are formatted via zero-allocation string converters (`strconv.FormatUint`, `strconv.Itoa`).
* **Query Parameters**: Scalar arguments not present in the route path default to URL query parameters.
* **Body Parameters**: Pointers to structs or slices default to JSON/Protobuf request bodies.
* **Request Modifiers**: Variadic `mods ...aoni.RequestModifier` allows runtime per-request overrides (custom headers, timeouts, proxy rotation).

---

### Resilience, Timeouts & Caching Annotations

```go
type MarketAPI interface {
    // @get "items/price"
    // @retry 3
    // @cache 30s
    // @timeout 5s
    // @since "v1.1.0"
    GetPrice(ctx context.Context, appID uint32, marketHashName string) (*PriceDTO, error)
}
```

| Tag | Argument | Description |
| :--- | :--- | :--- |
| `// @retry <N>` | Integer count | Automatically retries idempotent failures up to N times. |
| `// @cache <duration>` | Time duration | Caches successful 200 OK responses in the in-memory LRU cache. |
| `// @timeout <duration>` | Time duration | Enforces hard context deadline per request. |
| `// @since "<ver>"` | SemVer string | Tags the version when this endpoint was introduced. |
| `// @deprecated` | Optional reason | Marks method as deprecated in generated Go code and OpenAPI schema. |

---

### Data Transfer Object (DTO) Annotations

```go
// @aoni:dto casing=snake_case omitempty=true
type CreateClientRegisterRequest struct {
    AppName      string    `json:"appName,omitempty"`
    ConnectionID string    `json:"connectionId,omitempty"`
    InstanceID   string    `json:"instanceId,omitempty"`
    Interval     int64     `json:"interval,omitempty"`
    Started      time.Time `json:"started,omitempty"`
    Strategies   []string  `json:"strategies,omitempty"`
}
```

---

### Heterogeneous Tuple & JSPB Annotations (`@aoni:tuple`)

For RPC frameworks that return positional arrays or hierarchical JSPB trees:

```go
// @aoni:tuple
type ContentPartTuple struct {
    InlineData          *BlobTuple                `aoni:"0"`
    Text                string                    `aoni:"1"`
    FunctionCall        *FunctionCallTuple        `aoni:"2"`
    FunctionResponse    *FunctionRespTuple        `aoni:"3"`
    FileData            *FileDataTuple            `aoni:"4"`
    DriveFile           *DriveFileRefTuple        `aoni:"5"`
    ExecutableCode      *ExecutableCodeTuple      `aoni:"7"`
    CodeExecutionResult *CodeExecutionResultTuple `aoni:"8"`
    ThoughtSignature    string                    `aoni:"14"`
}
```

* **Zero-Allocation Decoding**: Emits high-speed `UnmarshalJSON` that performs zero heap allocations and directly extracts positional fields.
* **Bounds-Safe & Sparse Protection**: Automatically skips `null` elements or truncated arrays without panics.
* **Scalar & Array Compatibility**: Seamlessly handles both arrays of objects/arrays and single scalar responses.

---

### Shadow Root Source Mirroring (`@aoni:mirror`)

When building high-speed `aoni` wrappers over tightly-coupled or private legacy Go backends (which cannot be annotated with `@aoni:service` or regenerated), use `@aoni:mirror` to treat the legacy Go code as an **immutable, read-only Root of Truth**:

```go
// @aoni:service
// @aoni:mirror "internal/legacy/steam/inventory.go:LegacyInventoryService"
type InventoryWrapperAPI interface {
    // @get "inventory"
    GetInventory(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) ([]*Item, error)
}
```

---

## 3. Complete CLI Reference

Install or update the `vortex` CLI:

```bash
go install github.com/lemon4ksan/aoni/cmd/vortex@latest
```

---

### Daily Core Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| **`vortex autopilot`** | `vortex` or `vortex autopilot -watch` | Zero-configuration health audit, schema sync, and compilation in <50ms. |
| **`vortex gen`** | `vortex gen [path.go]` | Compiles declarative Go interfaces into zero-allocation API clients. |
| **`vortex check`** | `vortex check [--breaking-only] [--fix]` | Static contract validation, rule enforcement, and pre-commit checks. |
| **`vortex mock`** | `vortex mock [-fixtures]` | Generates in-memory HTTP/WebSocket mock servers for integration testing. |
| **`vortex env`** | `vortex env [--fill --out=.env.local]` | Scans contracts for `${VAR}` references and generates environment templates. |
| **`vortex smoke`** | `vortex smoke [--all --timeout=10s]` | Rapidly probes live contract endpoints and renders latency/TLS tables. |

---

### Traffic Hub

Manages local traffic captures, live process sniffing, interactive HTTP/JSON inspection, and the encrypted secrets vault.

```bash
# 1. Capture live process network traffic into a HAR archive:
vortex traffic record -out=session.har -- ./mycli

# 2. Inspect captured traffic (Table view of HTTP requests with previews):
vortex traffic inspect session.har
vortex inspect attach_local_image             # Short alias for cached sessions

# 3. Deep payload dumper (Pretty-printed JSON / tuple request & response bodies):
vortex traffic inspect attach_local_image --entry=2
vortex traffic inspect session.har --filter=GenerateContent

# 4. Ingest and manage local traffic cache (.vortex/cache/traffic):
vortex traffic list                           # Dynamic TUI table of cached sessions
vortex traffic store session.har              # Archive session into cache (keeps original)
vortex traffic move session.har               # Ingest session into cache and delete original
vortex traffic export <id> -out=clean.har     # Export clean, uncompressed HAR with gunzip
vortex traffic secrets                        # Dynamic TUI table of captured credentials vault
vortex traffic sanitize dirty.har -out=clean.har # Scrub tokens for Git commit safety
vortex traffic prune                          # Clean old traffic snapshots
```

---

### Specification Hub

Bidirectional schema toolchain for importing, exporting, and diffing OpenAPI and HAR specifications.

```bash
# 1. Ingest OpenAPI / HAR traffic captures into declarative Go contracts (3-Way AST Merge):
vortex spec import -spec=openapi.json -out=./pkg/api/api.go
vortex spec import -spec=session.har -out=./pkg/api/api.go -add
vortex spec import traffic1.har,traffic2.har -mode=intersect

# 2. Export @aoni:service contracts to OpenAPI 3.1:
vortex spec export -file=./pkg/api/api.go -out=openapi.json
vortex spec export -file=./pkg/api/api.go -yaml -out=openapi.yaml

# 3. Additive drift detection against captured HAR / OpenAPI:
vortex diff --add ./traffic.har ./pkg/api/api.go
vortex diff --against=main ./pkg/api

# 4. Compile Protocol Buffer definitions:
vortex spec proto -src=./proto -out=./pkg/pb
```

---

### AST Refactoring & VCS Hub

```bash
# Deobfuscate positional arrays / JSPB into typed @aoni:tuple structs:
vortex ast tuple pkg/api/client.go

# Split monolithic interface into separate focused interfaces (ISP principle):
vortex ast split --from=MarketAPI --methods="Get*,List*" --to=MarketReaderAPI

# Batch rename method names via regular expressions:
vortex ast rename --match="Fetch(.*)" --replace="Get$1" pkg/services/items/api.go

# Audit and merge consumer Git proposal branches:
vortex ast review openapi.json pkg/user/api.go
vortex ast accept openapi.json pkg/user/api.go

# Contract provenance, history journal, and rollback:
vortex ast history                   # Inspect past mutation journal
vortex ast undo                      # Revert the latest modifying AST operation
vortex ast blame pkg/user/api.go     # Line-by-line contract provenance
vortex ast log pkg/user/api.go       # Contract revision timeline
vortex ast tag add v1.2.0            # Tag contract release snapshot
```

---

### Performance & Profiling Hub

```bash
# Executive performance dashboard and pprof inspector:
vortex perf                          # Profile all workspace contracts
vortex perf prof --bench-time=100ms  # Custom benchmark duration

# Silicon hardware inspection and engine benchmarks:
vortex perf bench                    # Hardware CPU flags, AVX2/AVX-512, RPS scoring
vortex perf cover                    # Deduplicated core test coverage analyzer
vortex perf harness pkg/user/api.go  # Generate zero-allocation benchmark harness
vortex perf pgo -record=60s          # Collect runtime profile for default.pgo
```

---

### Workspace Management

```bash
vortex init                          # Auto-discover existing Go interfaces
vortex init billing -tpl=rest        # REST CRUD API template
vortex init chat -tpl=ws            # WebSocket Bi-Directional Event Client
vortex init ai -tpl=sse             # Real-Time SSE & NDJSON Client

vortex config list
vortex config get defaults.engine
vortex config set defaults.engine fast
vortex config lint disable S001 W002

vortex status -strict                # 360° workspace health check
vortex clean                         # Remove generated files and stale build cache
```

---

## 4. Configuration Schema (`.vortex.yml`)

The `.vortex.yml` file in your repository root defines workspace configuration and contract bindings:

```yaml
version: "1"

contracts:
  - name: makersuite
    file: pkg/agy/makersuite.go
    service: MakerSuiteAPI
    package: agy
    source: .vortex/cache/traffic/step5_function_calling.har.gz
    engine: fast

  - name: unleash
    file: pkg/agy/unleash.go
    service: UnleashAPI
    package: agy
    engine: fast

  - name: telegram
    file: pkg/telegram/telegram.go
    service: TelegramAPI
    package: telegram
    engine: fast

rules:
  S001: error   # Prohibit untyped any payloads in public interfaces
  W001: warn    # Require @version annotations on public services
  W002: ignore  # Suppress strict query parameter naming rules

formatting:
  casing: snake_case
  omitempty: true
```

---

## 5. Real-World Case Studies

### Case Study 1: Google AI Studio / Gemini 3.7 Reverse Engineering

Using Vortex's traffic hub, we completely reversed Google AI Studio's private `MakerSuiteService` protocol from 11 captured sessions into a 92KB zero-allocation sovereign client (`pkg/agy`):

1. **Traffic Capture & Caching**:
   ```bash
   vortex traffic store step1_sampling.har
   vortex traffic store step2_thinking.har
   vortex traffic store step3_tools.har
   vortex traffic store attach_drive_file.har
   vortex traffic store attach_local_image.har
   ```
2. **Terminal Inspection**:
   ```bash
   vortex inspect step3_tools --entry=0 # Reveals ExecutableCodeTuple & CodeExecutionResult
   vortex inspect attach_local_image    # Reveals CheckImage and background Drive backup
   ```
3. **Synthesis & AST Merge**:
   ```bash
   vortex spec import -spec=.vortex/cache/traffic/step3_tools.har.gz -out=pkg/agy/makersuite.go -add
   vortex build # Compiles makersuite.gen.go with zero allocations
   ```
4. **100% Chrome Browser Fidelity**:
   ```go
   sess, err := agy.NewSession(agy.SessionConfig{
       Cookies:         "SAPISID=...; HSID=...",
       EnableHeartbeat: true, // Waa-pa activity telemetry
   })
   resp, err := sess.API.GenerateContent(ctx, &agy.GenerateContentRequest{
       Model: "models/gemini-3.7-flash",
       Contents: []agy.ContentTuple{...},
   })
   ```

---

## 6. CI/CD Integration & SARIF Reporting

Integrate Vortex validation into GitHub Actions workflow (`.github/workflows/contracts.yml`):

```yaml
name: API Contracts & Security Audit

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.26.x'

      - name: Install Vortex
        run: go install github.com/lemon4ksan/aoni/cmd/vortex@latest

      - name: Verify Contract Health
        run: vortex status -strict

      - name: Verify Clean Working Tree
        run: |
          vortex gen
          git diff --exit-code

      - name: Run Static Security Analysis
        run: vortex check -sarif=results.sarif

      - name: Upload SARIF to GitHub Security
        uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: results.sarif
```

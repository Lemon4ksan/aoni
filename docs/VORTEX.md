# Vortex — Zero-Allocation AST Toolchain & Contract Engine

`vortex` is the unified declarative contract toolchain and code generator for `aoni`. It operates directly on Go Abstract Syntax Trees (AST), treating idiomatic Go interface declarations as the single source of truth for REST, WebSocket, SSE, OpenAPI 3.1, AsyncAPI 2.x/3.x, and Protocol Buffer communications.

---

## Table of Contents

1. [Architecture & Design Principles](#1-architecture--design-principles)
2. [Declarative Contract Syntax](#2-declarative-contract-syntax)
   - [Service Interface Annotations](#service-interface-annotations)
   - [Method Route & Protocol Annotations](#method-route--protocol-annotations)
   - [Parameter Binding Annotations](#parameter-binding-annotations)
   - [Resilience & Caching Annotations](#resilience--caching-annotations)
   - [Data Transfer Object (DTO) Annotations](#data-transfer-object-dto-annotations)
   - [Heterogeneous Tuple & JSPB Annotations (`@aoni:tuple`)](#heterogeneous-tuple--jspb-annotations-aonituple)
   - [Two-Tier Header Architecture](#two-tier-header-architecture)
3. [CLI Reference & Subcommands](#3-cli-reference--subcommands)
   - [Daily Core Commands (`vortex autopilot`, `vortex gen`, `vortex check`, `vortex mock`, `vortex env`, `vortex smoke`)](#daily-core-commands)
   - [Toolchain Hubs (`vortex spec`, `vortex traffic`, `vortex ast`, `vortex perf`)](#toolchain-hubs)
   - [Workspace Management (`vortex init`, `vortex config`, `vortex status`, `vortex clean`)](#workspace-management)
4. [Configuration Schema (`.vortex.yml`)](#4-configuration-schema-vortexyml)
5. [End-to-End Workflows](#5-end-to-end-workflows)
   - [Authoring a Service Contract](#authoring-a-service-contract)
   - [In-Memory Mock Testing](#in-memory-mock-testing)
   - [OpenAPI 3.1 Roundtrip Synchronization](#openapi-31-roundtrip-synchronization)
   - [AsyncAPI Event Streaming](#asyncapi-event-streaming)
   - [Protocol Buffers & vtprotobuf](#protocol-buffers--vtprotobuf)
6. [CI/CD Integration & SARIF Reporting](#6-cicd-integration--sarif-reporting)

---

## 1. Architecture & Design Principles

Traditional code generators frequently suffer from three structural defects:
1. **Excessive Heap Allocations**: Generating intermediate maps, reflection wrappers, and unbuffered string concatenations on every request.
2. **Type Drift & Schema Fragmentation**: Manually synchronizing YAML/JSON specs with Go structs leads to divergence over time.
3. **Heavy External Dependencies**: Reliance on heavy runtime reflection libraries and unversioned third-party parsers.

### The Vortex Pipeline

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                             SOURCE OF TRUTH                              │
│   Declarative Go Interface  •  OpenAPI 3.1 Schema  •  AsyncAPI 2.x/3.x   │
└──────────────────────────────────────────────────────────────────────────┘
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

* **Pure Standard Library**: All AST parsing, schema emission, and mock servers rely exclusively on the Go standard library (`go/ast`, `go/parser`, `go/token`).
* **Zero Allocations on Hot Paths**: Generated client methods reuse pre-allocated buffer pools (`sync.Pool`), intern static headers in `.rodata`, and perform zero heap allocations during parameter serialization.
* **Dual-Engine Dispatch**: Generated clients automatically support both standard `net/http` (`aoni.Client`) and ultra-high-performance `fasthttp` (`fast.Client`) engines through a single interface.

---

## 2. Declarative Contract Syntax

Vortex parses standard Go interface declarations decorated with structured Godoc comments.

### Service Interface Annotations

Service annotations are placed directly above the interface definition.

```go
// @aoni:service
// @version "v1.4.0"
// @source "https://api.example.com/openapi.json"
// @casing "snake_case"
// @engine "fast"
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

Vortex automatically maps method parameters based on name and Go types. When fine-grained control is required, parameter-level tags can be applied.

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

### Resilience & Caching Annotations

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

Vortex allows annotating struct models for zero-allocation JSON, Form, and Proto serialization:

```go
// @aoni:dto casing=snake_case omitempty=true
type CreateClientRegisterRequest struct {
    AppName          string    `json:"appName,omitempty"`
    ConnectionID     string    `json:"connectionId,omitempty"`
    InstanceID       string    `json:"instanceId,omitempty"`
    Interval         int64     `json:"interval,omitempty"`
    Started          time.Time `json:"started,omitempty"`
    Strategies       []string  `json:"strategies,omitempty"`
}
```

| Tag | Parameter | Description |
| :--- | :--- | :--- |
| `// @aoni:dto` | `casing=<style>`, `omitempty=true` | Marks struct as a managed DTO with global JSON key casing and omitempty policy. |
| `time.Time` | RFC3339 | Automatically inferred and formatted from ISO8601/RFC3339 timestamp payloads in HAR/OpenAPI. |

---

### Heterogeneous Tuple & JSPB Annotations (`@aoni:tuple`)

For RPC frameworks that return positional arrays or hierarchical JSPB trees (e.g. Google Internal RPC, Steam, Discord), Vortex provides `@aoni:tuple`:

```go
// @aoni:tuple
type ListModelsTuple struct {
    ID           string       `aoni:"0"`
    Name         string       `aoni:"1"`
    Description  string       `aoni:"2"`
    Capabilities [][][]string `aoni:"13"`
    TraceContext string       `aoni:"42.0.1"`
}
```

| Tag | Parameter | Description |
| :--- | :--- | :--- |
| `// @aoni:tuple` | — | Marks struct as a positional/heterogeneous array tuple. |
| `` `aoni:"<path>"` `` | Index path (e.g. `"0"`, `"13.0.1"`) | Maps the struct field to a specific array index or nested slice path. |

* **Zero-Allocation Decoding**: Emits high-speed `UnmarshalJSON` that performs zero heap allocations and directly extracts positional fields.
* **Bounds-Safe & Sparse Protection**: Automatically skips `null` elements or truncated arrays without panics.
* **Scalar & Array Compatibility**: Seamlessly handles both arrays of objects/arrays and single scalar responses.

---

### Two-Tier Header Architecture

Vortex provides declarative header inheritance across two layers:
1. **Global Service Headers**: Inherited across all client methods (e.g. shared `User-Agent` or default `Authorization`).
2. **Per-Method Specific Headers**: Bound directly to an individual RPC endpoint (e.g. specific `Unleash-*` tokens or custom telemetry).

```go
// @aoni:service
// @header "User-Agent" "my-client/1.0.0"
type GatewayAPI interface {
    // @post "api/client/register"
    // @header "X-App-Name" "payment-worker"
    // @header "X-Poll-Interval" "60000"
    CreateClientRegister(ctx context.Context, req CreateClientRegisterRequest, mods ...aoni.RequestModifier) error
}
```

---

## 3. CLI Reference & Subcommands

Install or update the `vortex` CLI:

```bash
go install github.com/lemon4ksan/aoni/cmd/vortex@latest
```

```text
Usage: vortex <command> [flags] [arguments]
```

---

### Daily Core Commands

#### `vortex autopilot`
The zero-configuration primary command: audits workspace health, synchronizes upstream schemas, and compiles all clients in under 50ms.

```bash
vortex                  # Runs autopilot by default
vortex autopilot -watch # Continuous watch and auto-compile mode
```

#### `vortex gen`
Compiles declarative Go interfaces into zero-allocation API client implementations.

```bash
vortex gen                     # Compile all contracts in .vortex.yml
vortex gen pkg/user/api.go     # Compile a single contract file
vortex gen -watch              # Continuous compilation on file save
vortex gen -dry-run            # Preview generated Go source code in stdout
```

#### `vortex check`
Performs static contract validation, rule enforcement, and pre-commit checks.

```bash
vortex check                        # Fast incremental check (hits cache for unchanged contracts)
vortex check --breaking-only        # Fast pre-commit hook mode (<25ms) for Git hooks and CI
vortex check --fix                  # Automatically apply safe code fixes
vortex check -strict                # Treat warnings as errors
vortex check -sarif=security.sarif  # Export findings for GitHub Security Code Scanning
```

#### `vortex mock`
Generates fully functional in-memory HTTP/WebSocket mock servers for integration testing.

```bash
vortex mock                          # Generate mock servers for all contracts
vortex mock -fixtures                # Auto-populate zero-code mock responses from recorded traffic
vortex mock pkg/user/api.go          # Generate mock server for a specific contract
vortex mock -strict                  # Enforces strict parameter validation
```

#### `vortex env`
Scans contracts for `${VAR_NAME}` references and generates `.env.example` templates.

```bash
vortex env                           # Scan all contracts and output .env.example
vortex env --fill --out=.env.local   # Pre-fill values from local .vortex/cache/secrets.json
vortex env pkg/api/api.go --out=-    # Print required variables to stdout
```

#### `vortex smoke`
Rapidly probes live contract endpoints using stored secrets and renders a latency and TLS summary table.

```bash
vortex smoke                         # Probe safe GET/HEAD endpoints across workspace
vortex smoke pkg/api/api.go          # Probe specific contract endpoints
vortex smoke --all --timeout=10s     # Probe all endpoints including POST/PUT
```

---

### Toolchain Hubs

#### `vortex spec` (OpenAPI 3.1, HAR & Proto Hub)
Bidirectional schema toolchain for importing, exporting, and diffing OpenAPI and HAR specifications.

```bash
# Ingest OpenAPI / HAR traffic captures into declarative Go contracts (3-Way AST Merge):
vortex spec import -spec=openapi.json -out=./pkg/api/api.go
vortex spec import -spec=session.har -out=./pkg/api/api.go -add
vortex spec import traffic1.har,traffic2.har -mode=intersect # Baseline extraction

# Export @aoni:service contracts to OpenAPI 3.1:
vortex spec export -file=./pkg/api/api.go -out=openapi.json
vortex spec export -file=./pkg/api/api.go -yaml -out=openapi.yaml

# Semantic diff against OpenAPI specifications:
vortex spec diff openapi.json pkg/user/api.go
vortex spec diff spec_v1.json spec_v2.json

# Compile Protocol Buffer definitions:
vortex spec proto -src=./proto -out=./pkg/pb
```

#### `vortex traffic` (Network Sniffer, Cache & Secrets Hub)
Manages local traffic captures, live process sniffing, and the encrypted secrets vault.

```bash
# Capture live process network traffic into a HAR archive:
vortex traffic record -out=session.har -- ./mycli

# Manage local traffic cache (.vortex/cache/traffic):
vortex traffic list                  # List cached traffic sessions
vortex traffic show <id|hash>        # Inspect metadata of a cached session
vortex traffic store session.har     # Store session in local cache with auto-gunzip
vortex traffic export <id> -out=.    # Export clean, uncompressed HAR with gunzip
vortex traffic secrets               # Manage captured header/query secrets vault
vortex traffic prune                 # Clean old traffic snapshots
```

#### `vortex ast` (Tuple Deobfuscation, Refactoring & VCS Hub)
AST-level refactoring, tuple deobfuscation, interface splitting, and contract version control.

```bash
# Deobfuscate positional arrays / JSPB into typed @aoni:tuple structs:
vortex ast tuple pkg/api/client.go
vortex ast tuple pkg/api/client.go --dry-run

# Split monolithic interface into separate focused interfaces (ISP principle):
vortex ast split --from=MarketAPI --methods="Get*,List*" --to=MarketReaderAPI
vortex ast split --from=PriceDB --methods="Predict*" --to=PredictAPI --out=pkg/services/pricedb/predict.go

# Batch rename method names via regular expressions:
vortex ast rename --match="Fetch(.*)" --replace="Get$1" pkg/services/items/api.go

# Cherry-pick methods and DTO structs across contracts (transitive closure):
vortex ast pick pkg/services/inventory/api.go:GetItemPrices --to=pkg/services/items/api.go

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

#### `vortex perf` (Throughput, Profiler, Benchmarks & PGO Hub)
High-performance benchmarking, allocation tracking, and runtime optimization.

```bash
# Executive performance dashboard and pprof inspector:
vortex perf                          # Profile all workspace contracts
vortex perf prof --bench-time=100ms  # Custom benchmark duration

# Silicon hardware inspection and engine benchmarks:
vortex perf bench                    # Hardware CPU flags, AVX2/AVX-512, RPS scoring
vortex perf bench -quick             # Quick express benchmark

# Test coverage analyzer:
vortex perf cover                    # Deduplicated core test coverage analyzer

# Standalone test and benchmark harness generation:
vortex perf harness pkg/user/api.go  # Generate zero-allocation benchmark harness

# Profile-Guided Optimization (PGO):
vortex perf pgo -record=60s          # Collect runtime profile for default.pgo
```

---

### Workspace Management

#### `vortex init`
Scaffolds a `.vortex.yml` workspace configuration or new API package templates.

```bash
vortex init                          # Auto-discover existing Go interfaces
vortex init billing -tpl=rest        # REST CRUD API template
vortex init chat -tpl=ws            # WebSocket Bi-Directional Event Client
vortex init ai -tpl=sse             # Real-Time SSE & NDJSON Client
```

#### `vortex config`
Views, queries, and modifies workspace settings, code generation defaults, and lint rules.

```bash
vortex config list
vortex config get defaults.engine
vortex config set defaults.engine fast
vortex config lint disable S001 W002
```

#### `vortex status`
Performs a 360° health check across all registered contracts in the workspace.

```bash
vortex status
vortex status -strict  # Exits with code 1 if out-of-sync contracts exist
```

#### `vortex clean`
Removes generated test mocks, benchmark harnesses, CPU/memory profiles, and cache directories.

```bash
vortex clean           # Clean mocks, harnesses, coverage dumps, and cache
vortex clean --all     # Also remove primary generated API clients (*.gen.go)
```

---

## 4. Configuration Schema (`.vortex.yml`)

The `.vortex.yml` file defines workspace-wide defaults, contract registry paths, and lint rules.

```yaml
version: 1

defaults:
  casing: snake_case       # snake_case | camelCase | kebab-case
  engine: fast             # fast (fasthttp + H2/H3) | standard (net/http)
  retry: 2
  timeout: 10s

contracts:
  - name: UserAPI
    package: user
    file: pkg/user/api.go
    gen: pkg/user/api.gen.go
    models: pkg/user/models.gen.go
    upstream:
      source: https://api.example.com/openapi.json
      format: openapi
      poll_interval: 24h

  - name: MarketStreamAPI
    package: market
    file: pkg/market/api.go
    gen: pkg/market/api.gen.go

ignore:
  - "missing-response-schema"

secrets:
  headers:
    authorization: AUTH_TOKEN
    x-goog-api-key: GOOGLE_API_KEY
    unleash-instanceid: INSTANCE_ID
  query:
    key: GOOGLE_API_KEY
    api_key: API_KEY
  cookies:
    session_id: SESSION_ID
  patterns:
    - regex: "ya29\\.[a-zA-Z0-9_-]+"
      var: AUTH_TOKEN
    - regex: "AIzaSy[a-zA-Z0-9_-]{33}"
      var: GOOGLE_API_KEY

lint:
  ignore:
    - "query-param-casing"
  disable:
    - "S001"
```

---

## 5. End-to-End Workflows

### Authoring a Service Contract

#### Step 1: Declare the Interface (`pkg/billing/api.go`)

```go
package billing

import (
    "context"
    "github.com/lemon4ksan/aoni"
)

// @aoni:service
// @version "v1.0.0"
type BillingAPI interface {
    // @get "invoices/{id}"
    GetInvoice(ctx context.Context, id string, mods ...aoni.RequestModifier) (*InvoiceDTO, error)

    // @post "invoices"
    CreateInvoice(ctx context.Context, req *CreateInvoiceRequest, mods ...aoni.RequestModifier) (*InvoiceDTO, error)
}

type InvoiceDTO struct {
    ID     string  `json:"id"`
    Amount float64 `json:"amount"`
    Paid   bool    `json:"paid"`
}

type CreateInvoiceRequest struct {
    Amount   float64 `json:"amount"`
    Currency string  `json:"currency"`
}
```

#### Step 2: Generate the Client

```bash
vortex gen pkg/billing/api.go
```

#### Step 3: Use the Generated Client

```go
package main

import (
    "context"
    "fmt"
    "github.com/lemon4ksan/aoni"
    "github.com/lemon4ksan/aoni/option"
    "github.com/my/project/pkg/billing"
)

func main() {
    baseClient := aoni.NewClient(nil,
        option.WithBaseURL("https://api.billing.com"),
        option.WithChrome(),
    )

    client := billing.NewBillingAPI(baseClient)

    invoice, err := client.GetInvoice(context.Background(), "inv_10293")
    if err != nil {
        panic(err)
    }

    fmt.Printf("Invoice amount: $%.2f (Paid: %v)\n", invoice.Amount, invoice.Paid)
}
```

---

### In-Memory Mock Testing

Vortex generates in-memory mock servers that plug directly into `aoni.Client` without listening on network sockets or opening OS ports.

#### 1. Generate the Mock Server
```bash
vortex mock pkg/billing/api.go
```

#### 2. Execute Unit Tests Against the Mock
```go
package billing_test

import (
    "context"
    "testing"
    "github.com/stretchr/testify/require"
    "github.com/my/project/pkg/billing"
)

func TestBillingService(t *testing.T) {
    ctx := context.Background()

    // 1. Instantiate in-memory mock server
    mockServer := billing.NewBillingAPIMockServer()

    // 2. Define handler behaviors
    mockServer.OnGetInvoice = func(ctx context.Context, id string) (*billing.InvoiceDTO, error) {
        return &billing.InvoiceDTO{
            ID:     id,
            Amount: 149.99,
            Paid:   true,
        }, nil
    }

    // 3. Obtain preconfigured client routed directly to in-memory transport
    client := mockServer.Client()

    // 4. Test business logic
    inv, err := client.GetInvoice(ctx, "inv_test_123")
    require.NoError(t, err)
    require.Equal(t, "inv_test_123", inv.ID)
    require.Equal(t, 149.99, inv.Amount)
    require.True(t, inv.Paid)
}
```

---

### OpenAPI 3.1 Roundtrip Synchronization

1. **Importing an existing specification**:
   ```bash
   vortex init -from-openapi=./swagger.json -pkg=stripe -service=StripeAPI -out=pkg/stripe/api.go
   ```
2. **Checking for upstream breaking changes in CI**:
   ```bash
   vortex diff ./swagger.json pkg/stripe/api.go
   ```
3. **Exporting updated Go contracts back to OpenAPI 3.1**:
   ```bash
   vortex oapi pkg/stripe/api.go -out=./swagger.json
   ```

---

### AsyncAPI Event Streaming

```go
package telemetry

import (
    "context"
    "github.com/lemon4ksan/aoni"
)

// @aoni:service
type TelemetryStreamAPI interface {
    // @event "sensorReading"
    OnSensorReading(ctx context.Context, handler func(msg *ReadingDTO)) (aoni.Subscription, error)

    // @ws:emit "deviceCommand"
    SendCommand(ctx context.Context, cmd *CommandDTO, mods ...aoni.RequestModifier) error
}
```

---

### Protocol Buffers & vtprotobuf

Compile `.proto` schemas with zero-allocation `vtprotobuf` marshaling routines:

```bash
vortex proto -src=./proto -out=./pkg/proto -import=github.com/my/project/pkg/proto
```

Execute binary protobuf requests using `aoni/request` generic helpers:

```go
import (
    "github.com/lemon4ksan/aoni/request"
    pb "github.com/my/project/pkg/proto"
)

resp, err := request.PostProtoTo[pb.QueryResponse](ctx, client, "https://grpc.example.com/query", reqMsg)
```

---

### Shadow Root Source Mirroring (`@aoni:mirror`)

When building high-speed `aoni` wrappers over tightly-coupled or private legacy Go backends (which cannot be annotated with `@aoni:service` or regenerated), use `@aoni:mirror` to treat the legacy Go code as an **immutable, read-only Root of Truth**:

```go
package inventory

import (
    "context"
    "github.com/lemon4ksan/aoni"
)

type Item struct {
    AssetID uint64
    Name    string
}

// @aoni:service
// @aoni:mirror "internal/legacy/steam/inventory.go:LegacyInventoryService"
type InventoryWrapperAPI interface {
    // @get "inventory"
    GetInventory(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) ([]*Item, error)
}
```

#### Specialized Mirror Linter Rules:
| Rule ID | Name | Severity | Purpose |
| :--- | :--- | :--- | :--- |
| **`E015`** | `mirror-source-not-found` | **Error** | Target Go file or interface specified in `@mirror` does not exist on disk/AST. |
| **`E016`** | `mirror-signature-drift` | **Error** | Divergence in method signatures, parameter types, or DTO struct field types. |
| **`W012`** | `mirror-ghost-method` | **Warning** | New public method appeared in root legacy interface not yet exposed in wrapper. |

```bash
# Verify synchronization with legacy backend without touching any legacy files:
vortex check pkg/steam/inventory/api.go
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
          go-version: '1.25.x'

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

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
   - [Two-Tier Header Architecture](#two-tier-header-architecture)
3. [CLI Reference & Subcommands](#3-cli-reference--subcommands)
   - [Workspace Management (`vortex init`, `vortex config`, `vortex status`, `vortex clean`)](#workspace-management)
   - [Code Generation & Mocks (`vortex gen`, `vortex mock`)](#code-generation--mocks)
   - [Specification Ingestion & Export (`vortex source`, `vortex import`, `vortex export`, `vortex proto`)](#specification-ingestion--export)
   - [AST Manipulation & Refactoring (`vortex cherry-pick`, `vortex refactor`, `vortex undo`)](#ast-manipulation--refactoring)
   - [Validation, Diffing & Auditing (`vortex check`, `vortex diff`, `vortex blame`, `vortex tag`)](#validation-diffing--auditing)
   - [Performance & Benchmarking (`vortex bench`, `vortex prof`, `vortex pgo`)](#performance--benchmarking)
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

### Two-Tier Header Architecture

Vortex provides declarative header inheritance across two layers:
1. **Global Service Headers**: Inherited across all client methods (e.g. shared `User-Agent` or default `Authorization`).
2. **Per-Method Specific Headers**: Bound directly to an individual RPC endpoint (e.g. specific `Unleash-*` tokens or custom telemetry).

```go
// @aoni:service
// @header "User-Agent" "antigravity/cli/1.1.13"
type AntigravityAPI interface {
    // @post "api/client/register"
    // @header "Unleash-Appname" "codeium-language-server"
    // @header "Unleash-Interval" "60000"
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

### Workspace Management

#### `vortex init`
Scaffolds a `.vortex.yml` workspace configuration from auto-discovered Go interfaces, OpenAPI, or AsyncAPI specifications.

```bash
# Auto-discover existing Go interfaces in workspace
vortex init

# Force overwrite existing configuration
vortex init -force

# Scaffold from starter templates (rest, stream, ws, auth, stealth, grpc, socket, list)
vortex init billing -tpl=rest       # REST CRUD API with DTOs, pagination, and filters
vortex init chat -tpl=ws           # WebSocket Bi-Directional Event Client
vortex init ai -tpl=sse            # Real-Time Server-Sent Events (SSE) & NDJSON Client
vortex init auth -tpl=auth         # OAuth2 Token Auto-Rotation & HMAC Signing
vortex init market -tpl=stealth    # Anti-Bot Browser Impersonator with JA4 & p0f Spoofing
vortex init rpc -tpl=grpc          # Framed gRPC-Web & Protobuf Client
vortex init raw -tpl=socket        # Persistent High-Throughput Binary Socket Facade
vortex init -tpl=list              # View full interactive template catalog

# Ingest OpenAPI / HAR traffic captures directly into new package
vortex init agy -from=traffic.har -service=AntigravityAPI
vortex init agy -from=traffic1.har,traffic2.har -mode=intersect # Multi-HAR baseline extraction
vortex init petstore -from=openapi.json -pkg=petstore -out=pkg/petstore/api.go
vortex init market -from-asyncapi=./specs/market_stream.yaml -out=pkg/market/api.go
```

#### `vortex config` (Aliases: `cfg`, `conf`)
Views, queries, and modifies workspace settings, code generation defaults, and lint rules without manual YAML editing.

```bash
# List all workspace configurations
vortex config list
vortex config list --json

# Query specific properties
vortex config get defaults.casing
vortex config get defaults.engine

# Modify settings with type validation
vortex config set defaults.casing snake_case
vortex config set defaults.engine fast
vortex config set defaults.retry 3
vortex config set defaults.timeout 15s

# Manage lint rules and strict mode
vortex config lint disable S001 W002
vortex config lint enable S001
vortex config lint strict true

# Manage polyglot export plugins
vortex config plugin add ts --out=src/api.ts --contract=Market
vortex config plugin rm ts --contract=Market
```

#### `vortex status`
Performs a health check across all registered contracts in the workspace.

```bash
vortex status
vortex status -strict  # Exits with code 1 if out-of-sync contracts exist
vortex status -json    # Machine-readable output for CI/CD
```

#### `vortex clean`
Removes generated test mocks, benchmark harnesses, CPU/memory profiles, test binaries, and cache directories.

```bash
# Preview artifacts to delete without disk modifications
vortex clean -dry-run

# Clean mocks, harnesses, coverage dumps, and .vortex/ cache
vortex clean

# Also remove primary generated API clients (*.gen.go)
vortex clean --all
```

---

### Code Generation & Mocks

#### `vortex gen`
Compiles declarative Go interfaces into zero-allocation API client implementations.

```bash
vortex gen                     # Compile all contracts in .vortex.yml
vortex gen pkg/user/api.go     # Compile a single contract file
vortex gen -watch              # Continuous compilation on file save
vortex gen -dry-run            # Preview generated Go source code in stdout
```

#### `vortex mock`
Generates fully functional in-memory HTTP/WebSocket mock servers for contract testing.

```bash
vortex mock                    # Generate mock servers for all contracts
vortex mock pkg/user/api.go    # Generate mock server for a specific contract
vortex mock -strict            # Enforces strict parameter validation
```

---

### Specification Ingestion & Export

#### `vortex source` (Aliases: `src`, `upstream`, `remote`)
Manages, fetches, diffs, and synchronizes upstream OpenAPI/Swagger/AsyncAPI specifications with zero manual path passing.

```bash
# List all upstream sources and their status across the workspace
vortex source list

# Bind an upstream remote URL or local file to a contract
vortex source set PriceDB https://api.pricedb.net/openapi.json --fetch
vortex source set Bptf api/specs/bptf.yaml

# Unbind upstream source from a contract
vortex source rm PriceDB

# Fetch remote specs locally into api/specs/
vortex source fetch [contract]

# Verify reachability of remote endpoints
vortex source ping [contract]

# Semantic diff against configured upstream schema without specifying file paths
vortex source diff PriceDB

# One-step pipeline: fetch remote spec, check diff, and regenerate client
vortex source sync PriceDB
```

#### `vortex import` (Aliases: `oapi import`)
Imports OpenAPI 3.x, Swagger 2.0, or HAR Traffic Captures into declarative Go contracts with **3-Way AST Semantic Reconciliation** (`Git-merge for APIs`).

```bash
# Ingest single OpenAPI spec or HAR capture
vortex import -spec=openapi.json -out=./pkg/api/api.go
vortex import session.har -service=MyAPI -out=pkg/myapi/api.go

# Multi-spec composition (comma-separated or globs)
vortex import traffic1.har,traffic2.har -service=API -out=pkg/api/api.go
vortex import "specs/*.json" -out=pkg/api/api.go

# Set-Operation Merge Modes:
vortex import a.har,b.har -mode=union      # (Default) Union all endpoints (A ∪ B)
vortex import a.har,b.har -mode=intersect  # Baseline extraction: only endpoints in BOTH (A ∩ B)
vortex import a.har,b.har -mode=diff       # Delta extraction: only endpoints in A missing in B (A \ B)

# 3-Way AST Reconciliation Options:
vortex import session.har -out=pkg/api/api.go -add      # Preserve unmentioned endpoints as active
vortex import session.har -out=pkg/api/api.go -prune    # Prune unmentioned endpoints
vortex import session.har -out=pkg/api/api.go -f        # Discard existing and regenerate fresh
vortex import session.har -out=pkg/api/api.go -dry-run  # Preview changes without modifying disk
vortex import session.har -out=pkg/api/api.go -split    # Split into api.go and models.go
vortex import session.har -type-map=steam_id=id.ID      # Custom domain type mapping
```

**Reconciliation Engine Guarantees:**
* **Rule A (Type Fidelity)**: Custom Go domain types (`steam.ID`, `time.Time`, custom DTOs) are preserved and never degraded to generic `any`.
* **Rule B (Non-Destructive Addition)**: New endpoints in the upstream schema are appended with synthetic method signatures and DTO structs.
* **Rule C (Conservative Deprecation)**: Endpoints removed upstream are marked with `// @deprecated` rather than silently deleted (unless `-prune` is passed).

#### `vortex export` (Aliases: `oapi export`, `oapi`)
Exports declarative Go contracts to clean standard OpenAPI 3.1.0 JSON or YAML schemas.

```bash
# Standard clean OpenAPI 3.1.0 export (Default — no vendor extensions)
vortex export pkg/user/api.go -out=openapi.json
vortex export pkg/user/api.go -yaml -out=openapi.yaml

# Full fidelity export with vendor extensions (x-vortex-*)
vortex export pkg/user/api.go --vortex -out=openapi.json
```

#### `vortex proto`
Compiles Protocol Buffer definitions with zero-allocation `vtprotobuf` codecs.

```bash
vortex proto -src=./proto -out=./pkg/pb -import=github.com/my/project/pkg/pb
```

---

### AST Manipulation & Refactoring

#### `vortex cherry-pick` (Aliases: `cp`, `transplant`, `pick`)
Transplants a method or DTO struct from a source contract to a target contract AST, **automatically copying all dependent nested DTO structs** (transitive closure):

```bash
# Cherry-pick method and all its transitive parameter/return DTO structs
vortex cherry-pick pkg/services/mannco/api.go:GetItemPrices --to=pkg/services/bptf/api.go

# Cherry-pick a specific DTO struct and its nested types
vortex cherry-pick Mannco:ItemDTO --to=Bptf

# Preview transplantation without modifying files on disk
vortex cherry-pick pkg/services/pricedb/api.go:PredictSpell --to=pkg/services/crit/api.go --dry-run
```

#### `vortex refactor` (Aliases: `rebase`, `rf`, `reorganize`)
Provides interactive AST-level contract restructuring, interface splitting, and batch renaming:

```bash
# Split monolithic interface into separate focused interface (ISP principle)
vortex refactor split --from=MarketAPI --methods="Get*,List*" --to=MarketReaderAPI

# Split into a separate new file
vortex refactor split --from=PriceDB --methods="Predict*" --to=PredictAPI --out=pkg/services/pricedb/predict.go

# Batch rename method names via regular expressions
vortex refactor rename --match="Fetch(.*)" --replace="Get$1" pkg/services/bptf/api.go
#### `vortex history` (Aliases: `ops`, `operations`, `journal`)
Inspects the mutation journal of past AST refactors, cherry-picks, and workspace changes:

```bash
vortex history
vortex history --json
```

#### `vortex undo` (Aliases: `revert`, `rollback`, `pop`)
Reverts the last modifying AST operation (or a specific `op-id`) and automatically restores code and regenerates clients:

```bash
# Revert the latest operation (Ctrl+Z for AST refactors & cherry-picks)
vortex undo

# Revert a specific operation by ID
vortex undo op-a1b2c3
```

---

### Validation, Diffing & Auditing

#### `vortex check`
Performs static analysis, linting, and security audits across Go contracts with **incremental SHA-256 caching (`0.1ms`)**:

```bash
vortex check                        # Fast incremental check (hits cache for unchanged contracts)
vortex check --no-cache             # Clean audit ignoring local cache
vortex check -strict                # Treat warnings as errors
vortex check --fix                  # Automatically apply safe code fixes
vortex check -sarif=security.sarif  # Export findings for GitHub Security Code Scanning
```

#### `vortex diff`
Performs semantic schema comparison between a Go contract and an upstream OpenAPI specification, or directly **compares two external specs / HAR captures (Spec-to-Spec)**.

```bash
# Contract-to-Spec comparison:
vortex diff openapi.json pkg/user/api.go
vortex diff session.har MarketAPI

# Direct Spec-to-Spec comparison (No Go contract required):
vortex diff spec_v1.json spec_v2.json
vortex diff session_staging.har session_prod.har
vortex diff "specs_old/*.json" "specs_new/*.json"
```

#### `vortex review` & `vortex accept`
Interactive review tool for inspecting and accepting upstream schema changes.

```bash
vortex review openapi.json pkg/user/api.go
vortex accept openapi.json pkg/user/api.go
```

#### `vortex blame` (Aliases: `pvn`, `provenance`, `origin`)
Inspects contract lineage, directive origin, and Git author metadata for every method and DTO struct:

```bash
vortex blame pkg/services/bptf/api.go
vortex blame Market
vortex blame pkg/services/pricedb/api.go --method=GetPrice
vortex blame pkg/services/pricedb/api.go --json
```

#### `vortex tag` (Aliases: `release`, `semver`)
Manages contract SemVer release snapshots, changelogs, and Git release tags:

```bash
# Record a contract release snapshot
vortex tag add v1.2.0 -m "Release with inventory filters"
vortex tag add v1.3.0 Market -m "Add instant buy order endpoints"

# List all release tags and historical snapshots
vortex tag list

# Inspect detailed snapshot (endpoint list, author, timestamp)
vortex tag show v1.2.0
vortex tag rm v1.2.0
```

#### `vortex log`
Displays the version timeline and evolution of a service contract.

```bash
vortex log pkg/user/api.go
vortex log -json pkg/user/api.go
```

---

### Performance & Benchmarking

#### `vortex harness` & `vortex bench`
Generates and executes zero-regression benchmark suites.

```bash
vortex harness pkg/user/api.go
vortex bench -benchtime=3s -cpu=1,4,8
```

#### `vortex pgo`
Collects and merges runtime profiles for Go Profile-Guided Optimization (`default.pgo`).

```bash
vortex pgo -record=60s -out=default.pgo
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

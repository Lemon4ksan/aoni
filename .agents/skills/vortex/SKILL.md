---
name: vortex
description: >-
  Expert guide, reference, and operational playbook for the Vortex Zero-Allocation API Toolchain.
  Use when designing, writing, validating, modifying, or compiling @aoni API contracts, .vortex.yml configurations,
  declarative REST/WebSocket/SSE/Socket clients, test mocks, and polyglot SDKs.
---

# Vortex API Guardian & AST Toolchain Guide

**`vortex`** is the official zero-allocation AST-driven code generator, linter, and OpenAPI 3.1 toolchain for Go projects using the **Aoni** networking engine.

It transforms declarative Go interfaces into high-throughput, compile-time optimized HTTP, WebSocket, SSE, and Socket clients running at line speed with 0 heap allocations on hot paths.

---

## 1. Quick Start & Daily CLI Workflow

Command | Purpose | Common Flags
:--- | :--- | :---
`vortex` | **Intelligent Auto-Pilot**: Runs pre-flight audit, synchronizes upstreams, and compiles all contracts in one shot. | *Zero arguments needed*
`vortex status` | 360° synchronization dashboard across all workspace contracts, generated code, and upstream OpenAPI drifts. | `--check` (CI exit code 1 if stale), `--json`
`vortex doctor` | Validates toolchain health, `.vortex.yml` paths, git ignore rules, and upstream connectivity. | `--fix` (auto-heals `.gitignore` & `.gitattributes`), `--json`
`vortex gen` | Compiles declarative Go interface into high-performance client code (`*.gen.go`). | `-file=pkg/api/api.go`, `-out=...`, `--harness`
`vortex check` | Static contract linter and diagnostic inspector. | `--fix` (auto-fixes safe warnings), `--strict`, `--breaking-only`
`vortex mock` | Generates in-memory, mock HTTP server for deterministic unit and integration tests. | `-file=pkg/api/api.go`, `-out=...`
`vortex init` | Scaffolds a new API package or initializes `.vortex.yml` workspace configuration. | `[name]`, `-tpl=rest|ws|sse|stealth`, `--git`
`vortex spec import` | Ingests OpenAPI 3.x, Swagger 2.0, or HAR session files with 3-way AST merge. | `-spec=openapi.json`, `-out=pkg/api/api.go`, `--add`
`vortex clean` | Removes generated artifacts and cache databases while preserving core code. | `--dry-run`, `--all`

---

## 2. Workspace Configuration (`.vortex.yml`)

The `.vortex.yml` file defines workspace-level defaults and registers service contracts.

```yaml
# .vortex.yml — Vortex API Guardian Workspace Configuration
version: 1

defaults:
  # Casing strategy for generated JSON and wire parameters:
  # Options: snake_case (default), camelCase, PascalCase, flatcase, keep
  casing: snake_case

  # Core HTTP client engine:
  # Options: 'fast' (aoni/fast fasthttp zero-alloc engine) or 'standard' (net/http compatible)
  engine: fast

  # Default browser persona for stealth evasion (chrome, firefox, safari)
  # persona: chrome

  # Generate test mocks (*_mock.gen.go) by default during compilation
  # mock: true

  # Generate performance test harnesses (*_harness.gen.go)
  # harness: false

contracts:
  # 1. Standard REST Service Contract
  - name: UserAPI
    # Target Go contract file (or use 'dir: pkg/user' to imply pkg/user/api.go)
    file: pkg/user/api.go
    # Optional generated client file (default: pkg/user/api.gen.go)
    gen: pkg/user/api.gen.go
    # Optional generated DTO models file
    # models: pkg/user/models.gen.go

  # 2. Upstream Synchronized Contract (OpenAPI / External Dump)
  - name: PaymentAPI
    dir: pkg/payment # Automatically implies file: pkg/payment/api.go
    upstream:
      # Local path or remote URL to OpenAPI/Swagger specification
      source: https://api.example.com/openapi.json
      # Optional command hook to regenerate Go contract when upstream spec changes
      # generate: go run ./cmd/generator/payment
      # Optional command hook to download latest upstream schema
      # refresh: curl -s https://api.example.com/openapi.json -o openapi.json

  # 3. Polyglot Target Generation
  - name: ChatAPI
    file: pkg/chat/api.go
    plugins:
      - name: ts
        out: frontend/src/api/chat.ts
      - name: swift
        out: mobile/Sources/API/Chat.swift
```

---

## 3. Declarative Go Contract Specification (`@aoni` Directives)

A contract is written as a Go interface tagged with `@aoni` doc comments.

### Example Contract (`pkg/user/api.go`):

```go
package user

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.example.com/v1"
// @engine fast
// @casing snake_case
type UserAPI interface {
	// @get "users/{id}"
	// @unwrap "data"
	GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (*UserDTO, error)

	// @post "users"
	CreateUser(ctx context.Context, req CreateUserRequest, mods ...aoni.RequestModifier) (*UserDTO, error)

	// @put "users/{id}/avatar"
	// @form
	UploadAvatar(ctx context.Context, id string, file aoni.UploadFile, mods ...aoni.RequestModifier) error

	// @delete "users/{id}"
	DeleteUser(ctx context.Context, id string, mods ...aoni.RequestModifier) error
}

// @aoni:dto
type UserDTO struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// @aoni:dto
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}
```

---

## 4. Directive Catalog

### Service Scope (Interface Declarations)
Directive | Arguments | Description
:--- | :--- | :---
`@aoni:service` | *(none)* | Marks the Go interface as a managed Vortex service contract.
`@base_url` | `"https://..."` | Sets the default base URL for all endpoints in this service.
`@version` | `"v1.2.0"` | SemVer version tag for timeline tracking and drift detection.
`@source` | `"path/or/url"` | Associates the service with an upstream OpenAPI/Swagger specification.
`@engine` | `fast` \| `standard` | Overrides the client execution engine for this service.
`@casing` | `snake_case` \| `camelCase` \| `PascalCase` | Sets the default wire parameter casing strategy.
`@persona` | `chrome` \| `firefox` \| `safari` | Sets browser impersonation fingerprint profile.
`@timeout` | `"5s"` | Sets default HTTP request timeout for service methods.

### Method Scope (Endpoint Operations)
Directive | Arguments | Description
:--- | :--- | :---
`@get` | `"path/{param}"` | Declares an HTTP GET endpoint.
`@post` | `"path/{param}"` | Declares an HTTP POST endpoint.
`@put` | `"path/{param}"` | Declares an HTTP PUT endpoint.
`@delete` | `"path/{param}"` | Declares an HTTP DELETE endpoint.
`@patch` | `"path/{param}"` | Declares an HTTP PATCH endpoint.
`@head` | `"path/{param}"` | Declares an HTTP HEAD endpoint.
`@unwrap` | `"key.subkey"` | Automatically unwraps nested response JSON envelopes (e.g. `{"data": {...}}`).
`@form` | *(none)* | Encodes parameters as `application/x-www-form-urlencoded` or `multipart/form-data`.
`@ws` | `"path"` | Declares a bi-directional WebSocket connection endpoint.
`@stream` / `@sse` | `"path"` | Declares a Server-Sent Events (SSE) or NDJSON real-time stream endpoint.
`@deprecated` | `"[v1.2.0] reason"` | Marks method as deprecated with graceful telemetry warnings.

### Parameter & Header Scope
Directive | Arguments | Description
:--- | :--- | :---
`@query` | `"name"` | Binds parameter explicitly to a URL query parameter.
`@header` | `"Name"` | Binds parameter or static value to an HTTP header.
`@cookie` | `"Name"` | Binds parameter to an HTTP Cookie.
`@body` | *(none)* | Binds parameter explicitly as the JSON/Protobuf request body.
`@referer` | `"https://..."` | Sets static or dynamic HTTP `Referer` header.
`@stealth` | *(none)* | Applies dynamic jitter, randomized header ordering, and anti-bot evasions.

### Struct / DTO Scope
Directive | Arguments | Description
:--- | :--- | :---
`@aoni:dto` | `[casing=...]` | Marks struct as a managed DTO with automated zero-alloc serializers.
`@tuple` | `[endian=...]` | Declares compact binary tuple encoding.
`@union` | `discriminator=field`| Declares a tagged polymorphic union type.

---

## 5. Golden Rules for Writing Contracts

1. **Method Signatures**:
   - First parameter **must** be `ctx context.Context`.
   - Last parameter **must** be `mods ...aoni.RequestModifier` (allows functional modifiers like `mod.WithHeader`, `mod.WithTimeout`).
   - Return values should be `(*ReturnType, error)` or `(ReturnType, error)` or `error`.
2. **Zero-Allocation Invariant**:
   - Return pointer to DTO `(*UserDTO, error)` or primitive/map types to allow pooled decoders.
3. **No Low-Level Logic in Interface**:
   - Interfaces must stay 100% declarative. Vortex generates the entire execution body automatically.

---

## 6. Troubleshooting & Quick Fixes

Problem | Root Cause | Solution
:--- | :--- | :---
`E001 Stale codegen` | Interface modified without recompiling `*.gen.go` | Run `vortex check --fix` or `vortex gen`
`E003 Missing HTTP method` | Interface method has no `@get`, `@post`, etc. | Add `@get "/path"` or run `vortex spec import` if upstream exists
`E004 Missing context` | 1st parameter is not `context.Context` | Add `ctx context.Context` as first parameter
`Contract missing target path` | `.vortex.yml` entry has no `file` or `dir` | Add `file: pkg/name/api.go` or `dir: pkg/name`
`Doctor warns about .gitignore` | Ephemeral test mocks not ignored | Run `vortex doctor --fix`

# Specification: Vortex DSL, Architecture Standard & Contract Inspector

`vortex` is the official AST-driven toolchain, code generation engine, and static contract inspector for Go that compiles declarative API contracts into zero-allocation (`0 B/op`), type-safe, Chromium-resilient HTTP/RPC network clients powered by the `aoni` networking engine.

> **Engineering Manifesto**:
> _«As soon as bytes need to leave one machine and get to another, this will happen with 0 allocations, at maximum silicon speed, without type desync and without the chance of being blocked by a WAF»._

## 1. Core Architectural Pillars

1. **The Iron Invariant (1 Method = Exactly 1 Network Operation)**:
   - **No Hidden Business Logic**: Each interface method maps deterministically to **exactly 1 network request or 1 persistent session**. Vortex handles strictly transport, serialization, wire protocol framing, and response decoding.
   - **Pure Transport Scope**:
     1. **Dynamic Host Authority (RFC 3986)**: `https://{tenant}.slack.com/api` or `https://{bucket}.s3.amazonaws.com` zero-alloc URL synthesis.
     2. **Multi-Status Wire Unions**: Strict typed decoding of status-dependent bodies (e.g. `200 OK -> *Order`, `202 Accepted -> *QueuedTask`) without `any` boxing.
     3. **Deep Query & Matrix Serialization (RFC 6570 / OpenAPI 3.1)**: Stack-allocated `deepObject` (`filter[id]=1`), `pipe`/`space`/`bracket`, and `matrix` parameter packing.
     4. **Pre-Flight Wire Signatures**: Deterministic HMAC / SigV4 header canonicalization before wire transmission without extraneous network calls.
     5. **RFC 9110 Byte Ranges**: Declarative single-request chunk fetching (`Range: bytes=0-1048576`).
2. **Strict Separation of Concerns**:
   - **Contract (DSL Directives)**: Defines the remote server's interface (method, path, headers, payloads, signatures).
   - **Infrastructure (`aoni.ClientOption`)**: Configures connection pools, TLS fingerprints, DNS resolvers, and proxy rotators.
   - **Dynamic Context (`aoni.RequestModifier`)**: Injects per-call parameters, runtime cancellation, and trace spans.
3. **Zero-Allocation Execution Path**:
   - Compiles static routes, stack buffers (`[256]byte`), and direct type encoders (`strconv.AppendInt`, `urlutil.AppendQueryEscapeString`) without interface boxing or reflection.
4. **Standard-Library & Tooling Compliance**:
   - Directives are written as standard Go doc comments. All interface definitions compile as valid Go without pre-processing.
5. **Contract Integrity & Static Linting**:
   - Built-in pluggable contract linter (`vortex check`) that guarantees drift prevention between interfaces and generated code, verifies type safety, and provides safe automated fixes (`--fix`).

## 2. Formal Grammar (EBNF)

```ebnf
DirectiveComment  ::= "//" WS* "@" DirectiveName [ WS+ DirectiveValue ] [ WS+ DirectiveArgList ]
DirectiveName     ::= [a-zA-Z0-9_:-]+
DirectiveValue    ::= StringLiteral | Identifier
DirectiveArgList  ::= DirectiveArg ( WS+ DirectiveArg )*
DirectiveArg      ::= Identifier "=" ( StringLiteral | Identifier | Number )
StringLiteral     ::= '"' ( [^"\\] | '\\' . )* '"'
Identifier        ::= [a-zA-Z_][a-zA-Z0-9_]*
WS                ::= ' ' | '\t'

SuppressionComment ::= "//" WS* "vortex:ignore" [ "-service" ] WS+ RuleList [ WS+ "--" WS* Reason ]
RuleList          ::= RuleIdentifier ( WS* "," WS* RuleIdentifier )*
RuleIdentifier    ::= [a-zA-Z0-9_-]+
Reason            ::= [^\r\n]*
```

## 3. Scope Hierarchy

```text
┌────────────────────────────────────────────────────────────────────────┐
│ SERVICE / SOCKET SCOPE (@aoni:service, @aoni:socket, @base_url)        │
│                                                                        │
│   ┌──────────────────────────────────────────────────────────────────┐ │
│   │ METHOD SCOPE (@get, @post, @form, @referer, @inject, @check)     │ │
│   │                                                                  │ │
│   │   ┌────────────────────────────────────────────────────────────┐ │ │
│   │   │ PARAMETER SCOPE (@query, @header, @format, @file, @part)   │ │ │
│   │   └────────────────────────────────────────────────────────────┘ │ │
│   └──────────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│ DTO & STRUCT SCOPE (@aoni:dto, @tuple, @union)                         │
└────────────────────────────────────────────────────────────────────────┘
```

## 4. Directive Reference

### Service Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@aoni:service (or @service)` | `name`, `casing`, `prefix` | Marks an interface as a declarative API contract for client code generation. |
| `@auth` | `type`, `header`, `prefix`, `provider` | Configures automated authentication header or OAuth2 token exchange. |
| `@base_url` | `"https://api.example.com/v1"` | Sets the service-wide base URL endpoint. |
| `@casing` | `snake_case \| flatcase \| camelCase \| kebab-case \| PascalCase \| none` | Sets default wire parameter casing style for service or form payload. |
| `@circuit` | `threshold`, `cooldown` | Configures automated circuit breaking against upstream outages. |
| `@engine` | `fast \| std \| custom \| required`, `type`, `required` | Selects underlying execution engine (fast.Client, net/http, or strictly required custom requester). |
| `@envelope` | `"data" \| "result"` | Sets default JSON envelope field to unwrap across all response models. |
| `@header` | `"Key: Value"` | Adds a static/dynamic default HTTP header to requests or binds parameter to a header. |
| `@p0f` | `"windows" \| "linux" \| "macos" \| "ios" \| "android"` | Spoofs TCP/IP SYN packet fingerprint (p0f) for OS stack evasion. |
| `@persona` | `"chrome_133" \| "firefox_135" \| "safari_18"` | Configures Chromium / Firefox / Safari browser impersonation profile. |
| `@protocol` | `http \| rpc \| socket \| channel \| grpc \| ws \| ssh` | Selects underlying communication protocol. |
| `@retry` | `attempts`, `backoff`, `max_backoff`, `on_status` | Configures automated retry policy with exponential backoff and jitter. |
| `@ssh` | `host`, `user`, `key`, `pass_env`, `agent` | Configures SSH connection parameters or command execution. |
| `@timeout` | `"5s" \| "500ms"` | Sets execution timeout for the service or individual method. |
| `@tls_spec` | `"chrome_auto"` | Configures TLS ClientHello fingerprint emulation specification. |
| `@type_map` | `<Type> -> <Strategy>` | Configures package-wide serialization strategy for specific Go types. |

### Socket Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@aoni:socket (or @socket)` | — | Marks an interface for persistent multi-core socket facade code generation. |
| `@endpoint` | `<EndpointType>` | Defines the target connection endpoint struct type for connector dialing. |
| `@heartbeat` | `interval` *(required)*, `opcode`, `msg` | Configures background ping/heartbeat loop parameters. |
| `@job_id` | `<JobIDType>` | Defines the integer job ID type used for RPC request/response correlation. |
| `@opcode` | `<OpCodeType>` | Defines the opcode enum type used for message dispatching. |
| `@packet` | `<PacketType>` | Defines the decoded packet structure passed through processor and dispatcher. |

### Method Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@body` | `<pipeline expression>` | Configures a Wire-Transform pipeline chain for outbound request body payload serialization. |
| `@cache` | `"1m" \| "30s"` | Enables method-level in-memory response caching TTL. |
| `@call` | `<pkg.Func>` | Escape hatch: delegates request execution to custom generic dispatcher function. |
| `@casing` | `snake_case \| flatcase \| camelCase \| kebab-case \| PascalCase \| none` | Sets default wire parameter casing style for service or form payload. |
| `@check` | `<field> <op> <expected> "<error_message>"` | Emits post-execution assertion check validating response status or payload properties. |
| `@coalesce` | — | Deduplicates concurrent in-flight requests with identical arguments via SingleFlight. |
| `@codec` | `<codecFunc>` | Specifies custom combined encoder/decoder codec. |
| `@decoder` | `<decoderFunc>` | Specifies custom response body decoder function. |
| `@delete` | `"/path/{var}"` | Defines an HTTP DELETE endpoint route. |
| `@encoder` | `<encoderFunc>` | Specifies custom request body encoder function. |
| `@etag` | — | Enables automatic HTTP 304 conditional ETag caching and If-None-Match headers. |
| `@event` | `<eventName>` | Subscribes to an inbound push event with a typed handler callback. |
| `@expect_status` | `<status_code>...` | Declares expected success HTTP status codes (returns error if mismatch). |
| `@extract` | `between`, `regex`, `attr`, `css` | Extracts response payload via regular expressions, boundary slicing, or DOM attribute tokens. |
| `@form` | — | Configures application/x-www-form-urlencoded request payload encoding. |
| `@get` | `"/path/{var}"` | Defines an HTTP GET endpoint route. |
| `@grpc` | `"/pkg.Service/Method"` | Defines a gRPC / gRPC-Web unary or streaming remote procedure call. |
| `@head` | `"/path/{var}"` | Defines an HTTP HEAD endpoint route. |
| `@idempotent` | — | Declares request as strictly idempotent for aggressive safe retry policies. |
| `@inject` | `target="field\|query\|header"`, `provider` | Injects session cookies, CSRF tokens, or secrets dynamically from requester context. |
| `@multipart` | — | Configures multipart/form-data request payload encoding with streaming boundaries. |
| `@notify` | `opcode` *(required)*, `msg` | Defines a one-way fire-and-forget persistent socket RPC notification. |
| `@op_id` | `<id>` | Sets numeric or string opcode ID for universal RPC dispatching. |
| `@options` | `"/path/{var}"` | Defines an HTTP OPTIONS endpoint route. |
| `@patch` | `"/path/{var}"` | Defines an HTTP PATCH endpoint route. |
| `@pipeline` | `<pipeline expression>` | Configures a Wire-Transform pipeline chain on outbound payload or inbound response. |
| `@post` | `"/path/{var}"` | Defines an HTTP POST endpoint route. |
| `@preset` | `"steam_webapi" \| "json_api"` | Applies predefined suite of headers, casings, and serialization formats. |
| `@put` | `"/path/{var}"` | Defines an HTTP PUT endpoint route. |
| `@referer` | `"https://..." \| "{var}"` | Sets static or dynamic Referer header for anti-scraping and CDN bypass. |
| `@req` | `baseURL` | Directs execution to an isolated SubRequester instance clustered by base URL. |
| `@return` | `<pipeline expression>` | Configures response transformation pipeline applied prior to decoding. |
| `@rpc` | `opcode` *(required)*, `job_id`, `reply_opcode` | Defines a bidirectional request-response persistent socket RPC call. |
| `@sign_hmac` | `key`, `env`, `algo`, `header` | Configures cryptographic HMAC request signing over headers, URI, or payload body. |
| `@ssh_exec` | `"command {var}"` | Executes remote SSH command and captures typed output. |
| `@ssh_shell` | — | Spawns interactive remote SSH PTY session. |
| `@status` | `<Code> -> <Type>` | Maps non-200 HTTP status codes directly to discriminated variant response models. |
| `@stream` | `client \| server \| bidi \| sse \| ndjson` | Configures client/server streaming for gRPC, SSE, or NDJSON. |
| `@timeout` | `"5s" \| "500ms"` | Sets execution timeout for the service or individual method. |
| `@unwrap` | `"data" \| "result"` | Automatically unwraps nested envelope object into target return type. |
| `@ws_on` | `<eventName>` | Subscribes to real-time WebSocket / Socket.IO event with typed message handler. |

### Parameter Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@arena` | — | Binds parameter memory allocation to an off-heap Arena allocator. |
| `@buf` | — | Binds a caller-supplied byte slice for zero-allocation response body copying. |
| `@cookie` | `"Name"` | Binds parameter value to an HTTP Cookie header. |
| `@file` | `"fieldName" [filename="name.ext"]` | Binds parameter as a streaming multipart file upload payload. |
| `@format` | `unix_sec \| unix_milli \| unix_nano \| rfc3339 \| hex \| base64 \| direct` | Configures compile-time formatting strategy for parameter serialization. |
| `@header` | `"Key: Value"` | Adds a static/dynamic default HTTP header to requests or binds parameter to a header. |
| `@part` | `"fieldName" [content_type="..."]` | Binds parameter as a multipart form field part. |
| `@pipeline` | `<pipeline expression>` | Configures parameter transformation pipeline prior to network serialization. |
| `@query` | `"key" \| "key=val"` | Binds parameter to an HTTP URL query string parameter. |
| `@time_layout` | `"2006-01-02"` | Custom `time.Time` formatting layout string. |

### Struct & Bitpack Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@aoni:dto (or @dto)` | `casing`, `omitempty` | Generates compiled AppendFormData and AppendQuery zero-allocation serialization methods. |
| `@aoni:tuple (or @tuple)` | — | Generates zero-alloc custom UnmarshalJSON decoder for positional JSON arrays. |
| `@aoni:union (or @union)` | — | Generates discriminator-based polymorphism and JSON unmarshaling for tagged unions. |
| `@aoni:bitpack (or @bitpack)` | `endian="little\|big"` | Generates SIMD-accelerated zero-allocation binary bitfield packing (Pack/Unpack/PackUint64/UnpackUint64). |

---

## 5. Compile-Time Optimization Pipeline

Vortex includes an AST optimization pass (`internal/codegen/optimizer`) executed before code emission:

```
Parsed AST / IR
      │
      ▼
┌────────────────────────────────────────────────────────┐
│              Optimizer Pipeline Passes                 │
├────────────────────────────────────────────────────────┤
│ 1. SubRequester Clustering                             │
│    • Partitions methods by target BaseURL domain       │
│    • Shares connection pools across identical origins  │
│                                                        │
│ 2. Stack Sizing & Preallocation Calculation            │
│    • Precalculates StackModsSize for 0-alloc arrays   │
│    • Precalculates StackBufSize for URI & query buffer │
│                                                        │
│ 3. Zero-Reflect Codec Specialization                   │
│    • Generates direct type-safe unmarshaling calls     │
│    • Employs strconv & SIMD byte formatting primitives │
└────────────────────────────────────────────────────────┘
      │
      ▼
Optimized Emitter (`api.gen.go`)
```

### Key Optimization Guarantees:
1. **Zero Heap Allocations on Hot Paths**: Method calls with stack-sized arguments allocate `0 B/op`.
2. **Deterministic Connection Sharing**: Subrequesters prevent redundant TCP handshakes and TLS negotiations.
3. **Idempotent AST Transformations**: Passes run deterministically without state corruption.

---

## 6. Contract Inspector & Linter (`vortex check`)

Vortex includes a static analysis and diagnostic framework (`internal/codegen/lint`) with strict safety tiers and inline suppression directives.

### Safety Tiers

| Tier | Behavior with `--fix` | Description |
| :--- | :---: | :--- |
| **Safe Automated Fixes** | ✅ Applied Automatically | 100% deterministic artifact synchronization and canonical replacements (`stale-codegen`, `deprecated-alias`, `redundant-tag`, `canonical-format`). |
| **Heuristic Warnings & Suggestions** | ❌ Report Only | Code smell and architectural suggestions that never mutate developer intent (`param-lifting`, `http-verb-mismatch`). |

### Standard Rule Suite

#### 1. Errors (`E`) — Compiler Invariants & RFC Standards
| Rule ID | Rule Name | Category | Severity | Fixable? | Description |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `E001` | `stale-codegen` | `Codegen` | `ERROR` | ✅ Yes | Target `*.gen.go` is missing or out-of-sync with interface AST |
| `E002` | `unmatched-path` | `Correctness` | `ERROR` | ❌ No | URL path `{variable}` has no matching parameter in method signature |
| `E003` | `missing-http-method` | `Correctness` | `ERROR` | ❌ No | Method is missing `@get`, `@post`, etc. directive |
| `E004` | `missing-context` | `Correctness` | `ERROR` | ❌ No | First method parameter is not `context.Context` |
| `E005` | `unrecognized-directive` | `Correctness` | `ERROR` | ❌ No | Unknown or misspelled directive |
| `E006` | `conflicting-payload` | `Correctness` | `ERROR` | ❌ No | Mutually exclusive body payload formats specified |
| `E007` | `illegal-body-method` | `Correctness` | `ERROR` | ❌ No | Body payload specified on GET/HEAD method (RFC 9110 violation) |
| `E008` | `missing-error-return` | `Correctness` | `ERROR` | ❌ No | Method signature does not return `error` |
| `E009` | `unbound-header-variable` | `Correctness` | `ERROR` | ❌ No | Dynamic header template `{var}` not found in parameters or injections |
| `E010` | `duplicate-route-collision` | `Correctness` | `ERROR` | ❌ No | Two methods in the same service share identical HTTP verb and path |
| `E011` | `invalid-check-field` | `Correctness` | `ERROR` | ❌ No | Field in `@check` assertion does not exist on response model |
| `E012` | `invalid-bitpack` | `Correctness` | `ERROR` | ❌ No | Bitpack struct has invalid bit widths or type overflows |
| `E013` | `shadowed-wire-name` | `Correctness` | `ERROR` | ❌ No | Parameter wire keys collide under active casing strategy |

#### 2. Performance (`P`) — Zero-Alloc & Memory Safety
| Rule ID | Rule Name | Category | Severity | Fixable? | Description |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `P001` | `missing-dto-encoder` | `Performance` | `WARN` | ✅ Yes | Struct payload lacks compile-time `@aoni:dto` encoder (auto-inserts `// @aoni:dto`) |
| `P002` | `any-param-boxing` | `Performance` | `WARN` | ❌ No | Parameter typed as `any` causing heap boxing allocation |
| `P003` | `unformatted-slice-strategy` | `Performance` | `WARN` | ✅ Yes | Slice query parameter lacks `@format` (auto-inserts `// @format comma`) |
| `P004` | `oversized-stack-frame` | `Performance` | `WARN` | ❌ No | Static buffer size > 2KB stack threshold |
| `P005` | `missing-coalesce-on-heavy-get` | `Performance` | `WARN` | ❌ No | Heavy GET read model without `@coalesce` deduplication |

#### 3. Security (`S`) — Credentials & Stealth Protection
| Rule ID | Rule Name | Category | Severity | Fixable? | Description |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `S001` | `sensitive-query-param` | `Security` | `WARN` | ❌ No | Sensitive credential passed in URL query string (leaks in logs/proxies) |
| `S002` | `hardcoded-signing-secret` | `Security` | `WARN` | ✅ Yes | Plaintext signing secret hardcoded in directive (auto-replaces with `key_env`) |
| `S003` | `header-crlf-injection-risk` | `Security` | `WARN` | ❌ No | Dynamic header template variable without escaping filter |
| `S004` | `naked-scraper-contract` | `Security` | `WARN` | ❌ No | Web scraping `@extract` used without browser persona stealth profile |

#### 4. Style & Hygiene (`W`) — Contract Clarity & Format
| Rule ID | Rule Name | Category | Severity | Fixable? | Description |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `W001` | `param-lifting` | `Style` | `WARN` | ❌ No | Parameter repeated across $\ge 4$ methods (suggests service-level parameter) |
| `W002` | `deprecated-alias` | `Style` | `WARN` | ✅ Yes | Deprecated directive or alias used (auto-normalizes to canonical form) |
| `W003` | `http-verb-mismatch` | `Style` | `WARN` | ❌ No | Read-only prefix (`Get...`, `List...`) annotated with `@post` |
| `W004` | `redundant-tag` | `Style` | `WARN` | ✅ Yes | Redundant `@query` or `@field` tag matching default casing inference |
| `W005` | `canonical-format` | `Style` | `WARN` | ✅ Yes | Directives not in canonical hierarchy (auto-reorders doc comments) |
| `W006` | `dead-directive` | `Style` | `WARN` | ✅ Yes | Ineffective or orphaned directive (e.g. `@unwrap` on void, `@cache` on mutation) |
| `W007` | `unused-param` | `Style` | `WARN` | ❌ No | Method parameter not bound to any wire element |
| `W008` | `invalid-status-code-range` | `Style` | `WARN` | ❌ No | HTTP status code outside standard RFC range 100–599 |

### Inline Suppression Directives

Developers can suppress warnings on method or service scopes using standard Go comments:

```go
// @post /GetMarketHistory
//vortex:ignore http-verb-mismatch, param-lifting -- Required by Steam WebAPI POST protocol
GetMarketHistory(ctx context.Context, sessionID string) (*MarketHistory, error)
```

For service-wide suppressions:

```go
// @aoni:service
//vortex:ignore-service param-lifting
type LegacyAPI interface { ... }
```

## 7. CLI Reference (`cmd/vortex`)

### Installation
```bash
go install github.com/lemon4ksan/aoni/cmd/vortex@latest
```

### Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `vortex [flags] [paths...]` | `vortex ./...` | Generates zero-allocation Go client implementation (`.gen.go`). |
| `vortex check [flags] [paths...]` | `vortex check ./...` | Inspects contracts and reports AST diagnostics. |
| `vortex check --fix [paths...]` | `vortex check --fix ./...` | Automatically synchronizes and fixes all safe issues. |
| `vortex check --json [paths...]` | `vortex check --json ./...` | Outputs structured JSON diagnostics for CI/CD pipelines. |
| `vortex watch [paths...]` | `vortex watch ./...` | Watches source directories and auto-compiles on save. |
| `vortex bench` | `vortex bench -h2 -h3` | Silicon hardware benchmark & throughput inspector. |
| `vortex cover` | `vortex cover -file=coverage.out` | Deduplicated test coverage analyzer. |
| `vortex oapi import` | `vortex oapi import -spec=spec.json` | Imports OpenAPI 3.1 schema into declarative Go interface contracts. |
| `vortex oapi export` | `vortex oapi export -out=spec.json` | Exports Go interface contracts into OpenAPI 3.1 specification. |
| `vortex list` | `vortex list -scope=method` | Lists all supported directives and documentation. |
| `vortex explain <dir>` | `vortex explain form` | Displays syntax documentation and runnable example for a directive. |

### Go Generate Integration
Add this directive at the top of contract files:
```go
//go:generate go run github.com/lemon4ksan/aoni/cmd/vortex -file=$GOFILE
```
Then execute:
```bash
go generate ./...
```

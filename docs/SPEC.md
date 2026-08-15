# Specification: DSL & Architecture Standard

`aoni-gen` is an AST-driven code generation engine for Go that compiles declarative API contracts into zero-allocation (`0 B/op`), type-safe, Chromium-resilient HTTP/RPC network clients powered by the `aoni` networking engine.

## 1. Core Architectural Pillars

1. **Strict Separation of Concerns**:
   - **Contract (DSL Directives)**: Defines the remote server's interface (method, path, headers, payloads, signatures).
   - **Infrastructure (`aoni.ClientOption`)**: Configures connection pools, TLS fingerprints, DNS resolvers, and proxy rotators.
   - **Dynamic Context (`aoni.RequestModifier`)**: Injects per-call parameters, runtime cancellation, and trace spans.
2. **Zero-Allocation Execution Path**:
   - Compiles static routes, stack buffers (`[256]byte`), and direct type encoders (`strconv.AppendInt`, `urlutil.AppendQueryEscapeString`) without interface boxing or reflection.
3. **Standard-Library & Tooling Compliance**:
   - Directives are written as standard Go doc comments. All interface definitions compile as valid Go without pre-processing.

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
| `@form` | `casing` | Encodes request body as application/x-www-form-urlencoded on stack buffer (0 B/op). |
| `@get` | `"/path/{var}"` | Defines an HTTP GET endpoint route with optional path template variables. |
| `@grpc` | `"/package.Service/Method"` | Configures gRPC procedure call. |
| `@grpc-web` | — | Encodes request as 5-byte framed gRPC-Web protocol with trailer validation. |
| `@head` | `"/path/{var}"` | Defines an HTTP HEAD endpoint route. |
| `@idempotent (or @idempotency_key)` | — | Injects time-ordered UUIDv7 into Idempotency-Key header on stack buffer (0 B/op). |
| `@inject` | `field`, `query`, `header`, `from` *(required)* | Performs zero-cost interface assertion on requester to inject dynamic session/CSRF tokens. |
| `@json` | — | Serializes request body as JSON. |
| `@multipart` | — | Encodes request body as multipart/form-data with zero-alloc boundary streaming. |
| `@notify` | `<operationName>` | Defines a one-way fire-and-forget asynchronous notification. |
| `@op (or @operation)` | `<operationName>` | Defines a generic RPC request-response operation. |
| `@options` | `"/path/{var}"` | Defines an HTTP OPTIONS endpoint route. |
| `@patch` | `"/path/{var}"` | Defines an HTTP PATCH endpoint route. |
| `@post` | `"/path/{var}"` | Defines an HTTP POST endpoint route with optional path template variables. |
| `@preset` | `:xhr \| :cors \| :navigate` | Injects browser header presets (X-Requested-With, Sec-Fetch-*, Accept). |
| `@proto` | — | Serializes request body and deserializes response via Protocol Buffers. |
| `@put` | `"/path/{var}"` | Defines an HTTP PUT endpoint route. |
| `@raw` | — | Sends raw binary byte slice or io.Reader stream directly. |
| `@referer` | `<path_template> \| :origin \| :page \| :parent \| :self` | Generates dynamic Referer header directly on a 128-byte stack buffer (0 B/op). |
| `@return` | `<pipeline expression>` | Configures a Wire-Transform pipeline chain for scraping, decoding, or transforming responses. |
| `@sign` | `secret`, `key_env`, `algo`, `header` | Calculates cryptographic HMAC request signature and attaches auth headers. |
| `@ssh` | `host`, `user`, `key`, `pass_env`, `agent` | Configures SSH connection parameters or command execution. |
| `@stream` | `sse \| ndjson \| raw` | Enables response streaming mode via Server-Sent Events, NDJSON, or raw chunked channel. |
| `@timeout` | `"5s" \| "500ms"` | Sets execution timeout for the service or individual method. |
| `@unwrap` | `<fieldName>` | Unwraps specific field from JSON response envelope before returning. |
| `@ws (or @websocket)` | `<event_name>` | Configures WebSocket / Socket.IO event emission or subscription. |

### Param Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@cast` | `<GoType>` | Applies explicit type casting before serialization. |
| `@cookie` | `<cookie_name>` | Binds function parameter to Cookie header. |
| `@field` | `<wire_name>` | Binds function parameter to application/x-www-form-urlencoded or multipart form field. |
| `@file` | `name`, `filename`, `content_type` | Binds byte slice, string, or io.Reader to multipart file upload part. |
| `@format` | `unix_s \| unix_ms \| rfc3339 \| bool_int \| flag \| json \| comma \| pipe \| space \| bracket`, `layout` | Specifies serialization format strategy for parameter value. |
| `@header` | `"Key: Value"` | Adds a static/dynamic default HTTP header to requests or binds parameter to a header. |
| `@part` | `<part_name>` | Binds function parameter to multipart form part. |
| `@path (or @param)` | `<var_name>` | Binds function parameter to URL path template variable. |
| `@query` | `<wire_name>` | Binds function parameter to URL query parameter with zero-alloc string formatting. |

### Struct Scope Directives

| Directive | Arguments / Value | Description |
| :--- | :--- | :--- |
| `@aoni:dto (or @dto)` | `casing`, `omitempty` | Generates compiled AppendFormData and AppendQuery zero-allocation serialization methods. |
| `@aoni:tuple (or @tuple)` | — | Generates zero-alloc custom UnmarshalJSON decoder for positional JSON arrays (e.g. [12.5, 100, "ok"]). |
| `@aoni:union (or @union)` | — | Generates discriminator-based polymorphism and JSON unmarshaling for tagged unions. |

---

## 5. End-to-End Real World Recipes

### Recipe 1: Modern REST Client with Smart Casing & Referer Stack Buffer
```go
// @aoni:service casing=snake_case
// @base_url "https://steamcommunity.com/"
// @header "Origin: https://steamcommunity.com"
type TradeCommunityAPI interface {
    // @post "tradeoffer/new/send"
    // @form casing=flatcase
    // @header "Referer: https://steamcommunity.com/tradeoffer/new/?partner={partnerID}"
    SendOffer(ctx context.Context, partnerID uint32, req SendNewTradeOfferRequest, mods ...aoni.RequestModifier) (*SendNewTradeOfferResponse, error)

    // @post "tradeoffer/{offerID}/accept"
    // @form casing=flatcase
    // @header "Referer: https://steamcommunity.com/tradeoffer/{offerID}/"
    AcceptOffer(ctx context.Context, offerID uint64, req AcceptTradeOfferRequest, mods ...aoni.RequestModifier) (*AcceptTradeOfferResponse, error)
}
```

### Recipe 2: Zero-Alloc HTML Scraping & Pipeline Transformation
```go
// @aoni:service
// @base_url "https://steamcommunity.com"
type SteamProfileAPI interface {
    // @get "profiles/{steamID}/edit/info"
    // @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
    GetEditConfig(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) (*ProfileEditConfig, error)
}
```

### Recipe 3: Multipart File Upload
```go
// @aoni:service
// @base_url "https://steamcommunity.com"
type FileUploaderAPI interface {
    // @post "actions/FileUploader"
    // @multipart
    UploadAvatar(
        ctx context.Context,
        uploadType string,
        // @file name="avatar" filename="{filename}" content_type="{contentType}"
        file []byte,
        filename string,
        contentType string,
        mods ...aoni.RequestModifier,
    ) (*UploadResponse, error)
}
```

---

## 6. CLI & DX Reference (`cmd/aoni-gen`)

### Installation
```bash
go install github.com/lemon4ksan/aoni/cmd/aoni-gen@latest
```

### Interactive Directives Reference (like `golangci-lint linters`)
```bash
# List all directives grouped by scope with arguments and descriptions
aoni-gen list

# Filter directives by scope
aoni-gen list -scope=method
aoni-gen list -scope=service
aoni-gen list -scope=socket

# Output specification as Markdown or JSON
aoni-gen list -markdown
aoni-gen list -json

# Explain specific directive with syntax and examples
aoni-gen explain referer
aoni-gen explain form
aoni-gen explain inject
```

### Code Generation & Continuous Watching
```bash
# Scan and compile all packages recursively
aoni-gen ./...

# Validate contracts without generating code
aoni-gen check ./...

# Watch mode for instantaneous recompilation on save in IDE
aoni-gen -watch ./...

# Compile a specific file
aoni-gen -file=pkg/market/market.go
```

### Go Generate Integration
Include this comment in any file defining `@aoni:service` interfaces:
```go
//go:generate go run github.com/lemon4ksan/aoni/cmd/aoni-gen -file=$GOFILE
```
Then run:
```bash
go generate ./...
```

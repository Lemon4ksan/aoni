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
│ SERVICE SCOPE (@aoni:service, @base_url, @engine, @type_map)           │
│                                                                        │
│   ┌──────────────────────────────────────────────────────────────────┐ │
│   │ METHOD SCOPE (@get, @post, @sign, @idempotent, @coalesce)        │ │
│   │                                                                  │ │
│   │   ┌────────────────────────────────────────────────────────────┐ │ │
│   │   │ PARAMETER SCOPE (@query, @header, @format, @file, @part)   │ │ │
│   │   └────────────────────────────────────────────────────────────┘ │ │
│   └──────────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│ DTO & STRUCT SCOPE (@aoni:dto, @tuple)                                 │
└────────────────────────────────────────────────────────────────────────┘
```

## 4. Directive Reference

### 4.1. Service-Level Directives

Applied to interface type declarations:

| Directive | Arguments | Description |
| :--- | :--- | :--- |
| `@aoni:service` | `name="ClientName"` | Marks interface for client code generation. |
| `@base_url` | `"https://api.domain.com/v1"` | Sets the service default base URL. |
| `@engine` | `fast` \| `std` \| `custom type="pkg.Requester" required` | Selects underlying engine (`fast.NewClient()`, standard client, or strictly required custom requester interface). |
| `@timeout` | `"5s"`, `"500ms"` | Sets service-wide default request timeout. |
| `@type_map` | `<GoType> -> <FormatStrategy>` | Sets package-wide formatting strategy for custom types (e.g. `time.Time -> unix_s`). |

### 4.2. Method-Level Directives

Applied to interface method signatures:

#### HTTP Method & Route
| Directive | Value / Path | Description |
| :--- | :--- | :--- |
| `@get` | `"/path/{param}"` | HTTP GET request. |
| `@post` | `"/path"` | HTTP POST request. |
| `@put` | `"/path/{id}"` | HTTP PUT request. |
| `@delete` | `"/path/{id}"` | HTTP DELETE request. |
| `@patch` | `"/path/{id}"` | HTTP PATCH request. |
| `@head` | `"/path"` | HTTP HEAD request. |

#### Browser Emulation, Headers & Injection
| Directive | Syntax / Arguments | Description |
| :--- | :--- | :--- |
| `@referer` | Template: `"path/{param:escape}"`<br>Keyword: `:origin` \| `:page` \| `:parent` \| `:self` | Formats dynamic `Referer` header directly on a `[128]byte` stack buffer (`0 B/op`). Automatically prepends base URL and supports string transforms (`:escape`, `:query`, `:lower`, `:upper`). |
| `@preset` | `:xhr` \| `:cors` \| `:navigate` | Injects standard Chromium / browser header presets (`X-Requested-With`, `Sec-Fetch-*`, `Accept`). |
| `@inject` | `field="sessionid" from="SessionID"`<br>`header="X-CSRF-Token" from="CSRF"`<br>`query="api_key" from="APIKey"` | Zero-cost interface assertion on requester to inject dynamic session tokens, CSRF keys, or secrets into form bodies, headers, or query strings without domain coupling. |

#### Payload & Serialization Mode
| Directive | Arguments | Description |
| :--- | :--- | :--- |
| `@json` | — | Request body is serialized as JSON (default for POST/PUT). |
| `@form` | — | Request parameters are encoded as `application/x-www-form-urlencoded`. |
| `@multipart` | — | Request is encoded as `multipart/form-data`. |
| `@proto` | — | Request/response are serialized via Google Protocol Buffers. |
| `@grpc-web` | — | gRPC-Web protocol with 5-byte framing and trailer validation. |
| `@raw` | — | Raw binary stream payload (`io.Reader` / `[]byte`). |
| `@decoder` | `customDecoderFunc` | Custom response body decoder function. |
| `@encoder` | `customEncoderFunc` | Custom request body encoder function. |
| `@call` | `customPkg.Func` | Escape hatch: invokes user-defined generic dispatcher function `func[T](ctx, requester, path, ...mods) (*T, error)`. |

#### Enterprise & Network Reliability
| Directive | Arguments | Description |
| :--- | :--- | :--- |
| `@idempotent` | — | Injects time-ordered UUIDv7 into `Idempotency-Key` header (0 B/op). |
| `@sign` | `algo="hmac_sha256"` `key_env="SECRET"` `header="X-Signature"` | Injects cryptographic HMAC signature and timestamp header. |
| `@coalesce` | — | In-flight request deduplication (Singleflight) across concurrent goroutines. |
| `@etag` | — | RFC 9111 conditional caching (`If-None-Match` + 304 body reconstruction). |
| `@timeout` | `"2s"` | Overrides request timeout for this method. |
| `@cache_ttl` | `"30s"` | Configures in-memory response cache TTL. |
| `@expect` | `status=200,201` | Validates HTTP response status codes, returning error on mismatch. |
| `@unwrap` | `"data"` | Unwraps single JSON field from response envelope. |

#### Web Scraping (`@extract`)
| Strategy | Syntax | Description |
| :--- | :--- | :--- |
| **Regex** | `@extract regex="<div id='val'>(.*?)</div>"` | Extracts regex capture group directly into JSON unmarshaler. |
| **Between** | `@extract between="data-config=\"" and="\""` | Zero-allocation byte scan (`bytes.Index`) for substring extraction. |
| **Prefix/Suffix** | `@extract prefix="var conf = " suffix=";"` | Alias for between/and extraction. |
| **HTML Token** | `@extract css="#profile_config" attr="data-json"` | Fast streaming HTML tokenizer attribute extractor. |
| **Custom** | `@extract custom=MyExtractorFunc` | Calls user-defined extractor `func([]byte) (T, error)`. |

### 4.3. Parameter-Level Directives

Applied directly above or trailing parameter definitions:

| Directive | Arguments | Description |
| :--- | :--- | :--- |
| `@query` | `"wire_name"` | Binds parameter to URL query parameter. |
| `@header` | `"X-Custom-Header"` | Binds parameter to HTTP request header. |
| `@cookie` | `"session_id"` | Injects parameter into HTTP `Cookie` header. |
| `@field` | `"form_field_name"` | Binds parameter to Form URL-Encoded field. |
| `@part` | `"part_name"` | Binds parameter to Multipart form field. |
| `@file` | `name="avatar" filename="{fn}" content_type="{ct}"` | Binds `io.Reader` / `[]byte` as a multipart file upload. |
| `@format` | Strategy (see §5) | Formatting rules for dates, slices, booleans, and nested JSON. |

### 4.4. DTO & Struct-Level Directives

Applied to request/response struct declarations:

| Directive | Arguments | Description |
| :--- | :--- | :--- |
| `@aoni:dto` | `casing=snake_case` \| `camelCase` \| `kebab-case` | Generates 0-alloc `AppendFormData`, `AppendQuery`, `EncodeValues` methods. |
| `@tuple` | — | Models fixed-length JSON arrays (e.g. `[timestamp, open, high, low, close]`). |

## 5. Formatting Strategies (`@format`)

### Date & Time (`time.Time`)
- `@format unix_s`: Unix timestamp in seconds (`strconv.AppendInt(buf, t.Unix(), 10)`).
- `@format unix_ms`: Unix timestamp in milliseconds (`strconv.AppendInt(buf, t.UnixMilli(), 10)`).
- `@format rfc3339`: RFC 3339 timestamp string (`2026-08-14T21:00:00Z`).
- `@format layout="2006-01-02"`: Custom date layout format.

### Slices & Collections (`[]T`)
- `@format comma`: Comma-separated list (`tag1,tag2,tag3`).
- `@format pipe`: Pipe-separated list (`1|2|3`).
- `@format space`: Space-separated list (`read write admin`).
- `@format bracket`: PHP/Rails array format (`tags[]=a&tags[]=b`).

### Booleans (`bool`)
- `@format bool_int`: Emits 1 (true) or 0 (false).
- `@format flag`: Emits key only if true (`compact` without `=true`).

### JSON-in-Form (`struct`)
- `@format json_string`: Marshals struct to JSON and URL-escapes into form field.

## 6. Copy-Paste Real-World Recipes

### Recipe 1: Standard CRUD REST Service
```go
// @aoni:service
// @base_url "https://api.github.com"
type GitHubAPI interface {
    // @get "users/{username}"
    GetUser(ctx context.Context, username string, mods ...aoni.RequestModifier) (*User, error)

    // @get "users/{username}/repos"
    ListRepos(
        ctx context.Context,
        username string,
        // @query "sort"
        sort string,
        // @query "per_page"
        perPage int,
        mods ...aoni.RequestModifier,
    ) ([]Repo, error)

    // @post "user/repos"
    CreateRepo(ctx context.Context, req *CreateRepoRequest, mods ...aoni.RequestModifier) (*Repo, error)

    // @delete "repos/{owner}/{repo}"
    // @expect status=204
    DeleteRepo(ctx context.Context, owner string, repo string, mods ...aoni.RequestModifier) error
}

// @aoni:dto casing=snake_case
type CreateRepoRequest struct {
    Name        string
    Description string
    Private     bool
}
```

### Recipe 2: Crypto Exchange API (HMAC Signature + Idempotency + Singleflight)
```go
// @aoni:service
// @base_url "https://api.binance.com"
// @engine fast
type BinanceAPI interface {
    // 1. High-throughput market data with Singleflight request coalescing
    // @get "api/v3/ticker/price"
    // @coalesce
    GetPrice(ctx context.Context, symbol string, mods ...aoni.RequestModifier) (*PriceTicker, error)

    // 2. Financial transaction with zero-alloc Idempotency Key & HMAC signing
    // @post "api/v3/order"
    // @idempotent
    // @sign hmac_sha256 key_env="BINANCE_SECRET"
    CreateOrder(ctx context.Context, req *NewOrderRequest, mods ...aoni.RequestModifier) (*OrderResult, error)
}

// @aoni:dto casing=snake_case
type NewOrderRequest struct {
    Symbol   string
    Side     string
    Type     string
    Quantity float64
    Price    float64
}

// @aoni:dto casing=snake_case
type OrderResult struct {
    OrderID uint64
    Status  string
}
```

### Recipe 3: Browser Emulation & Web Scraping (Steam Community Market)
```go
// @aoni:service
// @engine custom type="community.Requester" required
// @base_url "https://steamcommunity.com"
type SteamMarketAPI interface {
    // 1. Authenticated JSON form submission with XHR preset & dynamic Referer template
    // @post "market/createbuyorder"
    // @form
    // @preset :xhr
    // @inject field="sessionid" from="SessionID"
    // @referer "market/listings/{appID}/{marketHashName:escape}"
    CreateBuyOrder(
        ctx context.Context,
        // @field "appid"
        appID uint32,
        // @field "market_hash_name"
        marketHashName string,
        // @field "price_total"
        priceTotal string,
        // @field "quantity"
        quantity int,
        mods ...aoni.RequestModifier,
    ) (*CreateBuyOrderResponse, error)

    // 2. Scrape dynamic configuration from HTML DOM with canonical page referer
    // @get "profiles/{steamID}/edit/info"
    // @preset :navigate
    // @referer :origin
    // @extract css="#profile_edit_config" attr="data-profile-edit"
    GetEditConfig(ctx context.Context, steamID string, mods ...aoni.RequestModifier) (*ProfileConfig, error)

    // 3. Multipart Avatar Upload with Dynamic File Metadata & Injected Session ID
    // @post "actions/FileUploader"
    // @multipart
    // @inject field="sessionid" from="SessionID"
    // @referer "profiles/{steamID}/edit"
    UploadAvatar(
        ctx context.Context,
        steamID string,
        // @part "type"
        uploadType string,
        // @file name="avatar" filename="{filename}" content_type="{contentType}"
        file []byte,
        filename string,
        contentType string,
        mods ...aoni.RequestModifier,
    ) (*UploadResponse, error)
}
```

### Recipe 4: Tuple Data Serialization (Candlestick / OHLCV Data)
```go
// @tuple
type Candlestick struct {
    OpenTime  int64   // index 0
    Open      string  // index 1
    High      string  // index 2
    Low       string  // index 3
    Close     string  // index 4
    Volume    string  // index 5
    CloseTime int64   // index 6
}
```

## 7. CLI & DX Reference (`cmd/aoni-gen`)

### Installation
```bash
go install github.com/lemon4ksan/aoni/cmd/aoni-gen@latest
```

### Usage
```bash
# Scan and compile all packages recursively
aoni-gen ./...

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

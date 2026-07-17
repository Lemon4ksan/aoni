<div align="center">

# ❄️ aoni

### The Ice-Cold Resilience Engine for Go HTTP & Real-Time Networks

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/aoni?style=flat-square)](https://goreportcard.com/report/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)

> _"In networks, chaos is the default. Let aoni be your ice-cold anchor."_

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

### Why Aoni?

When integrating with unstable APIs, scraping, or working with complex proxy networks in Go, the standard `net/http` client often requires significant boilerplate to handle real-world challenges like proxy rotation, rate limits, legacy charsets, or TLS fingerprinting. 

`aoni` bridges this gap. It models HTTP requests as pipeline flows processed by declarative **RequestModifiers** and standard Go **Middlewares**, leveraging generics for type-safe response decoding. It remains unwavering under network load, just like the blue oni.

```shell
go get github.com/lemon4ksan/aoni
```

## 🎯 When to Use Aoni vs. Standard Clients (e.g., Resty)

`aoni` is not designed to replace `net/http` or lightweight wrappers like `resty` for standard, internal corporate microservices where raw, flat throughput over direct, reliable cloud connections is the only concern.

* **Choose `net/http` / `resty`** for: Internal microservices, direct cloud API integrations (AWS/S3, Stripe, Twilio), and standard high-throughput REST APIs where you fully control the destination server and the network environment.
* **Choose `aoni`** for: Deep-packet inspection (DPI) evasion, scraping/crawling targets behind aggressive firewalls (Cloudflare, Akamai, Imperva), rotating unstable proxy networks with sticky sessions, and real-time WebSockets over HTTP/2. It is your **tactical off-road armor** for uncooperative and chaotic network environments.

## 🌀 The Pipeline Philosophy

In `aoni`, a request is not a static object-it is a fluid stream processed in four distinct, highly optimized phases:

```mermaid
flowchart LR
    %% Styling
    classDef ice fill:#f0f8ff,stroke:#00a3e0,stroke-width:1.5px,color:#003366;
    linkStyle default stroke:#00a3e0,stroke-width:2px;
    
    p1["<b>[ RequestModifiers ]</b><br><i>Decorate req</i><br>━━━━━━━━━━━━━━━━━━<br>• Headers<br>• URL Variables<br>• Request Body"]:::ice
    p2["<b>[ HTTP Middlewares ]</b><br><i>Intercept & Wrap</i><br>━━━━━━━━━━━━━━━━━━<br>• Rate Limiter<br>• CircuitBreaker<br>• RetryEngine"]:::ice
    p3["<b>[ Transport Layer ]</b><br><i>Execute</i><br>━━━━━━━━━━━━━━━━━━<br>• HappyEyeballs<br>• ProxyRotator<br>• LoadBalancer"]:::ice
    p4["<b>[ Generic Decoders ]</b><br><i>Extract output</i><br>━━━━━━━━━━━━━━━━━━<br>• Auto-UTF8<br>• JSON/XML/YAML<br>• Error Models"]:::ice
    
    p1 --> p2 --> p3 --> p4
```

## ⚡ The Contrast: Standard Library vs. Aoni

To make a JSON request through a resilient proxy pool with retries and custom error parsing, standard Go requires manual loop management, type casting, and verbose transport setup. 

Here is how the two approaches compare:

<table width="100%">
<tr>
<th width="50%">Standard <code>net/http</code> (Manual Setup)</th>
<th width="50%">Using <code>aoni</code> (Declarative & Resilient)</th>
</tr>
<tr>
<td valign="top">

```go
// 🛑 Verbose, unsafe state handling
transport := &http.Transport{
    Proxy: http.ProxyURL(proxyURL),
}
client := &http.Client{Transport: transport}

var lastErr error
for i := 0; i < 3; i++ {
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    resp, err := client.Do(req)
    if err != nil {
        lastErr = err
        time.Sleep(backoff)
        continue
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        // Must manually decode error schema...
    }
    
    // Must manually decode JSON...
    err = json.NewDecoder(resp.Body).Decode(&user)
    break
}
```

</td>
<td valign="top">

```go
// ❄️ Clean, immutable, pipeline-driven flow
client := aoni.NewClient(nil,
    aoni.WithClientBaseURL("https://api.example.com"),
    aoni.WithClientTimeout(15*time.Second),
    aoni.WithClientTLSFingerprint(aoni.BrowserChrome),
)

// 1. Get structured JSON automatically
user, err := aoni.GetTo[User](ctx, client, "/users/{id}",
    aoni.WithVar("id", 123),
    aoni.WithErrorModel(&apiErr),
)

// 2. Or perform raw HTTP requests directly via client convenience methods
resp, err := client.Get(ctx, "/raw-data")
```

</td>
</tr>
</table>

## 🧱 Custom Middlewares & Pipeline Customization

`aoni` uses a modular pipeline architecture. The client's core request execution is composed of 12 decoupled, standard Go middlewares wrapped around the raw HTTP engine. 

### Custom Pipeline Wrapper
You can customize, reorder, or completely replace the default middleware chain using `WithClientPipelineWrapper`:

```go
client := aoni.NewClient(nil,
	aoni.WithClientPipelineWrapper(func(c *aoni.Client, engine aoni.HTTPDoer) aoni.HTTPDoer {
		// Build your own custom pipeline, injecting custom logic or reordering stages
		return aoni.Chain(engine,
			aoni.InspectorMiddleware(c.Inspector()),
			aoni.ResponseValidationMiddleware(),
		)
	}),
)
```

### Specialized Built-In Middlewares
In addition to internal core middlewares, `aoni` provides 6 specialized middlewares for advanced evasion, resilience, and auditing:

1. **`UserAgentAndHintsRotationMiddleware`** (Stealth & Evasion)
   Rotates both the `User-Agent` and matching `Sec-CH-UA-*` client hints consistently on every request to prevent browser fingerprint mismatches.
2. **`DPIJitterMiddleware`** (Stealth & Evasion)
   Introduces a randomized delay (jitter) between writing request headers and body, confusing Deep Packet Inspection (DPI) timing analysis.
3. **`ProxyFailoverMiddleware`** (Resilience)
   Transparently switches proxy servers from a pool and retries request execution if the primary proxy fails with a connection error or `502`/`503` status.
4. **`CacheMiddleware`** (Performance & Resilience)
   RFC-7234 compliant HTTP caching for `GET` requests. Saves requests to a thread-safe `InMemoryCacheStore` or custom `CacheStore` backend.
5. **`SensitiveDataRedactorMiddleware`** (Security & Auditing)
   Redacts sensitive headers (e.g. `Authorization`, `Cookie`) in context before they are passed to the `TrafficInspector` or debug loggers, replacing values with `[REDACTED]`.
6. **`HARGeneratorMiddleware`** (Auditing)
   Captures full request-response exchanges (including timings and response bodies) and records them to a `HARGenerator` for exporting standard HTTP Archive (HAR) JSON logs.

## 📊 Feature Matrix

This matrix shows where `aoni` focuses its design compared to Go's default capabilities and generic wrappers:

| Feature / Capability | Go `net/http` | Standard Wrapper (e.g., Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Generics-first Decoding** | ✗ (Manual) | ✗ (Interface-based) | **✓ (Type-safe `[T]`)** |
| **Parallel "Happy Eyeballs" Dialing** | ⚠️ (Basic) | ✗ | **✓ (RFC 8305)** |
| **Active Circuit Breaking** | ✗ | ✗ | **✓ (Native Middleware)** |
| **Polite `Retry-After` Parsing** | ✗ | ✗ | **✓ (Delta-sec & RFC1123)** |
| **Non-UTF8 Charset Translation** | ✗ | ✗ | **✓ (Automatic)** |
| **TLS Evasion (JA3/JA4)** | ✗ | ✗ | **✓ (via `uTLS` & Handshake)** |
| **JA4+ Fingerprinting** | ✗ | ✗ | **✓ (TLS & HTTP, pure Go)** |
| **Sub-millisecond Tracing** | ⚠️ (Verbose) | ✗ | **✓ (Single-modifier)** |
| **Structured Response Unwrapping** | ✗ | ✗ | **✓ (`WithBaseResponse`)** |
| **Socket.IO / Engine.IO v4 Client** | ✗ | ✗ | **✓ (Complete v5 Spec)** |
| **Per-Request Overrides** | ✗ (Manual transport) | ✗ (Requires client clone) | **✓ (Context Accessors)** |

## 🛠️ Two-Tier Configuration (Overrides)

`aoni` features a powerful two-tier configuration model allowing you to establish client-wide sensible defaults while reserving the ability to fine-tune transport behavior for individual requests.

```
Request-level Option -> Client-level Default -> System Environment / Transport Default
```

* **Client-Level (Config once):** Set options globally. Every `With*` call on a `*Client` returns an immutable cloned client, preserving thread safety.
* **Request-Level (Override once):** Tweak behavior for a single execution using context-bound `RequestModifier` options.

### Override Support Matrix

| Option | Client-Level Default | Per-Request Modifier | Behavior / Resolution Priority |
| :--- | :--- | :--- | :--- |
| **Proxy Routing** | `WithClientProxy(url)` | `aoni.WithProxyOverride(url)` | Per-request wins → Client-level default → System environment (`HTTP_PROXY`) |
| **TLS Bypassing** | `WithClientInsecureSkipVerify()` | `aoni.WithInsecureSkipVerify()` | Per-request wins → Client-level setting → Standard TLS verification |
| **TCP Connect Jitter** | `WithClientTCPDelay(min, max)` | `aoni.WithTCPDelay(min, max)` | Per-request wins → Client-level default → No delay |
| **Response Validation** | `WithClientResponseValidator(fn)` | `aoni.WithResponseValidator(fn)` | Both run sequentially. Per-request error overrides client-level error. |
| **Retry Policies** | *Automatic in middleware* | `aoni.WithRetryPolicy(override)` | Per-request `RetryOverride` settings take precedence over global middleware settings. |
| **Cache Duration** | *Decided by middleware* | `aoni.WithCacheTTL(duration)` | Passed via context to be retrieved by a caching middleware layer. |
| **Request Metadata** | *N/A* | `aoni.WithConnMetadata(key, val)` | Thread-safe connection-bound values readable in transports or logging hooks. |

## 🍳 Cookbook: Common Resiliency Recipes

Instead of dry features, here is how you solve common, frustrating networking challenges with `aoni`.

### 1. Transparent Proxy Rotation with Sticky Sessions
* **The Problem:** You need to rotate proxies to distribute load, but specific user requests must land on the exact same proxy address to preserve their active session state.
* **The Ice-Cold Solution:**

```go
p1, _ := aoni.NewProxyClient(aoni.ProxyConfig{ProxyURL: "http://proxy1.local"})
p2, _ := aoni.NewProxyClient(aoni.ProxyConfig{ProxyURL: "http://proxy2.local"})

rotator, _ := aoni.NewProxyRotator(aoni.ProxyRotatorConfig{
    MaxFails:   3,
    RetryAfter: 30 * time.Second,
}, p1, p2)

// Lock proxy selection dynamically based on the request's session cookie
stickyRotator := rotator.WithStickySessions(func(req *http.Request) string {
    if c, err := req.Cookie("sessionid"); err == nil {
        return c.Value
    }
    return ""
})

client := aoni.NewClient(aoni.Chain(stickyRotator, rateLimiter))
```

### 2. Fine-Tuning Transport Per-Request (The Overrides Pattern)
* **The Problem:** You have a configured client, but a specific target requires bypassing certificate verification, routing through a premium proxy, and introducing connection delay to evade detection.
* **The Ice-Cold Solution:** Pass the modifiers to the specific request. The rest of the client's operations remain untouched and secure.

```go
resp, err := client.Get(ctx, "/premium-endpoint",
    aoni.WithProxyOverride("http://premium-proxy.com:8080"),
    aoni.WithInsecureSkipVerify(),
    aoni.WithTCPDelay(100*time.Millisecond, 500*time.Millisecond),
)
```

### 3. Mitigating Long-Tail Latency via Hedging
* **The Problem:** Unstable proxies or overloaded servers occasionally freeze, delaying your entire execution queue.
* **The Ice-Cold Solution:** If the primary request stalls and doesn't return headers in 150ms, a backup request is dispatched in parallel, returning whichever finishes first.

```go
data, err := aoni.GetTo[Data](ctx, aoni.NewClient(hedgedClient), "/data", aoni.WithHedging(10*time.Millisecond))
```

### 4. Automatic Legacy Charset Translation
* **The Problem:** Legacy regional APIs or crawled websites return text encoded in old charsets (e.g., Cyrillic or Asian legacy encodings), resulting in garbled characters during JSON unmarshaling.
* **The Ice-Cold Solution:** `aoni` detects the encoding on-the-fly from the headers and transparently translates the stream to standard UTF-8 before passing it to any decoder.

```go
manifest, err := aoni.GetTo[Manifest](ctx, client, "/legacy-manifest",
    aoni.WithDownloadProgress(func(current, total int64) {
        fmt.Printf("Downloaded %d of %d bytes\n", current, total)
    }),
)
```

### 5. Modern WAF Evasion & JA4 Fingerprinting
* **The Problem:** Modern Web Application Firewalls (WAFs like Cloudflare or Akamai) block automated requests based on TLS ClientHello fingerprints (JA3/JA4) and HTTP header ordering (JA4H).
* **The Ice-Cold Solution:** `aoni` natively emulates modern browser TLS handshakes using `uTLS` and automatically aligns headers to generate a clean, completely browser-compliant fingerprint. The built-in [`ja4`](ja4/) subpackage provides pure-Go JA4/JA4H computation.

```go
info := &aoni.TraceInfo{}

client := aoni.NewClient(nil,
    aoni.WithClientTLSFingerprint(aoni.BrowserChrome), // Spoofs TLS ClientHello
    aoni.WithClientJA4Callback(func(r ja4.JA4Report) {
        fmt.Println("Active TLS Handshake JA4:", r.JA4)
    }),
)

user, err := aoni.GetTo[User](ctx, client, "/profile",
    aoni.Trace(info),
    aoni.TraceJA4(info), // Traces both TLS (JA4) and HTTP (JA4H) fingerprints
)

fmt.Println("Handshake TLS JA4:", info.JA4.JA4)   // "t13d1516h2_8daaf6152771_e5627efa2ab1"
fmt.Println("Request HTTP JA4H:", info.JA4.JA4H)  // "ge11nn03enus_9ed1ff1f7b03_cd8dafe26982"
```

### 6. Bulletproof, Real-Time Socket.IO v5 / Engine.IO v4 Streaming
* **The Problem:** Real-time web sockets on protected servers get blocked during handshake due to standard Go TLS fingerprints, or silent TCP disconnects go unnoticed.
* **The Ice-Cold Solution:** `aoni` establishes fully authenticated, JA4-spoofed, proxy-routed Socket.IO v5 sessions over standard WebSockets or stealthy HTTP/2 Extended CONNECT tunnels. It includes automatic, jittered backoff reconnection and ping-timeout heartbeats natively.

```go
import "github.com/lemon4ksan/aoni/socketio"

cfg := socketio.SocketIOConfig{
    Reconnection: true,
    Namespace:    "/realtime-prices",
    Auth:         map[string]string{"token": "my-secure-token"},
}

// Automatically inherits proxy rotators, DoT, JA4, and SSRF guards from the client!
sio, err := socketio.DialSocketIO(ctx, client, "wss://api.pricedb.io", cfg)
if err != nil {
    log.Fatal(err)
}

sio.On("price_update", func(args []json.RawMessage) {
    var price Price
    _ = json.Unmarshal(args[0], &price)
    fmt.Printf("Live Price: %s -> %.2f\n", price.SKU, price.Value)
})
```

### 7. Diagnostic Tracing & Offline Debugging
* **The Problem:** Tracking network bottlenecks across proxies is difficult, and recreating failing requests in terminal for manual verification takes time.
* **The Ice-Cold Solution:**

```go
var trace aoni.TraceInfo

aoni.GetTo[User](ctx, client, "/debug",
    aoni.Trace(&trace), // Detailed DNS, TCP, and TLS metrics
    aoni.AsCurl(),      // Prints equivalent executable curl command to stderr
)

fmt.Printf("DNS: %s | TCP Connect: %s | TLS Handshake: %s | TTFB: %s\n",
    trace.DNSLookup, trace.TCPConn, trace.TLSHandshake, trace.ServerProcessing)
```

### 8. Structured API Response Unwrapping with `WithBaseResponse`
* **The Problem:** Many APIs wrap successful data in an envelope object like `{"success":true,"data":{...}}` or `{"status":"ok","result":{...}}`. Manually unwrapping these envelopes, checking success flags, and extracting errors is repetitive boilerplate.
* **The Ice-Cold Solution:** Implement the `BaseResponse` interface (`IsSuccess`, `Error`, `SetData`, `UnmarshalJSON`) and configure the client with `WithBaseResponse`. The decoder automatically decodes into your wrapper, checks the success flag, extracts errors, and unwraps the inner payload - all in one pass.

```go
// 1. Define your envelope wrapper
type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"` // Capture raw JSON
	target  any
}

func (r *apiResponse) IsSuccess() bool            { return r.Success }
func (r *apiResponse) Error() error               { return errors.New(r.Message) }
func (r *apiResponse) SetData(data any)           { r.target = data }

func (r *apiResponse) UnmarshalJSON(data []byte) error {
	// 1. Avoid recursion with Alias
	type Alias apiResponse
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	r.Success = aux.Success
	r.Message = aux.Message
	r.Data = aux.Data // Save raw "data"

	// 2. Decode the "data" into our target object
	if r.target != nil && len(r.Data) > 0 {
		return json.Unmarshal(r.Data, r.target)
	}
	return nil
}

// 2. Configure the client - every JSON request unwraps through this envelope
client := aoni.NewClient(nil,
	aoni.WithClientBaseURL("https://api.example.com"),
	aoni.WithClientBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }),
)

// 3. Use it - the decoder handles envelope unwrapping automatically
user, err := aoni.GetTo[User](ctx, client, "/users/1")
// If API returns {"success":false,"message":"not found"}, err is non-nil
// If API returns {"success":true,"data":{"name":"Alice"}}, user.Name == "Alice"
```

## 📦 Subpackages

| Package | Import path | Description |
| :--- | :--- | :--- |
| **ws** | `github.com/lemon4ksan/aoni/ws` | WebSocket dialing over uTLS and HTTP/2 Extended CONNECT (RFC 8441) |
| **socketio** | `github.com/lemon4ksan/aoni/socketio` | Socket.IO v5 / Engine.IO v4 client with reconnection and namespaces |
| **inspector** | `github.com/lemon4ksan/aoni/inspector` | Traffic inspector — capture, replay, and export HTTP exchanges |
| **ja4** | `github.com/lemon4ksan/aoni/ja4` | Pure-Go JA4 / JA4H fingerprint computation |
| **p0f** | `github.com/lemon4ksan/aoni/p0f` | TCP/IP stack fingerprint signatures (TTL, MSS, window size) |
| **profiles** | `github.com/lemon4ksan/aoni/profiles` | Pre-built browser TLS + HTTP/2 profiles (Chrome, Firefox) |

## 🎨 Memory & Resource Footprint

While standard clients focus only on raw speed, `aoni` is engineered to protect your host application's resources when scaling to thousands of concurrent worker loops:

* **Static Heap Footprint:** Maintains an ultra-lean runtime profile, consuming roughly **~1.2 MB** of live heap memory in idle states.
* **Sync.Pool Recycled Buffers:** Utilizes pooled memory slices for body streaming, JSON parsing, and multipart encoding to keep GC overhead and "GC pauses" to a minimum.
* **Leak Defense (Finalizers):** Leverages `runtime.SetFinalizer` on critical network responses to automatically release unclosed connections and warn you about resource leaks before file descriptors are exhausted.
* **Response Bomb Protection:** Enforces strict payload reading limits (e.g. 10MB) via `io.LimitReader` on incoming responses to prevent out-of-memory (OOM) crashes from malicious or unexpectedly massive responses.

## ⚖️ Legal & License

This project is licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for full details.

<div align="center">
  <sub>Keep a cold head, stay unyielding. Just like the blue oni.</sub>
</div>

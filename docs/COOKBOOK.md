## 🍳 Cookbook: Common Resiliency Recipes

Here is how you solve common, frustrating networking challenges with `aoni`.

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
    mod.WithProxyOverride("http://premium-proxy.com:8080"),
    mod.WithInsecureSkipVerify(),
    mod.WithTCPDelay(100*time.Millisecond, 500*time.Millisecond),
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
    mod.WithDownloadProgress(func(current, total int64) {
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
    option.WithTLSFingerprint(aoni.BrowserChrome), // Spoofs TLS ClientHello
    option.WithJA4Callback(func(r ja4.JA4Report) {
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
		option.WithBaseURL("https://api.example.com"),
		option.WithBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }),
)

// 3. Use it - the decoder handles envelope unwrapping automatically
user, err := aoni.GetTo[User](ctx, client, "/users/1")
// If API returns {"success":false,"message":"not found"}, err is non-nil
// If API returns {"success":true,"data":{"name":"Alice"}}, user.Name == "Alice"
```

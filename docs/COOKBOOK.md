## 🍳 Cookbook: Common Resiliency Recipes

Here is how you solve frustrating networking challenges with `aoni`.

### 1. Transparent Proxy Rotation with Sticky Sessions
* **The Problem:** You need to rotate proxies across a pool to distribute load, but requests belonging to the same user session must land on the exact same proxy exit node.
* **The Ice-Cold Solution:** Use `netutil/proxy.Rotator` with a custom sticky session key extractor based on cookies, headers, or user IDs.

```go
import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/request"
)

// Instantiate a resilient, health-checked proxy pool
rotator, err := proxy.NewRotatorFromStrings(proxy.RotatorConfig{
	MaxFails:   3,
	RetryAfter: 30 * time.Second,
}, "http://proxy1.local:8080", "http://proxy2.local:8080")
if err != nil {
	log.Fatal(err)
}

// Bind session affinity dynamically to the 'sessionid' cookie
stickyRotator := rotator.WithStickySessions(proxy.StickyKeyFromCookie("sessionid"))

// Wrap the rotator into an immutable aoni Client
client := aoni.NewClient(stickyRotator)

// Requests carrying the same 'sessionid' will consistently route to the same proxy
user, err := request.GetTo[User](ctx, client, "/profile")
```

### 2. Fine-Tuning Transport Per-Request (The Overrides Pattern)
* **The Problem:** You have a globally configured client, but one specific target requires bypassing TLS verification, routing through a dedicated premium proxy, and introducing random TCP dial delay to evade rate limiters.
* **The Ice-Cold Solution:** Pass per-request modifiers (`mod.With...`). The client's global state remains untouched and secure for all other goroutines.

```go
import (
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

resp, err := client.Request(ctx, http.MethodGet, "/vip-endpoint",
	mod.WithProxyOverride("http://premium-proxy.local:9090"),
	mod.WithInsecureSkipVerify(),
	mod.WithTCPDelay(100*time.Millisecond, 500*time.Millisecond),
)
```

### 3. Mitigating Long-Tail Latency via Request Hedging
* **The Problem:** Unstable proxies or overloaded servers occasionally stall, introducing high latency spikes that block your execution queue.
* **The Ice-Cold Solution:** Enable hedging. If the primary request stalls and fails to return response headers within a specific delay, `aoni` dispatches a parallel secondary attempt and yields whichever completes first.

```go
import "github.com/lemon4ksan/aoni/option"

// Static 150ms hedging delay
client := aoni.NewClient(nil, option.WithHedging(150*time.Millisecond))

// Or Dynamic Hedging: automatically calculates delay based on observed p95 RTT
clientDynamic := aoni.NewClient(nil, option.WithDynamicHedging(nil))

user, err := request.GetTo[User](ctx, client, "/fast-path")
```

### 4. Automatic Legacy Charset Translation
* **The Problem:** Legacy APIs or regional websites return responses encoded in non-UTF-8 formats (e.g. Windows-1251, Shift-JIS, ISO-8859-1), resulting in garbled text during JSON/XML decoding.
* **The Ice-Cold Solution:** `aoni` inspects incoming `Content-Type` headers on the fly and transparently transcodes the stream into standard UTF-8 before decoding.

```go
manifest, err := request.GetTo[Manifest](ctx, client, "/legacy-manifest",
	mod.WithDownloadProgress(func(current, total int64) {
		fmt.Printf("Downloaded %d of %d bytes\n", current, total)
	}),
)
```

### 5. WAF Evasion & Pure-Go JA4/JA4H Fingerprinting
* **The Problem:** Web Application Firewalls (Cloudflare, Akamai, Imperva) block automated requests based on TLS ClientHello fingerprints (JA3/JA4) and HTTP header ordering (JA4H).
* **The Ice-Cold Solution:** `aoni` emulates modern browser TLS handshakes via `uTLS` and aligns header sequences. Use `TraceJA4` to inspect wire signatures in real time.

```go
import (
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

info := &telemetry.TraceInfo{}

client := aoni.NewClient(nil,
	option.WithTLSFingerprint(aoni.BrowserChrome),
	option.WithJA4Callback(func(r ja4.Report) {
		fmt.Println("Handshake TLS JA4:", r.JA4)
	}),
)

user, err := request.GetTo[User](ctx, client, "/profile",
	aoni.Trace(info),
	aoni.TraceJA4(info),
)

fmt.Println("TLS Fingerprint (JA4): ", info.JA4.JA4)  // "t13d1516h2_8daaf6152771_e5627efa2ab1"
fmt.Println("HTTP Fingerprint (JA4H):", info.JA4.JA4H) // "ge11nn03enus_9ed1ff1f7b03_cd8dafe26982"
```

### 6. Stealth Real-Time Socket.IO v5 / Engine.IO v4 Streaming
* **The Problem:** Real-time WebSockets on protected servers get blocked during handshake due to standard Go TLS signatures, or drop silently without heartbeat detection.
* **The Ice-Cold Solution:** `aoni` establishes Socket.IO v5 sessions over WebSockets or HTTP/2 Extended CONNECT tunnels while inheriting the parent client's uTLS profile, proxy rotation, and ping-timeout heartbeats.

```go
import "github.com/lemon4ksan/aoni/realtime/socketio"

cfg := socketio.Config{
	Reconnection: true,
	Namespace:    "/realtime-feed",
	Auth:         map[string]string{"token": "secure-session-token"},
}

// Automatically inherits proxy rotators, uTLS browser signatures, and DNS settings
sio, err := socketio.DialSocketIO(ctx, client, "wss://api.example.com/socket.io/", cfg)
if err != nil {
	log.Fatal(err)
}
defer sio.Close()

sio.On("price_update", func(args []json.RawMessage) {
	fmt.Printf("Live Payload: %s\n", string(args[0]))
})
```

### 7. Diagnostic Tracing & Terminal Shell cURL Generation
* **The Problem:** Debugging network bottlenecks across multiple proxies is tedious, and manually reproducing failing requests in terminal takes time.
* **The Ice-Cold Solution:** Attach connection tracers and print cURL commands on demand.

```go
var trace telemetry.TraceInfo

user, err := request.GetTo[User](ctx, client, "/debug",
	aoni.Trace(&trace),    // Detailed DNS, TCP, TLS, and TTFB metrics
	mod.WithCurlDump(),    // Prints ready-to-run 'curl -X GET ...' to stderr
)

fmt.Printf("DNS: %s | TCP: %s | TLS: %s | TTFB: %s\n",
	trace.DNSLookup, trace.TCPConn, trace.TLSHandshake, trace.ServerProcessing)
```

### 8. Structured API Response Unwrapping with `WithBaseResponse`
* **The Problem:** APIs wrap responses in envelope structures like `{"success":true,"data":{...}}` or `{"status":"ok","result":{...}}`. Manually unwrapping them and checking error fields adds repetitive boilerplate.
* **The Ice-Cold Solution:** Implement the `aoni.BaseResponse` interface. `aoni` decodes into your envelope wrapper, validates status flags, and extracts the target payload in a single pass.

```go
type APIEnvelope struct {
	Status string          `json:"status"`
	Error  string          `json:"error,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	target any
}

func (e *APIEnvelope) IsSuccess() bool  { return e.Status == "success" }
func (e *APIEnvelope) Error() error     { return errors.New(e.Error) }
func (e *APIEnvelope) SetData(data any) { e.target = data }

func (e *APIEnvelope) UnmarshalJSON(b []byte) error {
	type Alias APIEnvelope
	var aux Alias
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	e.Status = aux.Status
	e.Error = aux.Error
	e.Data = aux.Data

	if e.target != nil && len(e.Data) > 0 {
		return json.Unmarshal(e.Data, e.target)
	}
	return nil
}

client := aoni.NewClient(nil,
	option.WithBaseURL("https://api.example.com"),
	option.WithBaseResponse(func() aoni.BaseResponse { return &APIEnvelope{} }),
)

// Automatically unwraps 'data' directly into User struct
user, err := request.GetTo[User](ctx, client, "/users/1")
```

### 9. Socket-Level DPI Evasion (Packet Fragmentation & CDN Padding)
* **The Problem:** Deep-packet inspection (DPI) firewalls inspect TCP segment boundaries and ClientHello sizes to block unauthorized scrapers.
* **The Ice-Cold Solution:** Split payloads into TCP chunks with micro-delays and inject randomized CDN tracing headers.

```go
import "github.com/lemon4ksan/aoni/fingerprint"

client := aoni.NewClient(nil,
	// Split TLS ClientHello into small 2-byte TCP segments with jitter
	option.WithFragmentation(aoni.FragmentConfig{
		ChunkSize: 2,
		MaxDelay:  10 * time.Millisecond,
	}),
	// Inject randomized CDN padding headers (e.g. AWS CloudFront / Cloudflare trace IDs)
	option.WithPacketPadding(fingerprint.PaddingConfig{
		MinPaddingBytes: 16,
		MaxPaddingBytes: 64,
		HeaderPool:      fingerprint.CloudflareHeaderPool,
	}),
)
```

### 10. In-Memory Unit Testing with Vortex Mocks (0 Port Overhead)
* **The Problem:** Spinning up real `httptest.Server` instances in parallel unit test suites leads to port exhaustion, slow execution, and flaky network socket timeouts.
* **The Ice-Cold Solution:** Generate in-memory mock servers with `vortex mock`. The mock routes requests directly through `fasthttputil.InmemoryListener` without opening OS network ports.

```bash
# Generate the mock server implementation
vortex mock pkg/services/user/api.go
```

```go
func TestUserAPI(t *testing.T) {
	ctx := context.Background()

	// 1. Create in-memory mock
	mockServer := user.NewUserAPIMockServer()

	// 2. Define custom response
	mockServer.OnGetUser = func(ctx context.Context, id string) (*user.UserDTO, error) {
		return &user.UserDTO{ID: id, Name: "G-Man"}, nil
	}

	// 3. Obtain client routed directly into in-memory transport
	client := mockServer.Client()

	// 4. Test service
	res, err := client.GetUser(ctx, "76561198000000000")
	require.NoError(t, err)
	require.Equal(t, "G-Man", res.Name)
}
```

### 11. Zero-Allocation Protobuf Services with `vtprotobuf`
* **The Problem:** Standard `protoc-gen-go` generates reflection-heavy serialization routines that allocate heap memory on every message encode/decode.
* **The Ice-Cold Solution:** Compile `.proto` schemas with `vortex proto` to generate high-speed `vtprotobuf` zero-allocation codecs and use generic `request.PostProtoTo[T]`.

```bash
vortex proto -src=./proto -out=./pkg/pb -import=github.com/my/project/pkg/pb
```

```go
import (
	"github.com/lemon4ksan/aoni/request"
	pb "github.com/my/project/pkg/pb"
)

// Zero-allocation binary protobuf request and response decoding
resp, err := request.PostProtoTo[pb.TradeResponse](ctx, client, "https://api.steam.com/trade", reqProto)
```


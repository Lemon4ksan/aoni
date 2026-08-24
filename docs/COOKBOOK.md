## 🍳 Cookbook: Common Resiliency Recipes

This cookbook provides battle-tested recipes for solving complex networking, proxy routing, WAF evasion, and high-concurrency challenges using `aoni`.

## 1. Transparent Proxy Rotation with Sticky Sessions

When rotating outbound requests across a large proxy pool to distribute load, requests belonging to the same logical user session must consistently route through the exact same proxy exit node to prevent session invalidation. `netutil/proxy.Rotator` provides session affinity via custom sticky key extractors based on cookies, headers, or user identifiers.

```go
import (
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/request"
)

// Instantiate a health-checked proxy pool with automatic failover
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

// Requests carrying the same 'sessionid' will consistently route to the same proxy exit node
user, err := request.GetTo[User](ctx, client, "/profile")
```

## 2. Per-Request Transport Overrides

In multi-tenant systems, an application may share a global client instance while specific sensitive endpoints require bypassing TLS verification, routing through a dedicated premium proxy, or introducing randomized TCP dial delays to avoid rate limiter detection. Passing per-request modifiers (`mod.With...`) overrides behavior for that specific execution without mutating the client's global immutable configuration.

```go
import (
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

resp, err := client.Request(ctx, http.MethodGet, "/vip-endpoint",
	mod.WithProxyOverride("http://premium-proxy.local:9090"),
	mod.WithInsecureSkipVerify(),
	mod.WithTCPDelay(100*time.Millisecond, 500*time.Millisecond),
)
```

## 3. Mitigating Long-Tail Latency via Request Hedging

Unstable proxy exit nodes or congested upstream servers occasionally stall, causing severe tail-latency spikes (P99/P99.9) that block worker threads. Enabling request hedging instructs `aoni` to monitor response header latency: if the primary attempt does not yield headers within a specified duration, a parallel secondary attempt is dispatched, and whichever finishes first is returned while cancelling the other.

```go
import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

// Static 150ms hedging delay
client := aoni.NewClient(nil, option.WithHedging(150*time.Millisecond))

// Dynamic Hedging: automatically calculates delay threshold based on EWMA P95 RTT
clientDynamic := aoni.NewClient(nil, option.WithDynamicHedging(nil))

user, err := request.GetTo[User](ctx, client, "/fast-path")
```

## 4. Automatic Legacy Charset Transcoding

Legacy APIs and regional services frequently return responses in non-UTF-8 character sets (such as Windows-1251, Shift-JIS, or ISO-8859-1), resulting in corrupt Unicode strings during JSON or XML unmarshaling. `aoni` inspects incoming `Content-Type` charset parameters on the fly and transparently transcodes the payload into valid UTF-8 before decoding.

```go
manifest, err := request.GetTo[Manifest](ctx, client, "/legacy-manifest",
	mod.WithDownloadProgress(func(current, total int64) {
		fmt.Printf("Downloaded %d of %d bytes\n", current, total)
	}),
)
```

## 5. WAF Evasion with JA4 & JA4H Fingerprinting

Web Application Firewalls (such as Cloudflare, Akamai, and Imperva) evaluate TLS ClientHello fingerprints (JA3/JA4) and HTTP header ordering (JA4H) to detect automated HTTP clients. `aoni` emulates browser TLS handshakes via `uTLS` profiles and matches exact browser header capitalization and ordering.

```go
import (
	"fmt"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
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

## 6. Real-Time Socket.IO v5 / Engine.IO v4 Streaming

Connecting to WebSocket and Socket.IO endpoints on protected servers often fails during the initial HTTP handshake due to missing browser TLS signatures or drops silently without ping-timeout detection. `aoni` establishes Socket.IO v5 sessions over WebSockets or HTTP/2 Extended CONNECT tunnels while inheriting the parent client's uTLS profile, proxy rotation, and ping-timeout heartbeats.

```go
import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/lemon4ksan/aoni/realtime/socketio"
)

cfg := socketio.Config{
	Reconnection: true,
	Namespace:    "/realtime-feed",
	Auth:         map[string]string{"token": "secure-session-token"},
}

sio, err := socketio.DialSocketIO(ctx, client, "wss://api.example.com/socket.io/", cfg)
if err != nil {
	log.Fatal(err)
}
defer sio.Close()

sio.On("price_update", func(args []json.RawMessage) {
	fmt.Printf("Live Payload: %s\n", string(args[0]))
})
```

## 7. Diagnostic Tracing & Shell cURL Generation

Diagnosing connection bottlenecks across complex proxy chains requires detailed latency breakdowns across DNS, TCP, TLS, and TTFB phases. Attaching connection tracers allows inspecting wire metrics in real time and dumping executable cURL commands for reproduction.

```go
var trace telemetry.TraceInfo

user, err := request.GetTo[User](ctx, client, "/debug",
	aoni.Trace(&trace),    // Detailed DNS, TCP, TLS, and TTFB latency metrics
	mod.WithCurlDump(),    // Prints executable 'curl -X GET ...' command to stderr
)

fmt.Printf("DNS: %s | TCP: %s | TLS: %s | TTFB: %s\n",
	trace.DNSLookup, trace.TCPConn, trace.TLSHandshake, trace.ServerProcessing)
```

## 8. Structured API Response Unwrapping

Many JSON APIs wrap payload objects inside standard envelope structures (such as `{"status":"success","data":{...}}`). Implementing the `aoni.BaseResponse` interface allows `aoni` to validate status codes, inspect envelope errors, and extract inner data models into destination structs in a single pass without intermediate allocations.

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

// Unmarshals inner 'data' directly into User struct
user, err := request.GetTo[User](ctx, client, "/users/1")
```

### 💡 BaseURL & Path Resolution Rules (RFC 3986 vs. Fast Normalization)

Under strict RFC 3986 §5.2 rules:
- `BaseURL` should include a trailing slash (e.g. `https://api.example.com/v1/`) to signify a directory.
- `Path` should omit a leading slash (e.g. `users/1`) to signify an intra-directory relative resource.

`aoni` enforces a zero-allocation defense-in-depth normalization layer so that all combinations resolve cleanly without 404s or double-slashes:

| BaseURL Config | Request Path | Resolved Target URL | Notes |
| :--- | :--- | :--- | :--- |
| `https://api.com/v1/` | `users/1` | `https://api.com/v1/users/1` | Standard RFC 3986 directory resolution |
| `https://api.com/v1` | `/users/1` | `https://api.com/v1/users/1` | Server-style route concatenation |
| `https://api.com/v1/` | `/users/1` | `https://api.com/v1/users/1` | Normalizes slashes (does NOT drop `/v1/` to domain root) |
| `https://api.com/v1` | `users/1` | `https://api.com/v1/users/1` | Auto-appends boundary slash |
| `https://api.com/v1/` | `https://other.com/auth` | `https://other.com/auth` | Absolute URL bypasses BaseURL completely |

## 9. Socket-Level DPI Evasion (Fragmentation & CDN Padding)

Deep Packet Inspection (DPI) firewalls inspect initial TCP segment boundaries and ClientHello byte lengths to detect automated scraping clients. `aoni` mitigates statistical packet length analysis by segmenting ClientHello payloads into micro-chunks and injecting randomized CDN tracing headers.

```go
import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/option"
)

client := aoni.NewClient(nil,
	// Split TLS ClientHello across 2-byte TCP segments with jitter
	option.WithFragmentation(aoni.FragmentConfig{
		ChunkSize: 2,
		MaxDelay:  10 * time.Millisecond,
	}),
	// Inject randomized CDN padding headers to flatten packet length histograms
	option.WithPacketPadding(fingerprint.PaddingConfig{
		MinPaddingBytes: 16,
		MaxPaddingBytes: 64,
		HeaderPool:      fingerprint.CloudflareHeaderPool,
	}),
)
```

## 10. In-Memory Unit Testing with Vortex Mocks

Running integration tests against real `httptest.Server` instances in parallel test suites causes TCP port exhaustion, slow execution, and OS socket contention. Generating in-memory mock servers with `vortex mock` allows routing HTTP traffic directly through `fasthttputil.InmemoryListener` without opening OS network ports.

```bash
vortex mock pkg/services/user/api.go
```

```go
func TestUserAPI(t *testing.T) {
	ctx := context.Background()

	mockServer := user.NewUserAPIMockServer()
	mockServer.OnGetUser = func(ctx context.Context, id string) (*user.UserDTO, error) {
		return &user.UserDTO{ID: id, Name: "Alice"}, nil
	}

	client := mockServer.Client()

	res, err := client.GetUser(ctx, "usr_100")
	require.NoError(t, err)
	require.Equal(t, "Alice", res.Name)
}
```

## 11. Zero-Allocation Protobuf Services with `vtprotobuf`

Standard `protoc-gen-go` generates reflection-based serialization routines that allocate heap memory on every message encode and decode. Compiling `.proto` schemas with `vortex proto` generates optimized `vtprotobuf` zero-allocation codecs that integrate with `request.PostProtoTo[T]`.

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

## 12. Streaming Server-Sent Events (SSE) & NDJSON Processing

Consuming high-throughput event streams (such as AI token streams or exchange order books) line by line using standard scanners creates memory bloat and allocation churn. `realtime/stream.StreamNDJSON` and `realtime/stream.StreamSSE` process incoming records sequentially with pooled decoders and context-aware cancellation.

```go
import "github.com/lemon4ksan/aoni/realtime/stream"

type OrderbookUpdate struct {
	SeqID uint64      `json:"seq_id"`
	Bids  [][]float64 `json:"bids"`
	Asks  [][]float64 `json:"asks"`
}

err := stream.StreamNDJSON(ctx, client, "GET", "https://stream.example.com/orderbook", nil,
	func(update *OrderbookUpdate) error {
		fmt.Printf("Seq %d: Top Bid: %.2f\n", update.SeqID, update.Bids[0][0])
		return nil // Return non-nil error to gracefully stop stream
	},
)
```

## 13. TLS 1.3 Encrypted Client Hello (ECH) & DNS-over-HTTPS

Intermediate middleboxes and ISPs inspect unencrypted Server Name Indication (SNI) headers in TLS handshakes to enforce domain-based filtering and censorship. Enabling ECH (RFC 9460) via secure DoH or DoQ resolvers allows `aoni` to resolve SVCB/HTTPS records and encrypt the inner SNI inside outer TLS frames.

```go
import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/dns"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

client := aoni.NewClient(nil,
	option.WithDoHResolver(dns.CloudflareDoH),
	option.WithECH(aoni.ECHConfig{
		Enabled:            true,
		FallbackToPlainTLS: false, // Enforce strict privacy
	}),
	option.WithChrome(),
)

resp, err := request.GetTo[map[string]any](ctx, client, "https://crypto.cloudflare.com/cdn-cgi/trace")
```

## 14. IPv6 Subnet Rotation (/64 Prefix Pool)

High-throughput scraping against aggressive rate limiters exhausts IPv4 pools rapidly. Binding a `/64` IPv6 prefix to the server allows dynamically generating and rotating random source IPv6 addresses on each outbound dial with zero OS network configuration.

```go
import (
	"log"

	"github.com/lemon4ksan/foundation/net/ip"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

rotator, err := ip.NewIPv6SubnetRotator("2001:db8:1234:5678::/64")
if err != nil {
	log.Fatal(err)
}

client := aoni.NewClient(nil, option.WithDialer(rotator.Dialer()))

// Every request originates from a unique randomized IPv6 address
user, err := request.GetTo[User](ctx, client, "https://ipv6.api.example.com/profile")
```

## 15. Chromium-Grade Network Resilience (421 Recovery & Happy Eyeballs v3)

HTTP/2 connection coalescing triggers HTTP 421 (Misdirected Request) when host certificate scopes mismatch, while broken HTTP/3 endpoints cause silent connection hangs. `aoni` implements Chromium Happy Eyeballs v3 to race HTTP/3 against HTTP/2/1.1 and automatically re-routes rejected 421, 408, and 425 requests onto clean transport connections.

```go
import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

client := aoni.NewClient(nil,
	option.WithHTTP3(),             // Enables QUIC/H3 protocol racing
	option.WithHappyEyeballs(true), // Chromium Happy Eyeballs v3 racing
	option.WithAutoRecovery(true),  // Transparent re-dial on HTTP 421, 408, 425
)
```

## 16. Continuous Contract Drift Detection in CI/CD

Upstream API changes can introduce breaking schema modifications without warning. Integrating `vortex diff` into CI pipelines validates that local Go AST interface contracts remain fully synchronized with remote OpenAPI specifications.

```bash
# In CI: fails if upstream schema drifted from local declarative Go contract
vortex diff https://api.example.com/openapi.json pkg/services/user/api.go --fail-on-breaking

# Health check all workspace contracts
vortex status -strict
```

## 17. Outbound HTTP Routing via Multi-Hop SSH Jump Hosts

When backend microservices reside behind secure bastion hosts or private VPC boundaries, outbound HTTP requests can be tunneled directly through an SSH client pipeline without running a local SOCKS proxy daemon.

```go
import (
	"context"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/tunnel/ssh"
)

// 1. Establish bastion jump host session
bastion, err := ssh.NewClient(ctx, "bastion.corp.local",
	ssh.WithKeyFile("~/.ssh/id_ed25519"),
)
if err != nil {
	panic(err)
}

// 2. Connect to internal target through the bastion
internalSSH, err := ssh.NewClient(ctx, "10.0.1.50:22",
	ssh.WithJump(bastion),
	ssh.WithKeyFile("~/.ssh/id_ed25519"),
)
if err != nil {
	panic(err)
}

// 3. Bind SSH dialer to an aoni HTTP Client
client := aoni.NewClient(nil, option.WithDialer(internalSSH))

// Outbound request executes through the SSH encrypted tunnel
resp, err := request.GetTo[User](ctx, client, "http://internal-api.service.local/v1/data")
```

## 18. Reverse SSH Tunnel Gateway with TLS SNI Routing

To expose local HTTP services behind NAT or firewalls without third-party services like ngrok, `aoni` provides an embedded reverse SSH tunnel router that routes incoming TLS connections based on their SNI hostname.

```go
import (
	"context"
	"net/http"

	"github.com/lemon4ksan/aoni/tunnel/ssh/reverse"
)

router := reverse.NewRouter()

// Register custom subdomain route
subdomain, err := router.Register(ctx, "api.tunnel.example.com", remoteSSHConn)
if err != nil {
	panic(err)
}

// Gateway inspects SNI in ClientHello and forwards raw traffic to the appropriate SSH channel
gateway := reverse.NewGateway(router)
http.ListenAndServe(":443", gateway)
```

## 19. Full-Duplex Bidirectional and Client Streaming gRPC

Beyond standard unary RPCs, `aoni/grpc` supports full-duplex HTTP/2 streaming protocols directly over custom stealth personas with zero heavyweight `google.golang.org/grpc` dependencies.

```go
import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/option"
)

client := aoni.NewClient(nil, option.WithChrome())

// 1. Full-Duplex Bidirectional Stream (Bidi)
stream, err := grpc.BidiStream[*ChatMessage, ChatMessage](ctx, client, "/ChatService/BiDiChat")
if err != nil {
	log.Fatal(err)
}
defer stream.Close()

// Concurrent send pipeline
go func() {
	for _, msg := range outboundQueue {
		_ = stream.Send(msg)
	}
	_ = stream.CloseSend() // Sends HTTP/2 END_STREAM on request channel
}()

// Stream reading loop with trailer validation
for {
	msg, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Received: %s", msg.Text)
}
```

# Cookbook: Resiliency Recipes

## 1. Proxy Rotation with Sticky Sessions

Session affinity requires consistent routing through proxy exit nodes. `proxy.Rotator` provides session affinity via sticky key extractors.

```go
rotator, err := proxy.NewRotatorFromStrings(proxy.RotatorConfig{
	MaxFails:   3,
	RetryAfter: 30 * time.Second,
}, "http://proxy1.local:8080", "http://proxy2.local:8080")

stickyRotator := rotator.WithStickySessions(proxy.StickyKeyFromCookie("sessionid"))
client := aoni.NewClient(nil, option.WithProxyRotator(stickyRotator))
```

## 2. Per-Request Transport Overrides

Request modifiers (`mod.With...`) override behavior for specific executions without mutating global configuration.

```go
resp, err := client.Request(ctx, http.MethodGet, "/vip-endpoint",
	mod.WithProxyOverride("http://premium-proxy.local:9090"),
	mod.WithInsecureSkipVerify(),
	mod.WithTCPDelay(100*time.Millisecond, 500*time.Millisecond),
)
```

## 3. Request Hedging

Request hedging mitigates tail-latency spikes. If a primary attempt stalls, a parallel secondary attempt is dispatched.

```go
client := aoni.NewClient(nil, option.WithHedging(150*time.Millisecond))
clientDynamic := aoni.NewClient(nil, option.WithDynamicHedging(nil))
```

## 4. Legacy Charset Transcoding

`aoni` inspects `Content-Type` charset parameters and transcodes legacy payloads (Windows-1251, Shift-JIS) into UTF-8 prior to unmarshaling.

```go
manifest, err := client.GetTo[Manifest](ctx, "/legacy-manifest")
```

## 5. TLS & HTTP Fingerprinting

Configures uTLS profiles and exact browser header capitalization.

```go
client := aoni.NewClient(nil,
	option.WithTLSFingerprint(aoni.BrowserChrome),
	option.WithJA4Callback(func(r ja4.Report) {
		fmt.Println("Handshake TLS JA4:", r.JA4)
	}),
)
```

## 6. Socket.IO v5 / Engine.IO v4 Streaming

Establishes sessions over WebSockets or HTTP/2 Extended CONNECT tunnels.

```go
cfg := socketio.Config{
	Reconnection: true,
	Namespace:    "/realtime-feed",
	Auth:         map[string]string{"token": "secure-session-token"},
}
sio, err := socketio.DialSocketIO(ctx, client, "wss://api.example.com/socket.io/", cfg)
```

## 7. Diagnostic Tracing

Connection tracers output wire metrics and executable cURL commands.

```go
var trace telemetry.TraceInfo
user, err := client.GetTo[User](ctx, "/debug",
	mod.WithTrace(&trace),
	mod.WithCurlDump(),
)
```

## 8. Structured API Response Unwrapping

Implements `aoni.BaseResponse` to unwrap nested JSON envelopes in a single pass.

```go
client := aoni.NewClient(nil,
	option.WithBaseResponse(func() aoni.BaseResponse { return &APIEnvelope{} }),
)
user, err := client.GetTo[User](ctx, "/users/1")
```

### BaseURL & Path Resolution Rules

| BaseURL Config | Request Path | Resolved Target URL | Notes |
| :--- | :--- | :--- | :--- |
| `https://api.com/v1/` | `users/1` | `https://api.com/v1/users/1` | RFC 3986 |
| `https://api.com/v1` | `/users/1` | `https://api.com/v1/users/1` | Server-style concat |
| `https://api.com/v1/` | `/users/1` | `https://api.com/v1/users/1` | Slash normalization |
| `https://api.com/v1` | `users/1` | `https://api.com/v1/users/1` | Boundary auto-append |
| `https://api.com/v1/` | `https://other.com` | `https://other.com` | Absolute URL bypass |

## 9. Packet Fragmentation & Padding

Segments TCP payloads and injects padding headers to obscure byte length signatures.

```go
client := aoni.NewClient(nil,
	option.WithFragmentation(fragment.Config{ChunkSize: 2, MaxDelay: 10 * time.Millisecond}),
	option.WithPacketPadding(fingerprint.PaddingConfig{MinPaddingBytes: 16, MaxPaddingBytes: 64, HeaderPool: fingerprint.CloudflareHeaderPool}),
)
```

## 10. In-Memory Vortex Mocks

Routes HTTP traffic directly through `fasthttputil.InmemoryListener`.

```bash
vortex mock pkg/services/user/api.go
```

## 11. Protobuf Services (`vtprotobuf`)

Generates and integrates zero-allocation `vtprotobuf` codecs.

```go
resp, err := client.PostProto[pb.TradeResponse](ctx, "https://api.steam.com/trade", reqProto)
```

## 12. SSE & NDJSON Processing

Sequential streaming with Go 1.23+ range-over-func iterators.

```go
respStream, err := stream.Get(ctx, client, "https://stream.example.com/orderbook")
if err != nil {
	log.Fatal(err)
}
defer respStream.Close()

for update, err := range stream.IterNDJSON[OrderbookUpdate](respStream) {
	if err != nil {
		break
	}
	_ = update
}
```

## 13. TLS 1.3 ECH & DNS-over-HTTPS

Encrypts SNI headers via ECH (RFC 9460) and DoH/DoQ resolvers.

```go
dohResolver := dns.NewDoHResolver("https://cloudflare-dns.com/dns-query")
client := aoni.NewClient(nil,
	option.WithDNSResolver(dohResolver),
	option.WithAutoECH(true),
)
```

## 14. IPv6 Subnet Rotation

Generates random source IPv6 addresses on each outbound dial from a configured prefix.

```go
rotator, _ := ip.NewIPv6SubnetRotator("2001:db8:1234:5678::/64")
client := aoni.NewClient(nil, option.WithDialer(rotator.Dialer()))
```

## 15. HTTP Recovery & Happy Eyeballs v3

Races protocols and re-routes 421/408/425 rejections.

```go
client := aoni.NewClient(nil,
	option.WithHTTP3(),
	option.WithHappyEyeballs(true),
	option.WithAutoRecovery(true),
)
```

## 16. Outbound SSH Jump Hosts

Routes HTTP requests through an SSH client pipeline.

```go
bastion, _ := ssh.NewClient(ctx, "bastion.corp.local")
internalSSH, _ := ssh.NewClient(ctx, "10.0.1.50:22", ssh.WithJump(bastion))
client := aoni.NewClient(nil, option.WithDialer(internalSSH))
```

## 17. Reverse SSH Tunnel Gateway

Routes incoming TLS connections based on SNI hostname over SSH.

```go
router := reverse.NewRouter()
router.Register(ctx, "api.tunnel.example.com", remoteSSHConn)
gateway := reverse.NewGateway(router)
http.ListenAndServe(":443", gateway)
```

## 18. gRPC-Web Streaming

Full-duplex HTTP/2 streaming protocols.

```go
stream, _ := grpc.BidiStream[*ChatMessage, ChatMessage](ctx, client, "/ChatService/BiDiChat")
```

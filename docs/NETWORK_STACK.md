# aoni Network Stack Specification

The aoni networking engine is a high-performance, resilient, and anti-analysis network library written in Go. It is designed to navigate restrictive networks, evade Deep Packet Inspection (DPI) and fingerprinting systems, and maintain high availability across unreliable transport layers.

This document details the architectural components, protocol implementations, RFC specifications, and resilience mechanisms implemented within aoni and its zero-allocation engine aoni/fast.

## 1. Multi-Protocol Engine and Dispatching

The aoni/fast architecture manages three distinct protocol engines through a unified interface:

1. **HTTP/1.1 Engine**: Built on a zero-allocation byte-buffer architecture provided by fasthttp.
2. **HTTP/2 Engine (h2engine)**: A custom HTTP/2 multiplexer providing low-level control over HPACK dynamic tables, frame serialization order, and custom connection settings.
3. **HTTP/3 Engine (h3engine)**: Built on QUIC (quic-go), implementing QPACK header compression, QUIC datagrams, and stream multiplexing without Head-of-Line blocking.

### Happy Eyeballs v3 (Protocol Racing)
To minimize connection establishment latency on networks where UDP/QUIC might be throttled or blocked, aoni implements Protocol Racing (RFC 8305):

- Upon encountering an Alt-Svc: h3 advertisement, the client initiates an HTTP/3 connection over QUIC immediately.
- Concurrently, a 250 ms delay timer (HappyEyeballsDelay) is started.
- If HTTP/3 completes before the timer expires, the HTTP/3 stream is selected and the timer is canceled.
- If the timer expires before HTTP/3 completes, a parallel TCP-based HTTP/2 or HTTP/1.1 connection attempt is launched.
- The first protocol to complete its handshake and yield a valid response wins the race. The context for the losing connection attempt is canceled, and its allocated memory buffers are safely returned to memory pools.

### Broken Alt-Svc Exponential Backoff (RFC 7838)
When an HTTP/3 connection attempt fails (e.g., due to UDP filtering by an ISP or middlebox), the target endpoint is marked as broken in the global cache (globalAltSvcCache).

To prevent repeated connection stalls, aoni applies an exponential backoff algorithm:

$$\text{Backoff} = \min\Big(48\text{ hours},\ 5\text{ minutes} \times 2^{\text{Fails} - 1}\Big)$$

- Initial Failure: 5-minute cooldown.
- Subsequent Consecutive Failures: Cooldown doubles sequentially (10m, 20m, 40m, etc.) up to a maximum cap of 48 hours.
- Recovery: A single successful HTTP/3 transaction immediately clears the failure record and resets the failure counter to zero.

### HTTP/1.1 to HTTP/2 Frame Fallback
If an HTTP/1.1 request receives an HTTP/2 frame payload (e.g., due to HTTP/2 Prior Knowledge without ALPN negotiation), aoni intercepts the frame error, resets the response buffer, and transparently re-dispatches the request through h2engine.

## 2. Stealth, Anti-DPI, and Fingerprint Evasion

### TLS 1.3 Encrypted Client Hello (ECH / RFC 9460)
aoni supports complete Server Name Indication (SNI) encryption:

- **Resolution**: Automatically queries DNS HTTPS (Type 65) records via DNS-over-HTTPS (DoH) or DNS-over-QUIC (DoQ) to retrieve ech parameter blocks (SvcParamKey 5).
- **Execution**: Configures uTLS with the retrieved EncryptedClientHelloConfigList to encrypt the ClientHelloInner containing the target SNI.
- **DPI Evasion**: Observers and middleboxes only inspect the unencrypted Outer SNI or a generic fallback domain, preventing domain-based filtering.

### uTLS Fingerprint Impersonation and JA4 Telemetry
aoni decouples TLS handshake construction from standard Go crypto/tls:

- Supports precise ClientHello profile emulation for Google Chrome, Mozilla Firefox, and Apple Safari.
- Configures cipher suite ordering, elliptic curves, ALPN tokens, extension ordering, and GREASE values.
- Computes pure-Go JA4 (TLS) and JA4H (HTTP) fingerprint strings for real-time telemetry and inspection.

### TLS Certificate Compression (RFC 8879)
aoni implements the compress_certificate extension (extension type 27) within uTLS:

- Supports **Brotli** and **Zstd** decompression algorithms.
- Reduces TLS handshake payload size by 1 to 3 KB per connection, decreasing initial packet round-trips.

### p0f TCP/IP Stack Signature Spoofing
To prevent OS-level TCP fingerprinting:

- Modifies raw socket parameters at the system call level via sys/unix.
- Spoofs IPv4 Time to Live (TTL), the Don't Fragment (DF) bit, and TCP Receive Window size (SO_RCVBUF) to match target operating systems (Windows 10/11, macOS, Linux, Android, iOS).

### High-Entropy Client Hints (W3C / Chromium)
aoni includes an automated Client Hints generator (fingerprint/clienthints.go):

- Parses the configured User-Agent string and target OS identifier.
- Generates a fully consistent set of Low-Entropy and High-Entropy headers: Sec-CH-UA, Sec-CH-UA-Mobile, Sec-CH-UA-Platform, Sec-CH-UA-Full-Version-List, Sec-CH-UA-Arch, Sec-CH-UA-Bitness, Sec-CH-UA-Model, Sec-CH-UA-PlatformVersion, and Sec-CH-UA-Form-Factors.

### TCP Packet Fragmentation and Padding
- **Write Chunking**: Splits outgoing TCP payloads into variable-sized chunks (MinChunkSize to MaxChunkSize) with configurable inter-chunk jitter delays.
- **Header Padding**: Injects randomized padding strings (PaddingConfig) into request headers using pools of CDN header names (e.g., X-Amz-Trace-Id, CF-RAY) to obfuscate packet length signatures against statistical DPI analysis.

## 3. Chrome-Grade Resilience and Auto-Recovery

aoni implements an automated transaction recovery pipeline corresponding to Chromium network stack behavior.

### HTTP 421 Misdirected Request Auto-Recovery (RFC 9113 Section 9.1.2)
- **Condition**: Sent by CDNs or origin servers when connection coalescing (IP pooling) causes a request to arrive at a server instance incapable of serving the requested authority.
- **Action**: Per RFC 9113, a 421 response guarantees that the server did not process the request. aoni evicts the host mapping from the connection pool, marks the Alt-Svc endpoint as failed, sets DisableAltSvc = true, and re-executes the transaction on a fresh connection. This recovery applies to all HTTP methods, including non-idempotent operations.

### HTTP 408 Request Timeout Keep-Alive Retry (RFC 9112 Section 9.6)
- **Condition**: Sent when a server closes an idle keep-alive connection due to an internal timeout right as the client transmits a new request.
- **Action**: aoni detects the race condition, closes all idle connections in the pool, sets Connection: close, and transparently re-dispatches the request on a new socket.

### HTTP 425 Too Early 0-RTT Rejection Fallback (RFC 8470)
- **Condition**: Sent by a server unwilling to process 0-RTT Early Data due to anti-replay policies or stale session tickets.
- **Action**: aoni disables 0-RTT for the request context (Disable0RTT = true), strips Early-Data headers, and re-executes the request in standard 1-RTT mode.

### Async Retry (Priority Starvation Guard)
- **Problem**: Long-running synchronous retry loops (time.Sleep or channel waits) across thousands of concurrent goroutines can starve the Go runtime scheduler (GOMAXPROCS) and inflate stack memory.
- **Action**: For initial attempts (< 25 retries), aoni uses a zero-allocation timer pool (timer.Acquire). If retry attempts reach a configured threshold (defaulting to 25), execution transitions to asynchronous time.AfterFunc timers. This yields the goroutine and delegates wakeup to the Go timer runtime.

### Response Smuggling and Desync Guard (RFC 9112)
aoni validates incoming response headers to prevent HTTP desynchronization and response splitting attacks:

- **Content-Length Validation**: Rejects responses containing multiple conflicting Content-Length header values.
- **Location Validation**: Rejects 3xx responses containing multiple conflicting Location headers.
- **Chunked vs Content-Length (RFC 9112 Section 6.3)**: If Transfer-Encoding: chunked is present, any Content-Length header is automatically stripped to prevent parser desynchronization between proxies and the client.
- **Header Control Character Inspection**: Scans header names and values for carriage return (\r), line feed (\n), or null (\x00) bytes, rejecting non-compliant messages.

### Strict Body Truncation
When a response specifies Content-Length: N:
- For buffered responses, the byte slice is truncated to body[:N] without reallocation.
- For streaming responses, the body reader is wrapped in io.LimitReader(stream, N).
- Upon closing the response body, unread bytes up to $N$ are drained, while any trailing garbage bytes sent beyond $N$ are discarded, preventing connection corruption in keep-alive pools.

### OS Power Management (Clock-Jump Analysis)
To eliminate "zombie sockets" caused by system sleep and resume cycles (where network interfaces are disabled while connection pools retain stale sockets):

- PowerWatcher monitors system time deltas using a 1-second ticker.
- If the measured interval between two consecutive ticks exceeds 5 seconds, system suspension is inferred.
- aoni immediately invokes CloseIdleConnections(), purging all idle H1, H2, and H3 sockets, and flushing TLS session ticket caches.

## 4. Transport, Proxying, and Network Layers

### Multi-Protocol Proxy Tunneling
aoni supports chaining and proxy routing over multiple protocols:

- **SOCKS5 / SOCKS5h**: Supports local and remote DNS resolution, with optional username/password authentication.
- **HTTP CONNECT**: Establishes encrypted tunnels over HTTP proxies with basic and digest authentication support.

### Adaptive Proxy Timeout
To optimize failover times across heterogeneous proxy pools:

$$\text{Timeout} = \max\Big(8\text{s},\ \min\big(30\text{s},\ p95\text{RTT} \times 4.0\big)\Big)$$

- Low-latency data center proxies (p95 RTT = 50 ms) use a tight 8-second timeout, triggering rapid failover if an exit node fails.
- High-latency mobile or residential proxies (p95 RTT = 1500 ms) receive a scaled 10-second timeout, avoiding false-positive connection drops.

### Source IP Rotation and IPv6 Subnet Randomization
- **IPv4 Rotation (SourceIPRotator)**: Rotates outbound connections across a pool of local network interface IP addresses using round-robin selection.
- **IPv6 Subnet Randomization (IPv6SubnetRotator)**: Generates cryptographically random IPv6 host addresses within a configured CIDR prefix (e.g., /64 or /56) for every outbound socket.

### BCP 38 Ingress Filtering and Martian Address Protection
- Enforces BCP 38 / BCP 84 source address validation on incoming network packets.
- Automatically drops packet payloads originating from unroutable or reserved Martian address space (RFC 2827 / RFC 3704).

### MASQUE and Layer 3 TUN Bridging
- **TUN Adapters**: Native cross-platform Layer 3 TUN drivers (Wintun on Windows, utun on macOS, /dev/net/tun on Linux).
- **MASQUE Protocol**: Bridges Layer 3 IP packets over HTTP/3 connect-ip (RFC 9484) and connect-udp (RFC 9298) sessions.
- **Path MTU Discovery & MSS Clamping**: Features an ICMP "Packet Too Big" generator (IPv4 RFC 1191, IPv6 RFC 4443) and performs in-place TCP SYN MSS clamping (MaxMTU - 40 bytes for IPv4, MaxMTU - 60 bytes for IPv6) to prevent path MTU black-holing.

## 5. High-Performance Caching and State Isolation

### W3C `No-Vary-Search` (URL Normalization)
To maximize cache hit rates across URLs containing non-semantic marketing or tracking parameters:

- **Query Normalization**: Automatically strips known tracking parameters (utm_source, utm_medium, utm_campaign, utm_term, utm_content, fbclid, gclid, msclkid, _ga, ref).
- **Alphabetical Sorting**: Reorders remaining query parameters alphabetically by key.
- **Server Header Support**: Parses response headers (No-Vary-Search: params=("key")) and dynamically updates normalization rules for the cached resource.

### `Cookie-Indices` (Selective Cookie Hashing)
To prevent Vary: Cookie headers from invalidating caches due to unrelated session or tracking cookies:

- Parses the server's Cookie-Indices: "theme", "lang" response header or uses client-configured cookie lists.
- Extracts matching cookie name-value pairs, sorts them alphabetically, and computes a 12-character hex SHA-256 hash.
- Appends the hash to the cache key: GET:https://example.com/page:c=8f3b2a1c9d0e.

### HTTP/2 `PUSH_PROMISE` Caching
When SETTINGS_ENABLE_PUSH = 1 is enabled:

- h2engine intercepts incoming PUSH_PROMISE frames (type 0x05).
- Validates that the promised request is a safe method (GET/HEAD) and that the server is authoritative for the target origin.
- Asynchronously accumulates response frames on the promised stream ID and saves the completed resource into CacheStore.

### CHIPS / Cookie Partitioning (RFC 6265bis)
ProxyIsolatedCookieJar supports CHIPS (Cookies Having Independent Partitioned State):

- Parses the Partitioned attribute (SameSite=None; Secure; Partitioned).
- Keys cookies using a triple key: (Top-Level Site, Cookie Domain, Proxy).
- Isolates cookies within embedded iFrames and external authorization widgets without cross-site tracking vulnerabilities.

### Proxy-Aware TLS Session Ticket Cache
ProxyAwareSessionCache wraps session storage for both uTLS and standard crypto/tls:

- Keyed by the active proxy endpoint or source IP address.
- When proxy failover or rotation occurs (SetProxyKey), the session cache is automatically flushed, preventing exit-node tracking via TLS session ticket correlation.

## 6. High-Performance DNS Engine

aoni provides an independent, anti-censorship DNS resolution package (netutil/dns):

- **DNS over HTTPS (DoH — RFC 8484)**: Queries A and AAAA records using HTTP GET or POST wire format payloads over fast.Client.
- **DNS over TLS (DoT — RFC 7858)**: Queries DNS records over TLS with a 2-byte length prefix.
- **DNS over QUIC (DoQ — RFC 9250)**: Queries DNS records over dedicated QUIC streams (ALPN "doq", port 853/UDP, Message ID = 0).
- **EDNS0 Padding (RFC 7830)**: Pads binary DNS queries to multiples of 128 bytes with zero octets to prevent side-channel packet size analysis over encrypted transports.
- **EDNS Client Subnet (ECS — RFC 7871)**: Passes anonymized client subnets (/24 IPv4, /56 IPv6) in OPT RRs to obtain optimal CDN edge node IP addresses.
- **Authoritative TTL Extraction**: Parses TTL fields from binary response records, populating InMemoryDNSCache with exact expiration durations.
- **Race and Fallback Resolvers**: Supports concurrent racing across multiple DNS resolvers (FastRaceResolver) and prioritized sequential failovers (FallbackResolver).

## 7. Telemetry, Observability, and Diagnostics

### PING RTT Calibration (Pure Network Latency)
- Transmits background HTTP/2 (0x6) and HTTP/3 PING frames with an embedded 8-byte timestamp (time.Now().UnixNano()).
- Upon receiving the PING ACK, calculates time.Since(sentTimestamp) to measure layer-7 network RTT without application-level processing delay.
- Feeds measurements directly into telemetry.RTTTracker to calibrate dynamic hedging parameters.

### Dynamic Percentile Hedging
- Calculates $p95$ network RTT from RTTTracker measurements.
- If a request does not yield response headers within $1.5 \times p95\text{RTT}$, aoni automatically dispatches a second parallel request over a separate socket.
- The first successful response is returned, while the slower request context is canceled.

### Duplicate Request Logger (Loop Guard)
- Maintains a 128-element zero-allocation ring buffer (DuplicateRequestGuard).
- Computes a 64-bit FNV-1a hash of Method + ":" + URL without string allocations using bytesconv.S2B.
- Emits a diagnostic warning log (logger.Warn) if an identical request signature is dispatched repeatedly within 10 seconds.

### Web Traffic Inspector Dashboard
- Embedded HTTP dashboard (inspector.Enable(client, ":8080")) providing real-time UI monitoring over Server-Sent Events (SSE).
- Displays live protocol selection (H3/H2/H1), 0-RTT status, ECH encryption flags, JA4/JA4H fingerprints, header inspections, and timing breakdowns.
- Exports recorded transactions to **HAR 1.2** JSON format or shell-escaped cURL commands.

## 8. Developer Experience

aoni provides single-line configuration presets that bundle all stealth, performance, and resilience features:

```go
// Enables uTLS Chrome 120+, H2/H3 settings, High-Entropy Client Hints, ECH,
// 0-RTT, Certificate Compression, CHIPS cookie partitioning, and 421/408/425 auto-recovery.
client := aoni.NewClient(nil, option.WithChrome())

// Enables mobile Chrome emulation for Android.
mobileClient := aoni.NewClient(nil, option.WithChromeMobile())

// Enables Firefox desktop profile with ECH and 0-RTT.
firefoxClient := aoni.NewClient(nil, option.WithFirefox())
```

## 9. Real-Time Protocol Engine (WebSocket, Extended CONNECT & Socket.IO)

aoni extends stealth and resilience features to real-time bidirectional transports via github.com/lemon4ksan/aoni/ws and github.com/lemon4ksan/aoni/realtime/socketio.

### HTTP/2 & HTTP/3 Extended CONNECT (RFC 8441 / RFC 9220)
When establishing WebSocket connections over encrypted transports (wss://), aoni negotiates multiplexed streams:

- **Protocol Negotiation**: Negotiates h2 or h3 during ALPN. If the peer advertises SETTINGS_ENABLE_CONNECT_PROTOCOL = 1, aoni opens a single stream using HTTP/2 or HTTP/3 Extended CONNECT with :protocol = websocket.
- **Multiplexing**: Eliminates dedicated TCP/TLS handshake overhead per WebSocket connection, sharing the existing HTTP/2 or HTTP/3 session.
- **Fallback**: Automatically falls back to standard HTTP/1.1 Upgrade: websocket if Extended CONNECT is unsupported.

### Permessage-Deflate Compression (RFC 7692)
- Implements RFC 7692 permessage-deflate payload compression with client_no_context_takeover and server_no_context_takeover.
- **Sync Flush Stripping**: Automatically strips trailing 0x00 0x00 0xFF 0xFF sync flush bytes on compression and appends them prior to decompression.
- **Buffer Pooling**: Recycles DEFLATE readers (flate.Reader) and writers (flate.Writer) via sync.Pool.

### Socket.IO v5 / Engine.IO v4 State Machine
Real-time messaging over aoni WebSockets utilizes a deterministic Finite State Machine (FSM):

- **FSM States**: Strictly models states (sioStateClosed, sioStateOpening, sioStateOpen, sioStateClosing) via kata.FSM.
- **Binary Frame Deconstruction**: Automatically extracts binary attachments ([]byte) from event payloads, replaces them with _placeholder JSON descriptors, transmits raw binary Engine.IO frames (type 'b'), and reconstructs payloads on the receiving peer.
- **Asynchronous Acknowledgments**: Manages event acknowledgments via jobs.Manager[int64, []json.RawMessage] with configurable timeout guarantees.

## 10. Streaming Engine, SSE, and gRPC-Web Transcoding

The realtime/stream package provides zero-allocation parsing for long-lived HTTP streams.

### gRPC-Web Frame Parsing (5-Byte Framing)
- **Framing Specification**: Parses gRPC-Web streams carrying a 5-byte header prefix:
  $$\text{Header} = [\text{Flags (1B)}] + [\text{Length (4B Big-Endian)}]$$
  - Flag 0x00: Protobuf Data Frame.
  - Flag 0x80: Trailer Frame (gRPC status and status message).
- **Text / Base64 Auto-Detection**: Automatically detects Base64-encoded text streams (application/grpc-web-text), wrapping input streams in a base64.NewDecoder.
- **Gzip Frame Decompression**: Handles Flag 0x01 compressed frames on the fly using pooled gzip.Reader instances.

### Resumable Server-Sent Events (SSE)
- **Header Injection**: Requests streams using Accept: text-event-stream, Cache-Control: no-cache, and Connection: keep-alive.
- **Auto-Reconnection**: Tracks the most recent id: value. Upon network disconnection, automatically reconnects injecting Last-Event-ID: <id>.
- **Retry Backoff Parsing**: Parses server-sent retry: <ms> directives, dynamically updating the reconnection delay.

## 11. Pipeline Architecture and Custom Phase Hooks

Transaction execution within aoni is structured around a 7-phase execution pipeline (internal/pipeline).

```
[ Request ] ──► (1. Prep) ──► (2. CacheLookup) ──► (3. Dispatch) 
                                                          │
[ Response ] ◄── (7. CacheSave) ◄── (6. Validate) ◄── (5. WAF) ◄── (4. Decompress)
```

### Precomputed Execution Flags
Feature selection is resolved into a single uint32 bitmask (BuildFlags()), permitting zero-allocation phase evaluations during hot-path execution:

- FlagRotateUA (0x1), FlagDPIJitter (0x2), FlagRedact (0x4), FlagDecompress (0x8), FlagValidate (0x10), FlagChallenge (0x20), FlagCache (0x40), FlagProxyFailover (0x80), FlagHedging (0x100), FlagInspect (0x200), FlagHAR (0x400), FlagMultiRead (0x800).

### Unsafe Hook Injection & Custom Phase Reordering
For advanced network analysis, users can alter execution topology:

- **Phase Reordering**: Custom phase sequences can be injected per-request via WithUnsafePhaseOrder(PhasePrep, PhaseDispatch, PhaseWAF...).
- **Unsafe Hooks**: Custom callbacks (UnsafeHook) can be attached before any phase execution to inspect or mutate intermediate http.Request or http.Response objects without heap allocation.

## 12. WAF Interception and Challenge Resolution

aoni integrates passive Web Application Firewall (WAF) challenge detection (resiliency/challenge).

### Cloudflare & DDoS Challenge Detection
- **Header & Payload Analysis**: Inspects response status codes (403, 503) and scans the initial 4 KB response body buffer for known challenge signatures (cf-challenge, ray id, <!doctype html>).
- **Zero-Copy Stream Rewinding (ExplicitBufferedBody)**: Pre-reads the initial 4 KB payload into e.Prefix. If no challenge is detected, the body reader is rewound via io.MultiReader(e.Prefix, e.Stream) without consuming or breaking the underlying HTTP response stream.
- **Automated Solver Interface (ChallengeSolver)**: If a challenge is detected, execution delegates to an external solver driver (e.g., browser automation) to complete the JS challenge and resume the request.

## 13. Distributed Load Balancing and DNS SRV

The resiliency/loadbalancer package provides client-side load balancing across backend instances.

### Selection Strategies & Health Tracking
- **Algorithms**: Implements RoundRobin, Random, and WeightedRoundRobin.
- **Health State Machine (health.Tracker)**: Monitors consecutive failure counts (maxFails). Transitions backend status:
  $$\text{Healthy} \longrightarrow \text{Degraded} \longrightarrow \text{Unhealthy} \xrightarrow{\text{Cooldown}} \text{Recovering}$$
- **Cooldown Recovery**: Unhealthy backends enter a cooldown period (retryAfter). Once expired, backends enter StatusRecovering and receive trial probe requests.

### Dynamic DNS SRV Discovery
- **RFC 2782 Resolution**: Resolves _service._proto.name records via net.LookupSRV.
- **Automatic Pool Updates**: Periodically re-queries SRV records and updates the active Balancer backend pool (SetBackendPool), adjusting weights based on SRV record priorities and weights.

## 14. Mechanical Sympathy and Zero-Allocation Geometry

To achieve 1.5M+ RPS throughput, aoni employs low-level Go runtime optimizations:

### Zero-Copy Unsafe Conversions (bytesconv)
- **B2S / S2B**: Converts byte slices to strings and vice versa without heap allocations using unsafe.String, unsafe.StringData, and unsafe.Slice.
- **ASCII Lowercasing**: Replaces bytes.ToLower with a 256-element lookup table (toLowerTable), executing in $O(1)$ time with zero branching.

### Bounds Check Elimination (BCE) and SWAR
- **BCE Hints**: Explicitly asserts slice bounds before loops (e.g., _ = src[n-1]) to allow the Go compiler's SSA pass to eliminate bounds checks within loops.
- **SWAR / Vectorization**: Uses SIMD-like SWAR (SIMD Within A Register) bitwise masks for token validation and header character checks.

### Cache Line Padding (False Sharing Prevention)
- Critical atomic counters (e.g., in HttpStreamPool, Rotator, and health.Tracker) are padded with 64-byte golang.org/x/sys/cpu.CacheLinePad structs.
- Prevents CPU L1/L2 cache line bouncing across concurrent CPU cores during high-frequency atomic operations.

## 15. Standard Library Bridging and SDK Integration

aoni seamlessly bridges into standard Go networking ecosystems via NewStdClient and NewTransport.

### Transport Adapter (aoni.Transport)
- Implements http.RoundTripper.
- Adapts third-party libraries (e.g., resty, aws-sdk-go-v2, custom API clients) into aoni's uTLS, JA4, ECH, and Happy Eyeballs pipeline.
- Automatically disables default http.Client.Jar to allow ProxyIsolatedCookieJar to manage cookies without double-handling.

### Request Context Propagation
- Context modifiers (WithContextModifier) allow attaching aoni.RequestModifier options directly to a standard context.Context.
- When passed through aoni.Transport, context modifiers are extracted and applied to the underlying aoni execution pipeline.

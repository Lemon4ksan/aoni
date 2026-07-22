# 🔮 Demystifying the "Voodoo": The Network Physics of Aoni

> _"Is this too much voodoo for our purposes? This is voodoo; the question is – is this too much?"_  
> — Terry Davis, Creator of TempleOS

To an outside observer, bypassing modern Deep Packet Inspection (DPI) firewalls and Web Application Firewalls (WAFs) looks like network necromancy. Developers often paste magic strings of cipher suites and network parameters into their code, hoping the remote gatekeepers will let their requests pass.

`aoni` rejects blind faith. This document demystifies the "voodoo" behind TLS, HTTP/2, and TCP/IP evasion, explaining the exact physical and protocol-level rules we manipulate to achieve browser-grade stealth.

## The Core Pillars of Evasion

Modern anti-bot systems (Cloudflare, Akamai, Imperva, DataDome) analyze traffic in layers. To remain invisible, your client must present a consistent, high-fidelity profile across all layers:

```
[ Layer 7: HTTP/2 Settings & Header Order ] -> (WAF Inspection)
                  ↓
[ Layer 6: TLS ClientHello Spec & JA4 ]    -> (TLS Fingerprint)
                  ↓
[ Layer 4: TCP/IP Window, TTL, & MSS ]     -> (p0f Fingerprint)
```

If your HTTP headers claim you are "Chrome on Windows," but your TLS extensions are sorted like Go's default `net/http` and your TCP packets carry a Linux TTL, the connection is instantly flagged and blocked.

Here is how `aoni` aligns all layers.

### 1. The TLS Ritual: ClientHello Specs & JA4 (`fingerprint/ja4`, `utls`)
* **The Problem:** Firewalls analyze the **JA3/JA4 fingerprint** of your TLS ClientHello. If your Go client connects using standard TLS (`crypto/tls`), it sends Go's default cipher suites and extension sequence. This is an instant bot signature.
* **The "Voodoo":** `aoni` uses `uTLS` under the hood to completely override the TLS client handshake (`dial.go`).
* **The Physics:** In `fingerprint/profiles/chrome/chrome.go`, we define `HelloChrome*`. This spec mimics Chrome's handshake down to the byte, including:
  * **GREASE Placeholders:** Chrome injects random "GREASE" values (`0x1a1a`, etc.) to test server extensibility. `aoni` emulates this behavior.
  * **Extension Shuffling:** Chrome shuffles its TLS extensions on every handshake. We use `utls.ShuffleChromeTLSExtensions` to replicate this exact entropy.
  * **Pure-Go `ja4` Package:** Our built-in `fingerprint/ja4` package calculates the TLS signature (e.g. `t13d1516h2_8daaf6152771_e5627efa2ab1`) directly from the active handshake, letting you monitor your TLS footprint in real-time.

### 2. The HTTP/2 Alchemy: Header Reordering & HPACK (`fingerprint/h2`)
* **The Problem:** HTTP/2 is binary and stateful. Firewalls inspect the order of pseudo-headers (e.g., `:method`, `:authority`, `:scheme`, `:path`). Standard clients serialize these in arbitrary order. Additionally, firewalls inspect the initial HTTP/2 `SETTINGS` and `PRIORITY` frames sent in the connection preface.
* **The "Voodoo":** `aoni` intercepts the HTTP/2 preface and re-encodes HPACK frames manually (`fingerprint/h2/h2.go`).
* **The Physics:**
  * **HPACK Frame Re-encoding:** In `fingerprint/h2/h2.go`, the `reorderH2Headers` function parses the HPACK-compressed headers, sorts them according to the target browser's exact specifications, and re-encodes them using `hpack.NewEncoder`. Because HPACK is stateful, this is done precisely during the initial handshake phase to prevent compression desynchronization.
  * **Frame Impersonation:** `FramedTransport` wraps the raw network socket and intercepts the HTTP/2 preface. It replaces the default Go settings frame with browser-specific settings (such as Chrome's massive `InitialWindowSize: 6291456` or `ConnectionFlow: 15663105`).

### 3. The OS-Level Necromancy: p0f & `setsockopt` (`fingerprint/p0f`)
* **The Problem:** Passive OS Fingerprinting (p0f) monitors Layer 4 TCP parameters. If your user-agent says "Windows 10" but your packet's TTL (Time-To-Live) is `64` (Linux default) and your TCP window size is unaligned, you are flagged as an emulator.
* **The "Voodoo":** We bypass high-level Go APIs and make direct system calls to the operating system kernel (`fingerprint/p0f/platform_linux.go`, `fingerprint/p0f/platform_windows.go`).
* **The Physics:**
  * **Socket Interception:** `aoni` registers a custom `SocketController` hook on the socket file descriptor (`fd`) immediately after dialing, before the first `SYN` packet is written.
  * **Syscall Modifications:** In `platform_linux.go` and `platform_darwin.go`, we use the `syscall` package to invoke `setsockopt`:
    - Sets the exact initial TTL (e.g. `128` for Windows, `64` for Linux/macOS).
    - Configures `SO_RCVBUF` to force specific TCP window sizes.
    - Enforces the Don't Fragment (`DF`) flag at the IP level (`IP_MTU_DISCOVER` on Linux, `IP_DONTFRAG` on macOS).

### 4. The Chaos Ward: Padding & Jitter (`fingerprint/padding.go`, `pipeline.go`)
* **The Problem:** Deep Packet Inspection (DPI) systems use machine learning models trained on packet lengths and transaction intervals. A bot that behaves with robotic, sub-millisecond precision or sends static-size request payloads is easily separated from human users.
* **The "Voodoo":** Injecting randomized, semantic-free noise and execution jitter into the pipeline.
* **The Physics:**
  * **TCP MSS Limiting:** The `applyMSSLimit` helper limits the TCP Maximum Segment Size (MSS) (`pipeline.go`). This forces the operating system to fragment large TLS payloads into smaller chunks, disrupting signature-based length detection of the ClientHello.
  * **Randomized Header Padding:** In `fingerprint/padding.go`, we generate random cryptographical noise via `crypto/rand` and append it to headers selected randomly from CDN/Cloud tracing pools (such as `CF-RAY` or `X-Amz-Trace-Id`). To a firewall, this padding looks like legitimate diagnostic metadata, but it effectively randomizes the total request payload size.
  * **DPI Jitter:** Introduces randomized micro-delays between writing request headers and writing the body, breaking predictable timing signatures analyzed by DPI hardware (`pipeline.go`).

### 5. Proxy Insulation: Cookie & TLS Session Ticket Partitioning (`cookie/`, `netutil/proxy`)
* **The Problem:** When rotating through proxy exit IPs, firewalls can correlate and track client sessions across different IP addresses if the client reuses the same TLS Session Ticket or Cookie Jar.
* **The "Voodoo":** Partitioning and isolating TLS session ticket caches and cookies per proxy key.
* **The Physics:**
  * **Proxy-Isolated Cookie Jar (`cookie/jar.go`):** `ProxyIsolatedJar` maintains an isolated `http.CookieJar` instance for each proxy address key stored in the request context.
  * **Proxy-Aware Session Cache (`netutil/proxy/session.go`):** `SessionCache` wraps the uTLS session ticket manager and automatically invalidates or isolates cached TLS connection tickets whenever the target proxy changes. This guarantees zero session leakage across exit nodes.

## 📦 Domain Modules & Their Mechanics

### 1. `realtime/ws` (WebSocket Transport)
* **The Physics:** Handshakes WebSocket connections over custom-negotiated TLS or HTTP/2 transport (`realtime/ws/websocket.go`).
* **Under the Hood:**
  * For standard TLS handshakes, it utilizes `uTLS` to spoof browser-specific ClientHello signatures.
  * For HTTP/2 Extended CONNECT ([RFC 8441]), instead of opening a raw TCP connection, it sends a single HTTP/2 `CONNECT` request with the `:protocol` pseudo-header set to `websocket`. This multiplexes the WebSocket connection as a single stream inside an existing HTTP/2 connection, completely avoiding extra TCP handshakes and bypassing firewalls.

### 2. `realtime/socketio` (Socket.IO v5 Client)
* **The Physics:** Establishes Engine.IO (v4) / Socket.IO (v5) client connections with automatic upgrading and state synchronization (`realtime/socketio/socketio.go`).
* **Under the Hood:**
  1. **Handshake:** Performs an HTTP handshake via the parent client to negotiate protocol version, obtain a Session ID (`sid`), and determine transport upgrades.
  2. **Upgrading:** Initiates a parallel WebSocket connection. Once a WebSocket handshake completes successfully, it immediately transitions the active stream from HTTP long polling to WebSocket frames.
  3. **Liveness:** Spawns a background goroutine coordinating the ping-pong heartbeat cycle. If a ping response is not received within the `pingTimeout` window, the connection triggers automatic reconnection.
  4. **Reconnection:** Employs an exponential backoff algorithm with randomized jitter to prevent the "thundering herd" problem on target servers.

### 3. `telemetry/inspector` (Auditing & Live Traffic Replay)
* **The Physics:** Intercepts and replicates HTTP request/response payloads without modifying or locking active connections (`telemetry/inspector/inspector.go`).
* **Under the Hood:**
  1. Captures outgoing request metadata, headers, and redacted sensitive fields.
  2. Wraps `resp.Body` in a high-performance multi-read buffer (`internal/io/io.go`). As the caller reads the stream, the data is concurrently mirrored to an in-memory buffer.
  3. Serves a local, embedded real-time web dashboard displaying live Server-Sent Events (SSE), JA4 reports, timing metrics, and cURL commands.

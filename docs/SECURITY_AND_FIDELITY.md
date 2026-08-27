# 🛡️ Protocol Security, Edge Cases & Chromium Fidelity Specification

This document details the security model, protocol vulnerability mitigations, desynchronization guards, and Chromium-grade fidelity invariants implemented across **HTTP/1.1**, **HTTP/2**, and **HTTP/3 (QUIC)** in **`aoni`**, mapped against the C++ source code of Google Chrome (`net/` in Chromium).

## 1. Architectural Security Philosophy

Unlike traditional backend HTTP clients, modern web browsers operate in an adversarial execution environment:
1. **Untrusted Origins**: Web clients connect to potentially malicious edge nodes, servers, and proxies.
2. **Intermediate Middleboxes**: Corporate firewalls, DPI boxes, and transparent CDNs actively inspect, fragment, and rewrite frames.
3. **Multi-Tenant State Isolation**: Browsers isolate cookies, DNS caches, TLS session tickets, and socket pools to prevent cross-site side-channel tracking and timing attacks.

`aoni` mirrors Chromium's multi-layered defense architecture across Layer 4 (TCP/UDP), Layer 6 (TLS/BoringSSL), and Layer 7 (HTTP/1, H2, H3).

## 2. HTTP/1.1: Smuggling, Desynchronization & Socket Integrity

```
[ Malicious Server / CDN ] ──► [ Intermediary Proxy ] ──► [ aoni L7 Parser ]
   • Conflicting Content-Length    • Desync Point           • Truncation Guard
   • Chunked + CL (TE.CL)         • Request Smuggling      • Control Character Scan
   • CRLF Injection / Null-Byte   • Cache Poisoning        • 408 Retry Pipeline
```

### 2.1. HTTP Response Splitting & Desync Guard (RFC 9112 §6.3)
* **Vulnerability (TE.CL / CL.TE)**: If a response contains both `Transfer-Encoding: chunked` and `Content-Length: N`, parsers that prioritize `Content-Length` over `chunked` leave leftover chunk bytes in the keep-alive socket buffer. Subsequent requests on the reused socket parse those trailing bytes as HTTP headers of a different user transaction.
* **Chromium Defense** (`net/http/http_response_headers.cc`, `http_stream_parser.cc`):
  * When `Transfer-Encoding: chunked` is present, any `Content-Length` header is **strictly stripped** before passing headers to the application.
  * Any unrecognized `Transfer-Encoding` token triggers `ERR_INVALID_HTTP_RESPONSE`.
* **`aoni` Implementation**: `internal/fast/h1engine` and `internal/pipeline` automatically strip `Content-Length` whenever chunked encoding is active.

### 2.2. Conflicting & Multiple `Content-Length` Headers
* **Vulnerability**: Responses with multiple disparate `Content-Length` headers (e.g. `Content-Length: 42` and `Content-Length: 0`) are used to bypass WAFs and cause desync in keep-alive pipelines.
* **Chromium Defense**: `HttpResponseHeaders::GetContentLength()` asserts that all `Content-Length` headers parse to the exact same positive integer. Discrepancies trigger `ERR_RESPONSE_HEADERS_MULTIPLE_CONTENT_LENGTH` and immediately destroy the socket.
* **`aoni` Implementation**: `pipeline.ValidateResponse` enforces strict integer equivalence across all duplicate `Content-Length` entries, aborting the transaction on mismatch.

### 2.3. Multiple & Conflicting `Location` Headers
* **Vulnerability**: 3xx redirect responses with multiple `Location` headers can trick HTTP engines into following an attacker-controlled secondary redirect.
* **Chromium Defense**: Rejects the response with `ERR_RESPONSE_HEADERS_MULTIPLE_LOCATION`.
* **`aoni` Implementation**: The redirect engine verifies that exactly one `Location` header is present.

### 2.4. Control Character & Null-Byte (`\0`) Header Poisoning
* **Vulnerability**: Injected `\r`, `\n`, or `\x00` bytes in header names or values allow breaking HTTP frame delimiters.
* **Chromium Defense**: `HttpUtil::IsValidHeaderName()` and `IsValidHeaderValue()` reject any bytes below `0x20` (except `\t`).
* **`aoni` Implementation**: `bytesconv.ValidateHeaderChars` performs SIMD/SWAR-vectorized byte boundary validation with zero heap allocation.

### 2.5. Strict Body Truncation & Keep-Alive Hygiene
* **Edge Case**: A server declares `Content-Length: 100` but transmits 150 bytes before closing or idling. If unread bytes linger in the buffer, subsequent requests on the keep-alive socket will be corrupted.
* **Chromium Defense**: Wraps streams in `io::LimitReader(100)` and drains exactly $N$ bytes. If trailing garbage is detected upon stream closure, the socket is destroyed rather than returned to the pool.
* **`aoni` Implementation**: Strict truncation using `io.LimitReader` and buffer draining in `h1engine`.

### 2.6. HTTP 408 Request Timeout / Keep-Alive Race Condition
* **Edge Case**: An idle socket in the keep-alive pool is closed by the server due to its internal timeout at the exact microsecond the client writes a new request.
* **Chromium Defense** (`net/http/http_network_transaction.cc`): If `ERR_CONNECTION_RESET` or `408 Request Timeout` is received on a reused socket prior to receiving response headers, Chromium transparently retries the request on a fresh connection.
* **`aoni` Implementation**: `resiliency/recovery` invalidates the idle pool entry, appends `Connection: close`, and transparently replays the request.

## 3. HTTP/2: Binary Framing, HPACK & Resource Exhaustion

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                 HTTP/2 Threat Surface                                  │
├────────────────────────────────────────────────────────────────────────────────────────┤
│  • Rapid Reset (CVE-2023-44487)     ──► RST_STREAM Flood Rate Limiting                │
│  • CONTINUATION Flood (CVE-2024-27983) ──► Strict MaxHeaderListSize Cap (256 KB)       │
│  • HPACK Dynamic Table Bomb         ──► Byte-by-Byte Decompression Cap                 │
│  • Empty DATA Frame Loops           ──► Consecutive Empty Frame Tear-Down              │
│  • Misdirected Request (421)        ──► IP Pool Invalidation & Uncoalesce Retry        │
│  • Server Push Removal              ──► SETTINGS_ENABLE_PUSH = 0                       │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### 3.1. Rapid Reset Attack (CVE-2023-44487)
* **Vulnerability**: Opening hundreds of streams with `HEADERS` and immediately canceling them with `RST_STREAM` bypasses concurrency limits while forcing the engine to allocate and free stream contexts, driving CPU to 100%.
* **Chromium Defense** (`net/spdy/spdy_session.cc`): Tracks consecutive resets and rate-limits `RST_STREAM` frames, closing the session with `ENHANCE_YOUR_CALM` if thresholds are exceeded.
* **`aoni` Implementation**: `internal/fast/h2engine/conn.go` enforces `maxConsecutiveControlFrames = 1000` to prevent control frame flooding.

### 3.2. CONTINUATION Flood (CVE-2024-27983)
* **Vulnerability**: Transmitting a `HEADERS` frame without `END_HEADERS`, followed by an infinite stream of `CONTINUATION` frames. Parsers buffer these in memory, triggering out-of-memory crashes.
* **Chromium Defense**: Limits total header block size across all continuation frames to `SETTINGS_MAX_HEADER_LIST_SIZE` (262,144 bytes = 256 KB). Exceeding this limit terminates the stream with `PROTOCOL_ERROR`.
* **`aoni` Implementation**: `h2engine/frames.go` tracks accumulated `rawHeaders` byte count and aborts with `ErrFrameTooLarge` if the threshold is exceeded.

### 3.3. HPACK Decompression Bomb
* **Vulnerability**: Filling the HPACK dynamic table with 4096-byte strings and emitting headers referencing them hundreds of times. A 50-byte packet expands to 50 MB in memory.
* **Chromium Defense**: Tracks decompressed byte totals on every single decoded header token, enforcing a strict 256 KB ceiling.
* **`aoni` Implementation**: `internal/fast/h2engine/hpack.go` verifies decoded byte volume inside the Huffman decoding loop.

### 3.4. Connection Coalescing Mismatch & HTTP 421 (RFC 9113 §9.1.2)
* **Edge Case**: Requests to `a.com` and `b.com` are coalesced onto a single HTTP/2 connection (sharing IP and TLS SAN), but the server cannot serve `b.com` and replies with `421 Misdirected Request`.
* **Chromium Defense**: Evicts the host mapping from the connection pool, marks the endpoint uncoalesceable, and retries on a dedicated socket. Per RFC 9113, 421 guarantees the server did not execute the request, making it safe to retry non-idempotent methods (`POST`, `PUT`).
* **`aoni` Implementation**: `resiliency/recovery` evicts the host, sets `DisableAltSvc = true`, and re-executes the transaction on a fresh connection.

### 3.5. Deprecation & Removal of HTTP/2 Server Push
* **Vulnerability**: Server Push allows servers to inject unrequested scripts into client caches (Cache Poisoning) and track user sessions.
* **Chromium Action**: Server Push was completely removed in Chrome M106+. Chrome always advertises `SETTINGS_ENABLE_PUSH = 0`.
* **`aoni` Implementation**: `fingerprint/profiles/chrome/chrome.go` enforces `s.EnablePush = 0`.

## 4. HTTP/3 & QUIC: UDP Security, QPACK & Mobility

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                 HTTP/3 & QUIC Defenses                                 │
├────────────────────────────────────────────────────────────────────────────────────────┤
│  • Amplification Protection (3x limit) ──► Strict Address Validation (RFC 9000 §8)     │
│  • 0-RTT Anti-Replay / HTTP 425       ──► Safe-Method Filtering & 1-RTT Auto-Fallback  │
│  • QPACK Dynamic Table Blast           ──► QPACK Max Blocked Streams Cap               │
│  • Broken Alt-Svc / UDP Censorship    ──► Happy Eyeballs v3 (250ms Race to TCP/H2)     │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### 4.1. QUIC UDP Amplification Protection (RFC 9000 §8)
* **Vulnerability**: Attackers spoofing source IP addresses in `Initial` packets to weaponize servers as DDoS reflectors.
* **Chromium / QUICHE Defense**: Servers limit unvalidated response bytes to $3\times$ received bytes. Clients MUST pad `Initial` packets to at least 1200 bytes with `PADDING` frames.
* **`aoni` Implementation**: `internal/quic/wire` automatically pads outgoing `Initial` packets to $\ge 1200$ bytes.

### 4.2. 0-RTT Anti-Replay & HTTP 425 Too Early (RFC 8470)
* **Vulnerability**: 0-RTT Early Data lacks anti-replay guarantees. A captured packet containing a non-idempotent request could be replayed by an adversary.
* **Chromium Defense**:
  1. Only safe idempotent methods (`GET`, `HEAD`) are permitted in 0-RTT.
  2. If the server rejects 0-RTT with `425 Too Early`, Chromium sets `Disable0RTT = true` and replays the request in 1-RTT mode.
* **`aoni` Implementation**: `resiliency/recovery` intercepts status 425, strips `Early-Data` headers, and transparently retries in 1-RTT.

### 4.3. Broken Alt-Svc Exponential Backoff (RFC 7838)
* **Edge Case**: When middleboxes silently throttle or drop UDP packets on port 443, naive clients stall waiting for QUIC handshakes.
* **Chromium Defense**:
  * **Happy Eyeballs v3**: Races QUIC against TCP with a 250 ms delay timer.
  * **Exponential Cooldown**: Marks failed Alt-Svc endpoints broken for 5 minutes, doubling sequentially up to **48 hours**.
* **`aoni` Implementation**: `globalAltSvcCache` enforces the exact backoff formula:
  $$\text{Backoff} = \min\Big(48\text{ hours},\ 5\text{ minutes} \times 2^{\text{Fails} - 1}\Big)$$

## 5. Session Isolation: Network Isolation Keys (NIK / NAK)

```
[ Request Context ] ──► (TopFrameSite: "https://a.com", FrameSite: "https://b.com")
                                  │
         ┌────────────────────────┼────────────────────────┐
         ▼                        ▼                        ▼
  [ DNS Cache ]           [ TLS Tickets ]           [ Cookie Jar ]
  Key: NIK + Host         Key: NIK + Host           Key: NIK + Domain
```

To eliminate cross-site tracking, side-channel attacks, and cookie correlation across rotating proxy pools, `aoni` provides **`netutil/nik`**:
* **Compound Key Definition**:
  $$\text{NIK} = \langle \text{TopFrameSite},\ \text{FrameSite},\ \text{IsCrossSite} \rangle$$
* **Multi-Tenant Personas**: Allows hosting thousands of isolated "Virtual Browser Personas" in a single process without cross-tab session leakage.
* **CHIPS Integration**: Maps NIK context directly into RFC 6265bis Partitioned Cookie Jar lookups in [`cookie/jar.go`](../cookie/jar.go).

## 6. Comprehensive Defense & Fidelity Matrix

| Protocol | Attack / Edge Case Vector | Chromium C++ Defense (`net/`) | `aoni` Go Defense |
| :--- | :--- | :--- | :--- |
| **H1** | Desync (TE.CL / CL.TE) | Strip CL on chunked | `h1engine` / `pipeline` strip CL |
| **H1** | Multiple `Content-Length` | `ERR_MULTIPLE_CONTENT_LENGTH` | `pipeline.ValidateResponse` strict check |
| **H1** | Multiple `Location` | `ERR_MULTIPLE_LOCATION` | Redirect validator abort |
| **H1** | CRLF & Null-Byte Injection | `HttpUtil::IsValidHeaderValue` | `bytesconv.ValidateHeaderChars` (SWAR) |
| **H1** | Keep-Alive Socket Poisoning | Strict body truncation + drain | `io.LimitReader` + discard socket on excess |
| **H1** | Keep-Alive Timeout Race (408) | Auto-retry on fresh socket | `resiliency/recovery` 408 replay |
| **H2** | Rapid Reset (CVE-2023-44487) | Rate-limit `RST_STREAM` frames | `maxConsecutiveControlFrames = 1000` |
| **H2** | CONTINUATION Flood (CVE-2024-27983) | 256 KB Header Block ceiling | `ErrFrameTooLarge` on > 256 KB buffer |
| **H2** | HPACK Decompression Bomb | Token-by-token decompression cap | Huffman decoder byte accumulation check |
| **H2** | Empty DATA Frame Loops | Detect empty frame loops | Control frame cycle counter |
| **H2** | Misdirected Request (421) | Invalidate IP pooling & replay | `resiliency/recovery` 421 auto-fallback |
| **H2** | Server Push Vulnerabilities | Deprecated & removed | `SETTINGS_ENABLE_PUSH = 0` |
| **H3** | QUIC Amplification Attack | Validate address (3x limit) | `Initial` padded to $\ge 1200$ bytes |
| **H3** | 0-RTT Replay Attack | Safe methods only + 425 retry | `resiliency/recovery` 425 1-RTT fallback |
| **H3** | QPACK Head-of-Line Deadlock | Bound `QpackBlockedStreams` (100) | `s.QpackBlockedStreams = 100` |
| **H3** | UDP Drop / Silent Censorship | Happy Eyeballs v3 + 48h Backoff | Happy Eyeballs v3 (250ms) + RFC 7838 backoff |
| **State**| Cross-Site Session Tracking | Network Isolation Keys (NIK) | `netutil/nik` + Partitioned Jar |

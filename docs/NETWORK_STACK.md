# Network Stack Specification

## 1. Engine and Dispatching

1. **HTTP/1.1 Engine**: Built on `fasthttp` byte-buffer architecture.
2. **HTTP/2 Engine**: Multiplexer providing control over HPACK tables and frame serialization.
3. **HTTP/3 Engine**: Built on pure-Go internal QUIC engine (RFC 9000), implementing QPACK (RFC 9204).

### Protocol Racing (Happy Eyeballs v3, RFC 8305)
* Initiates HTTP/3 connection immediately upon `Alt-Svc` advertisement.
* Starts a 250 ms delay timer.
* If timer expires before HTTP/3 completes, parallel TCP (HTTP/2 or HTTP/1.1) is launched.
* The first protocol to complete the handshake wins; the slower context is canceled.

### Broken Alt-Svc Exponential Backoff (RFC 7838)
$$\text{Backoff} = \min\Big(48\text{ hours},\ 5\text{ minutes} \times 2^{\text{Fails} - 1}\Big)$$
A single successful HTTP/3 transaction clears the failure record.

## 2. Stealth & Evasion Mechanisms

* **TLS 1.3 ECH (RFC 9460)**: Encrypts SNI by querying DNS HTTPS (Type 65) via DoH/DoQ.
* **uTLS Fingerprint**: Emulates ClientHello profiles (Chrome, Firefox, Safari), extension ordering, and GREASE.
* **p0f Spoofing**: Modifies raw socket parameters (TTL, DF bit, `SO_RCVBUF`) to match target OS profiles.
* **Client Hints**: Generates High-Entropy `Sec-CH-UA` header structures.
* **Packet Fragmentation**: Splits TCP payloads and injects CDN padding headers to obfuscate packet length analysis.

## 3. Resilience and Auto-Recovery

* **HTTP 421 Misdirected Request (RFC 9113)**: Evicts host mapping from connection pool, disables `Alt-Svc`, and re-executes on a fresh connection.
* **HTTP 408 Request Timeout (RFC 9112)**: Detects idle socket race conditions and replays request on a new socket.
* **HTTP 425 Too Early (RFC 8470)**: Disables 0-RTT, strips `Early-Data` headers, and replays in 1-RTT.
* **Response Smuggling Guard**: Strips `Content-Length` if `Transfer-Encoding: chunked` is present. Rejects conflicting `Content-Length` or `Location` headers.
* **Strict Body Truncation**: Wraps buffered streams in `io.LimitReader(N)` and discards trailing garbage bytes.

## 4. Transport & Proxies

* **Adaptive Proxy Timeout**: $\text{Timeout} = \max(8\text{s}, \min(30\text{s}, p95\text{RTT} \times 4.0))$.
* **BCP 38 Ingress Filtering**: Drops payloads from unroutable Martian address space (RFC 2827).
* **MASQUE & TUN**: Layer 3 IP bridging over HTTP/3 (RFC 9298) and native TUN drivers. MSS Clamping prevents path MTU black-holing.

## 5. State Isolation & DNS

* **W3C No-Vary-Search**: Normalizes query parameters by stripping tracking params (`utm_*`, `fbclid`).
* **CHIPS (RFC 6265bis)**: Keys cookies via `(Top-Level Site, Cookie Domain, Proxy)` triple key.
* **DNS Engine**: Supports DoH (RFC 8484), DoT (RFC 7858), DoQ (RFC 9250), EDNS0 Padding (RFC 7830), and ECS (RFC 7871).

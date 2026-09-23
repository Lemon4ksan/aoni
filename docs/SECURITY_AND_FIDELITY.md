# Protocol Security, Fidelity & Stealth Specification

This document details the multi-layer protocol security invariants, vulnerability mitigations, state isolation mechanisms, and stealth evasion architecture implemented in **`aoni`**, mapped against the Chromium network stack (`net/`) and modern IETF RFC / W3C specifications.

---

## Threat Model & Engineering Philosophy

Web clients in high-throughput and adversarial deployments face dual challenges:

1. **Hostile Network & Server Attacks**: Malicious origin servers and intermediate middleboxes attempting HTTP request smuggling, protocol desynchronization, stream multiplexing denial-of-service (Rapid Reset, CONTINUATION floods), memory exhaustion (decompression bombs, unbounded streams), SSRF exploitation, and cross-session credential leakage.
2. **Adversarial Network Inspection & Fingerprinting**: Deep Packet Inspection (DPI), stateful firewall censors, WAF challenge systems (Cloudflare, DataDome, Akamai), and anti-bot surveillance engines analyzing TLS ClientHello signatures (JA3/JA4), TCP/IP stack parameters (p0f), and HTTP header formatting to identify and block automated automation engines.

```text
                                  ADVERSARIAL ENVIRONMENT
 ┌────────────────────────────────────────────────────────────────────────────────────────┐
 │  [ Malicious Server / CDN ] ──► [ Intermediary / DPI Middlebox ] ──► [ aoni Engine ]  │
 │     • Conflicting Content-Length       • Stateful Packet Inspection     • Truncation Guard   │
 │     • Chunked + CL (TE.CL Desync)      • TLS ClientHello Interception   • SIMD/SWAR Scanners │
 │     • Rapid Reset (CVE-2023-44487)     • TCP/IP Fingerprinting (p0f)    • ECH Decryption     │
 │     • CONTINUATION Bomb                • Timing Correlation             • SSRF Filter        │
 │     • Cloud Metadata Probing           • Cross-Tenant Session Snooping  • NIK & CHIPS        │
 └────────────────────────────────────────────────────────────────────────────────────────┘
```

`aoni` achieves **100% Chromium-grade network fidelity** and zero behavioral drift while strictly enforcing low-level protocol security guarantees.

---

## 1. L7 Protocol Invariants & Desynchronization Defenses

### 1.1 HTTP/1.1 Invariants (RFC 9112 & RFC 9110)

* **Response Splitting & Desynchronization (TE.CL / CL.TE)**: If `Transfer-Encoding: chunked` is present in any incoming or outgoing message, `Content-Length` headers are automatically stripped. If multiple contradictory `Transfer-Encoding` values or conflicting chunked codings appear, the transaction is immediately aborted.
* **Conflicting `Content-Length` Headers**: Receiving multiple disparate `Content-Length` values constitutes an unrecoverable framing ambiguity under RFC 9110 §8.6. The parser treats this condition as a fatal framing failure and closes the underlying socket without returning the stream to the connection pool.
* **Control Character & CRLF Sanitization**: All header keys, values, and status lines are validated via SIMD/SWAR vectorized scans (`bytesconv.ValidateHeaderChars`). Any byte value $< \text{0x20}$ (excluding horizontal tab `\t` / `0x09`), null-bytes (`\x00`), or carriage-return/line-feed (`\r`, `\n`) sequences immediately reject the request, preventing CRLF injection and header smuggling attacks.
* **Keep-Alive Socket Race Auto-Recovery (HTTP 408)**: When an origin server closes an idle keep-alive socket concurrently with the client transmitting a request, standard clients fail with EOF or HTTP 408 Request Timeout. `aoni` intercepts HTTP 408 responses on pooled connections and transparently re-executes the transaction once on a fresh socket.
* **Strict Body Truncation & Early EOF Protection**: All incoming response bodies are bounded by an `io.LimitReader` configured to the announced `Content-Length` (or `MaxResponseSize`). Any unannounced trailing bytes are dropped, and early connection termination before reading the expected byte count triggers `io.ErrUnexpectedEOF`.

### 1.2 HTTP/2 Invariants & Anti-DoS Defenses (RFC 9113)

* **Rapid Reset Mitigation (CVE-2023-44487)**: Malicious servers or peers can exploit rapid multiplexed stream cancellations to exhaust CPU resources. `aoni` maintains a sliding-window control frame budget (`maxConsecutiveControlFrames = 1000`). If excessive `RST_STREAM` or `PING` frames are received without intervening data frames, the connection is terminated with `GOAWAY`.
* **CONTINUATION Flood Mitigation (CVE-2024-27983)**: The HTTP/2 framing layer enforces a hard cap of 256 KB (`SETTINGS_MAX_HEADER_LIST_SIZE`) across fragmented `CONTINUATION` frame sequences. If an unclosed header block exceeds this limit, parsing halts immediately with `ErrFrameTooLarge`.
* **HPACK Huffman Decompression Bomb Defense**: Malicious HPACK payloads can expand small compressed byte sequences into gigabytes of uncompressed header strings. `aoni` accounts for decompressed byte budgets within the inner Huffman decoding loop, enforcing a strict 256 KB cap per transaction.
* **Server Push Policy**: Following Chromium's security model, `SETTINGS_ENABLE_PUSH = 0` is enforced by default to prevent cache poisoning. When explicit browser persona emulation requires it, Server Push can be toggled via `option.WithH2ServerPush`.
* **HTTP 421 (Misdirected Request) Auto-Recovery (RFC 9113 §9.4)**: When connection reuse routes a request for Host B over an existing TLS connection authenticated for Host A, and the server returns HTTP 421, `aoni` automatically evicts the host mapping from the socket pool, invalidates the cert cache entry, and retries the request over a fresh connection dedicated to Host B.
* **Chromium-Grade Pseudo-Header Ordering**: Outgoing HTTP/2 requests strictly format pseudo-headers in the canonical order (`:method`, `:authority`, `:scheme`, `:path`), matching Chromium and Firefox network stack signatures.

### 1.3 HTTP/3 & QUIC Defenses (RFC 9000, RFC 9114, RFC 9204)

* **UDP Amplification Mitigation (RFC 9000 §8.1)**: To prevent an endpoint from being weaponized in reflected denial-of-service attacks, outgoing QUIC `Initial` packets are padded to at least 1200 bytes using `PADDING` frames.
* **0-RTT Anti-Replay Defense (RFC 8470)**: 0-RTT Early Data is inherently vulnerable to network replay attacks. If an HTTP/3 or TLS 1.3 server returns HTTP 425 (Too Early), the resiliency layer intercepts the response, invalidates 0-RTT state for that origin, strips `Early-Data` headers, and automatically replays the request in a standard 1-RTT handshake.
* **QPACK Dynamic Table Protection**: Field section compression enforces strict memory limits on the QPACK dynamic table (`SETTINGS_QPACK_MAX_TABLE_CAPACITY`), and decoder streams enforce flow control to prevent memory exhaustion from stalled acknowledgment streams.
* **Broken `Alt-Svc` Exponential Cooldown (RFC 7838)**: If UDP/QUIC traffic to an advertised `Alt-Svc` endpoint fails or is throttled by intermediate firewalls, `aoni` applies an exponential backoff:
  $$\text{Cooldown} = \min\Big(48\text{ hours},\ 5\text{ minutes} \times 2^{\text{Fails} - 1}\Big)$$
  During cooldown, requests fall back cleanly to TCP (HTTP/2 or HTTP/1.1). A single successful QUIC transaction clears the failure counter.
* **Happy Eyeballs v3 (RFC 8305)**: When `Alt-Svc` indicates HTTP/3 availability, an HTTP/3 dial is initiated alongside a 250 ms delay timer. If HTTP/3 does not establish first bytes within 250 ms, parallel TCP (HTTP/2 or HTTP/1.1) racing is launched. The fastest protocol wins, and the slower attempt is canceled without socket leaks.

---

## 2. Multi-Tenant State Isolation & Anti-Tracking

### 2.1 Network Isolation Keys (NIK)

To eliminate cross-site tracking and side-channel timing attacks across origins, `aoni` implements Chromium-grade **Network Isolation Keys (NIK)**. State is partitioned by the tuple of `(TopFrameSite, FrameSite)`:

```text
   [ Request Context ] ──► (TopFrameSite: "https://a.com", FrameSite: "https://b.com")
                                     │
            ┌────────────────────────┼────────────────────────┐
            ▼                        ▼                        ▼
     [ DNS Cache ]           [ TLS Tickets ]           [ Connection Pool ]
     Key: NIK + Host         Key: NIK + Host           Key: NIK + Target
```

1. **Partitioned DNS Cache**: Prevents malicious third-party scripts or origins from determining if a user previously visited another origin via DNS query latency measurement.
2. **Partitioned TLS Session Cache & Tickets**: Pre-shared keys (PSK) and session tickets are partitioned under the NIK, preventing cross-origin session resumption tracking.
3. **Partitioned Socket Pools**: TCP and QUIC connections established in context A are strictly prohibited from being reused for context B, eliminating cross-tenant connection timing leaks.

### 2.2 Proxy-Isolated Cookie Jars (`ProxyIsolatedCookieJar`)

In rotating proxy or multi-tenant scraping environments, sharing cookies across disparate proxy IP addresses leads to account bans, session cross-contamination, and IP-correlation poisoning.

`ProxyIsolatedCookieJar` implements a three-dimensional storage index:
$$\text{Cookie Storage Key} = \Big(\text{Top-Level Site},\ \text{Cookie Domain},\ \text{Proxy Address}\Big)$$

* **Proxy Boundary**: Cookies set while routing through `proxy-us-1.net` are strictly isolated and never transmitted when sending requests through `proxy-de-1.net` or a direct dial.
* **CHIPS (RFC 6265bis)**: Cookies carrying the `Partitioned` attribute are isolated by their top-level site partition key.
* **RFC 6265 Sorting**: Cookies are sorted by longest path match first, breaking ties using the earliest creation timestamp.

### 2.3 Cross-Domain Credential & Header Scrubbing (RFC 9110 §15.4)

When following redirects across different origins or subdomains, sensitive credentials and authorization tokens are automatically scrubbed:

* **Sensitive Headers Stripped on Cross-Origin Hops**:
  `Authorization`, `Proxy-Authorization`, `Cookie`, `Cookie2`, `X-Api-Key`, `X-Auth-Token`, `X-Access-Token`, `X-Secret`, `X-Client-Secret`, `Api-Key`, `Token`, `Secret`, `Private-Key`.
* **Scheme Downgrade Stripping**: If a redirect downgrades from `https://` to `http://`, the `Referer` header is unconditionally deleted to prevent leaking sensitive URLs over plaintext networks.
* **Method & Representation Header Stripping**: Upon 301 (Moved Permanently), 302 (Found), or 303 (See Other) redirects converting a `POST`/`PUT` into a `GET` request, all payload representation headers are stripped:
  `Content-Type`, `Content-Length`, `Content-Encoding`, `Content-Language`, `Content-Location`, `Digest`.

---

## 3. Cryptographic Security & TLS Stealth Fidelity

### 3.1 TLS 1.3 Encrypted Client Hello (ECH / RFC 9460)

Traditional TLS handshakes expose the destination Server Name Indication (SNI) in plaintext, allowing intermediate firewalls, ISPs, and eavesdroppers to inspect and censor traffic.

`aoni` natively integrates **Encrypted Client Hello (ECH)**:

```text
   [ aoni Client ] ──► (DoH / DoQ Query for HTTPS RR Type 65) ──► [ Encrypted DNS ]
           │                                                               │
           ▼                                                               ▼
   [ Outer SNI: cloudflare.com ] ──► (Encrypted ClientHello) ──► [ Inner SNI: secret.org ]
```

1. **Out-of-band Discovery**: Automatically queries DNS HTTPS Resource Records (Type 65) via DoH or DoQ to fetch the target origin's `echconfig` payload and public encryption keys.
2. **Inner & Outer ClientHello**: The actual SNI (`secret.org`) and application ALPN are encrypted inside the `InnerClientHello` using HPKE (RFC 9180). The `OuterClientHello` presents a benign, shared public facade (`cloudflare.com`).
3. **ECH Fallback**: If the server rotates keys and rejects the inner handshake with `ech_required`, `aoni` retrieves the updated retry configuration from the server's cleartext extension and transparently retries.

### 3.2 uTLS & Browser Persona Emulation (JA3 / JA4 / JA4H)

Web Application Firewalls analyze the exact byte structure of the TLS `ClientHello` packet to distinguish standard browsers from bots. `aoni` provides pure-Go browser emulation via `utls`:

* **Exact Specification Matching**:
  - Emulates complete ClientHello specs for **Google Chrome**, **Mozilla Firefox**, and **Apple Safari**.
  - Matches exact Cipher Suite IDs and ordering.
  - Reproduces Extension IDs, Supported Elliptic Curves (`x25519`, `secp256r1`), Point Formats, and Signature Algorithms.
  - Generates realistic GREASE (Generate Random Extensions And Sustain Extensibility, RFC 8701) values for ciphers, extension types, and supported groups.
* **Pure-Go JA3 & JA4 Fingerprinting**:
  - Generates verifiable JA3 and JA4 fingerprint hashes natively for every TLS handshake without CGO dependencies.
  - Exposes `option.WithJA4Callback` for telemetry, auditing, and evasion verification.

### 3.3 Certificate Pinning & Protocol Minimums

* **SPKI SHA-256 Pinning**: Allows locking origins to specific Subject Public Key Info (SPKI) cryptographic hashes, defeating rogue Certificate Authorities and intermediate SSL inspection proxies.
* **Strict TLS 1.2+ Deprecation (RFC 8996 & RFC 7525)**: TLS 1.0 and TLS 1.1 are permanently disabled. Insecure ciphers (RC4, 3DES, CBC-mode ciphers without AEAD) are rejected.

---

## 4. L4 Transport, DPI Evasion & Packet-Level Fidelity

### 4.1 p0f TCP/IP OS Stack Emulation

Modern stateful packet inspection systems (e.g. p0f v3) inspect low-level Layer 4 TCP SYN packets. If a client sends a Chrome User-Agent on Windows, but the TCP SYN packet has a Linux TTL of 64 or a non-standard window size, WAFs flag the client as an automated bot.

`aoni` manipulates raw socket options via low-level OS system calls (`netutil/netdial`):

| Parameter | Windows 11 (Chrome) | macOS Sonoma (Safari) | Ubuntu (Firefox) |
| :--- | :--- | :--- | :--- |
| **IP TTL** | `128` | `64` | `64` |
| **Don't Fragment (DF)** | Enabled (`IP_DONTFRAG`) | Enabled (`IP_DONTFRAG`) | Enabled (`IP_DONTFRAG`) |
| **Initial TCP Window** | `64240` (`SO_RCVBUF`) | `65535` (`SO_RCVBUF`) | `65495` (`SO_RCVBUF`) |
| **TCP SACK Permitted** | Yes | Yes | Yes |
| **TCP Timestamp** | Disabled by default | Enabled | Enabled |

### 4.2 Middlebox & DPI Evasion via Packet Fragmentation & Jitter

* **TCP Payload Chunking (`fragment.NewFragmentedConn`)**: State-level firewalls and shallow DPI filters inspect only the initial TCP packet for SNI strings or HTTP method words. `aoni` can split the initial TLS ClientHello across small TCP segments (e.g. 2–5 bytes for the first chunk), forcing middlebox inspection buffers to either reassemble the full stream or pass the packet uninspected.
* **Pre-Dial TCP Delay Jitter**: Injects randomized millisecond jitter before initiating outbound socket handshakes, disrupting statistical timing correlation used by network surveillance tools.
* **MSS Clamping**: Enforces Maximum Segment Size clamping on VPN, MASQUE, and TUN tunnels, preventing Path MTU Discovery (PMTUD) black-holing and packet drops.
* **BCP 38 Ingress Filtering (RFC 2827)**: Validates incoming packets, dropping unroutable Martian or Bogon addresses.

---

## 5. SSRF Guard & Network Boundary Enforcement

Server-Side Request Forgery (SSRF) represents a critical risk when an HTTP client fetches user-supplied URLs. `aoni` provides an integrated **`SSRFGuard`** that intercepts requests prior to socket establishment:

```text
   [ User URL: "http://internal-host.com" ]
                       │
                       ▼
      [ Pre-Dial DNS Resolution ] ──► (Yields IP: 169.254.169.254)
                       │
                       ▼
       [ SSRF Boundary Filter ] ──► BLOCKED! (ErrSSRFBlocked)
```

### 5.1 Pre-Dial Evaluation & DNS Rebinding Mitigation

Standard HTTP libraries resolve DNS in `net/http` and dial the host string, creating a **Time-of-Check to Time-of-Use (TOCTOU) DNS Rebinding** vulnerability where an attacker's DNS server returns a public IP during validation, but a private IP during socket connection.

`aoni` resolves DNS *before* dialing, inspects every candidate IP address against the blacklist, and **dials the exact vetted IP address directly**, permanently eliminating DNS rebinding.

### 5.2 Blacklist Evaluation Matrix

| Category | Address Space | Description |
| :--- | :--- | :--- |
| **RFC 1918 Private** | `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | Internal corporate and private networks. |
| **RFC 1122 Loopback** | `127.0.0.0/8`, `::1` | Local host interfaces. |
| **RFC 3927 Link-Local** | `169.254.0.0/16`, `fe80::/10` | Auto-configured link-local interfaces. |
| **Cloud Metadata** | `169.254.169.254`, `fd00:ec2::254` | AWS, GCP, Azure, and OpenStack IMDS services. |
| **RFC 6598 Carrier-Grade** | `100.64.0.0/10` | Shared address space for CGNAT. |
| **IPv4-Mapped IPv6** | `::ffff:0:0/96` (e.g. `::ffff:127.0.0.1`) | IPv6 representations of IPv4 private addresses. |

---

## 6. DNS Privacy & Tamper Resistance

* **Encrypted DNS Transports**: Native support for **DNS over HTTPS (DoH / RFC 8484)**, **DNS over TLS (DoT / RFC 7858)**, and **DNS over QUIC (DoQ / RFC 9250)**.
* **EDNS0 Padding (RFC 7830)**: Encrypted DNS queries are padded to block sizes (e.g. 128 or 256 bytes), preventing network eavesdroppers from deducing the queried domain name from ciphertext length.
* **EDNS Client Subnet (ECS / RFC 7871) Control**: Allows stripping ECS data to protect user geolocation privacy, or injecting specific subnet tokens for accurate GeoDNS routing.
* **Stale DNS Cache Fallback**: When upstream DNS resolvers time out or are blocked during network outages, `aoni` serves expired records from an in-memory stale cache while asynchronously refreshing in the background.

---

## 7. WAF Evasion, Traffic Padding & Anti-Bot Fidelity

* **High-Entropy User-Agent Client Hints**: Automatically synthesizes and populates structured `Sec-CH-UA`, `Sec-CH-UA-Mobile`, `Sec-CH-UA-Platform`, `Sec-CH-UA-Platform-Version`, and `Sec-CH-UA-Full-Version-List` headers matching the configured browser profile.
* **Header Casing & Canonicalization**: Preserves exact header capitalization and serialization order in HTTP/1.1 and HTTP/2 framing (e.g. `User-Agent` vs `user-agent`), avoiding standard Go `net/textproto` canonicalization leaks.
* **CDN & Traffic Padding**: Injects variable-length padding headers (`X-Padding`) and payload alignment bytes to defeat machine-learning traffic analysis classifiers that profile packet sizes.
* **WAF Challenge Solving Pipeline**: Native interceptor architecture designed to handle Cloudflare Turnstile, DataDome, and AWS WAF JavaScript challenges without aborting client workflows.

---

## 8. Memory Safety & DoS Prevention

* **Strict Response Size Limits**: `c.cfg.Defaults.MaxResponseSize` bounds the maximum bytes buffered in memory. Payloads exceeding the threshold trigger truncation or streaming errors, preventing malicious servers from exhausting system RAM.
* **Duplicate Request Guard**: Implements a circular ring buffer tracking recent in-flight request hashes. If an infinite redirect loop or recursive retry storm is detected, the transaction aborts with `ErrDuplicateRequestLoop`.
* **Zero-Allocation Memory Model**: High-throughput components (`fast`, `codec`, `netutil`) employ `pool.PerPStorage` with cache-line padding (`cpu.CacheLinePad`) and `borrow.Scope` scoped lifecycles. Buffers are returned to per-CPU storage without garbage collector overhead, eliminating stop-the-world latency spikes under saturated multi-gigabit traffic.

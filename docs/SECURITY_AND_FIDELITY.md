# Protocol Security & Edge Cases Specification

This document details protocol vulnerability mitigations and desynchronization guards implemented across HTTP/1.1, HTTP/2, and HTTP/3, mapped against Chromium (`net/`).

Web clients operate in an adversarial environment: untrusted origins, middlebox fragmentation, and multi-tenant isolation constraints.

```
[ Malicious Server / CDN ] ──► [ Intermediary Proxy ] ──► [ aoni L7 Parser ]
   • Conflicting Content-Length    • Desync Point           • Truncation Guard
   • Chunked + CL (TE.CL)         • Request Smuggling      • Control Character Scan
   • CRLF Injection / Null-Byte   • Cache Poisoning        • 408 Retry Pipeline
```

## 1. HTTP/1.1 Invariants

* **Response Splitting (TE.CL)**: If `Transfer-Encoding: chunked` is present, `Content-Length` is stripped.
* **Conflicting Content-Length**: Responses with multiple disparate `Content-Length` headers trigger transaction abort.
* **Control Character Poisoning**: `bytesconv.ValidateHeaderChars` performs SIMD/SWAR validation, rejecting bytes below `0x20` (except `\t`).
* **Keep-Alive Race (408)**: Triggers transparent retry on fresh connections.

## 2. HTTP/2 Defenses

* **Rapid Reset (CVE-2023-44487)**: Limits consecutive `RST_STREAM` frames (`maxConsecutiveControlFrames = 1000`).
* **CONTINUATION Flood (CVE-2024-27983)**: Limits total header block size to `SETTINGS_MAX_HEADER_LIST_SIZE` (256 KB). Aborts with `ErrFrameTooLarge`.
* **HPACK Decompression Bomb**: Tracks decompressed byte totals inside the Huffman decoding loop (256 KB cap).
* **Server Push**: Enforces `SETTINGS_ENABLE_PUSH = 0` by default (configurable via `option.WithH2ServerPush` for browser impersonation).

## 3. HTTP/3 & QUIC Defenses

* **UDP Amplification (RFC 9000)**: Outgoing `Initial` packets are padded to $\ge 1200$ bytes.
* **0-RTT Anti-Replay (RFC 8470)**: `resiliency/recovery` intercepts status 425 and falls back to 1-RTT.
* **Broken Alt-Svc / UDP Throttling**: Employs Happy Eyeballs v3 and 48-hour exponential backoff.

## 4. Network Isolation Keys (NIK)

```
[ Request Context ] ──► (TopFrameSite: "https://a.com", FrameSite: "https://b.com")
                                  │
         ┌────────────────────────┼────────────────────────┐
         ▼                        ▼                        ▼
  [ DNS Cache ]           [ TLS Tickets ]           [ Cookie Jar ]
  Key: NIK + Host         Key: NIK + Host           Key: NIK + Domain
```

Partitions TCP/TLS socket pools and RFC 6265bis CHIPS per origin context to eliminate cross-site tracking.

# ja4

Pure Go implementation of [JA4+](https://github.com/FoxIO-LLC/ja4) network fingerprinting algorithms.

Part of the [aoni](../) HTTP client library.

## What is JA4+?

JA4+ is a suite of methods for creating human and machine-readable fingerprints of network traffic. It improves upon JA3 by:

- Using a **locality-preserving** `a_b_c` format for flexible partial matching
- **Sorting** cipher suites and extensions to resist stunting/randomization
- Including **signature algorithms** for better uniqueness
- Supporting both **TLS** (JA4) and **HTTP** (JA4H) fingerprints

## Supported Methods

| Method | Description | Format |
|--------|-------------|--------|
| **JA4** | TLS ClientHello fingerprint | `t13d1516h2_<cipher_hash>_<ext_hash>` |
| **JA4H** | HTTP request fingerprint | `ge11nn03enus_<headers_hash>_<cookie_names>_<cookie_values>` |

## Quick Start

### Standalone Usage

```go
import "github.com/lemon4ksan/aoni/ja4"

// JA4 — TLS fingerprint from ClientHello data
fingerprint := ja4.ComputeJA4(
    cipherSuites,     // []uint16 — cipher suite IDs
    extensions,       // []uint16 — extension IDs in wire order
    supportedVersions,// []uint16 — from supported_versions extension
    true,             // bool — SNI present?
    []string{"h2"},   // []string — ALPN protocols
    sigAlgorithms,    // []uint16 — signature algorithms (may be nil)
)
// "t13d1516h2_8daaf6152771_e5627efa2ab1"

// JA4H — HTTP request fingerprint
fingerprint := ja4.ComputeJA4H(
    "GET",                    // method
    "HTTP/1.1",               // protocol version
    []string{"Host", "UA"},   // header names (excl. Cookie, Referer)
    false,                    // has Cookie?
    false,                    // has Referer?
    "en-US",                  // Accept-Language
    nil, nil,                 // cookie names & values (sorted by name)
)
// "ge11nn02enus_..."
```

### With aoni Client

```go
import (
    "github.com/lemon4ksan/aoni"
    "github.com/lemon4ksan/aoni/ja4"
)

info := &aoni.TraceInfo{}

client := aoni.NewClient(nil).
    WithTLSFingerprint(aoni.BrowserChrome).
    WithJA4Callback(func(r ja4.JA4Report) {
        fmt.Println("JA4:", r.JA4)
    })

client.Get(ctx, "/path", aoni.TraceJA4(info))
fmt.Println(info.JA4.JA4)  // TLS fingerprint
fmt.Println(info.JA4.JA4H) // HTTP fingerprint
```

## Fingerprint Format

### JA4 (TLS)

```
{protocol}{version}{sni}{cipher_count}{ext_count}{alpn}_{cipher_hash}_{ext_hash}

t  13  d  15  16  h2  8daaf6152771  e5627efa2ab1
│   │   │   │   │   │       │              │
│   │   │   │   │   │       │              └─ SHA-256 of sorted extensions + sig algos
│   │   │   │   │   │       └─ SHA-256 of sorted cipher suites
│   │   │   │   │   └─ ALPN: first+last char of first protocol
│   │   │   │   └─ Extension count (GREASE excluded)
│   │   │   └─ Cipher count (GREASE excluded)
│   │   └─ SNI: d=domain, i=IP
│   └─ TLS version: 13=1.3, 12=1.2
└─ Protocol: t=TLS, q=QUIC, d=DTLS
```

### JA4H (HTTP)

```
{method}{version}{cookie}{referer}{header_count}{lang}_{headers_hash}_{cookie_names}_{cookie_values}

g  e  1  1  n  n  03  enus  1c8f3b0e29d1  000000000000  000000000000
│  │  │  │  │  │   │    │         │              │              │
│  │  │  │  │  │   │    │         │              │              └─ cookie values hash
│  │  │  │  │  │   │    │         │              └─ cookie names hash
│  │  │  │  │  │   │    │         └─ sorted header names hash
│  │  │  │  │  │   │    └─ first 4 alphanum chars of Accept-Language
│  │  │  │  │  │   └─ header count (excl. Cookie, Referer)
│  │  │  │  │  └─ Referer: r=present, n=absent
│  │  │  │  └─ Cookie: c=present, n=absent
│  │  │  └─ HTTP version: 10=1.0, 11=1.1, 20=2, 30=3
│  │  └─ first 2 chars of method, lowercased
```

## API Reference

| Function | Description |
|----------|-------------|
| `ComputeJA4(...)` | Compute TLS client fingerprint |
| `ComputeJA4H(...)` | Compute HTTP client fingerprint |
| `ParseExtensionsFromRaw(raw)` | Parse extension IDs from raw ClientHello bytes |
| `IsGREASE(v)` | Check if a value is a TLS GREASE code point |
| `FilterGREASE(vals)` | Remove GREASE values from a slice |

## Tests

```sh
go test ./ja4/ -v
```

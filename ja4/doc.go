// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package ja4 implements JA4+ network fingerprinting algorithms in pure Go.

JA4+ is a suite of methods for creating human and machine-readable fingerprints
of network traffic. This package provides [ComputeJA4] (TLS client fingerprinting)
and [ComputeJA4H] (HTTP client fingerprinting), the two methods most relevant
for HTTP client libraries.

# Fingerprint Format

All JA4+ fingerprints use an a_b_c locality-preserving format with three sections,
allowing partial matching on individual parts independently.

  - Section a: structured metadata (protocol, version, SNI, counts, ALPN)
  - Section b: hash of cipher suites (JA4) or header names (JA4H)
  - Section c: hash of extensions + signature algorithms (JA4) or cookie data (JA4H)

# JA4 — TLS Client Fingerprint

The [ComputeJA4] function produces a fingerprint from a TLS ClientHello:

		t13d1516h2_8daaf6152771_e5627efa2ab1

	  - t: protocol (t=TLS, q=QUIC, d=DTLS)
	  - 13: highest TLS version (13=TLS 1.3, 12=TLS 1.2)
	  - d: SNI present (d=domain, i=IP)
	  - 15: cipher suite count (GREASE excluded)
	  - 16: extension count (GREASE excluded)
	  - h2: first+last char of first ALPN protocol
	  - 8daaf6152771: SHA-256 hash of sorted cipher suites (truncated to 12 hex chars)
	  - e5627efa2ab1: SHA-256 hash of sorted extensions + sig algorithms (truncated to 12 hex chars)

# JA4H — HTTP Client Fingerprint

The [ComputeJA4H] function produces a fingerprint from HTTP request properties:

		ge11nn03enus_1c8f3b0e29d1_000000000000_000000000000

	  - ge: first 2 chars of method, lowercased
	  - 11: HTTP version (10=1.0, 11=1.1, 20=2, 30=3)
	  - n: no cookies (c=present)
	  - n: no referer (r=present)
	  - 03: header count (excluding Cookie, Referer)
	  - enus: first 4 chars of Accept-Language
	  - 1c8f3b0e29d1: SHA-256 hash of sorted header names (truncated to 12 hex chars)
	  - 000000000000: SHA-256 hash of sorted cookie names (12 zeros if no cookies)
	  - 000000000000: SHA-256 hash of cookie values in sorted-by-name order

# GREASE Handling

TLS GREASE (Generate Random Extensions And Sustain Extensibility) values are
filtered from all counts and hashes. Use [IsGREASE] to check individual values
and [FilterGREASE] to remove them from a slice.

# Integration with aoni

For automatic JA4 fingerprinting through the aoni HTTP client, use:

  - [aoni.WithTLSFingerprint] to emulate browser TLS handshakes

  - [aoni.WithJA4Callback] to receive fingerprints via a callback

  - [aoni.TraceJA4] to populate [aoni.TraceInfo] with both JA4 and JA4H

    info := &aoni.TraceInfo{}
    client := aoni.NewClient(nil).
    WithTLSFingerprint(aoni.BrowserChrome).
    WithJA4Callback(func(r ja4.JA4Report) {
    fmt.Println("TLS JA4:", r.JA4)
    })

    client.Get(ctx, "/path", aoni.TraceJA4(info))
    fmt.Println("HTTP JA4H:", info.JA4.JA4H)

# Reference Implementation

This package implements the algorithms described in the FoxIO JA4+ technical
specification. The fingerprint format is designed to be resilient against
cipher stunting and extension randomization used by modern browsers.
*/
package ja4

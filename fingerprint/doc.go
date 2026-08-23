// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fingerprint provides multi-layer network fingerprint evasion, TLS stealth emulation,
// JA4/JA4H algorithms, and Chromium-grade browser personas for Go.
//
// Modern Web Application Firewalls (Cloudflare, Akamai, DataDome, CloudFront) analyze incoming HTTP
// transactions across 5 distinct protocol layers:
//
//  1. Layer 3/4 (TCP/IP p0f Stack): Initial TTL, IP Don't Fragment (DF) flags, TCP Window Size, MSS, and Option ordering.
//  2. Layer 5 (TLS 1.3 Handshake): ClientHello cipher suite permutations, extension codepoints, elliptic curves, and GREASE (RFC 8701).
//  3. Layer 5+ (Encrypted Client Hello / ECH): DNS SVCB/HTTPS (RFC 9460) inner ClientHello encryption.
//  4. Layer 7 Framing (HTTP/2 & HTTP/3): SETTINGS frame values, WINDOW_UPDATE intervals, and pseudo-header (:method, :path, :authority, :scheme) order.
//  5. Layer 7 Semantic (Headers & Client Hints): Exact header casing, W3C High-Entropy Client Hints, and padding against packet length analysis.
//
// # Architectural Subpackages
//
//   - [github.com/lemon4ksan/aoni/fingerprint/ech]: RFC 9460 Encrypted Client Hello configuration parsers and TLS extensions.
//   - [github.com/lemon4ksan/aoni/fingerprint/grease]: RFC 8701 GREASE reserved values, identification, generation, and filtering.
//   - [github.com/lemon4ksan/aoni/fingerprint/h2]: HTTP/2 SETTINGS, PRIORITY, and HPACK header serialization.
//   - [github.com/lemon4ksan/aoni/fingerprint/h3]: HTTP/3 QPACK and datagram settings.
//   - [github.com/lemon4ksan/aoni/fingerprint/ja4]: Pure-Go JA4 and JA4H fingerprint calculation algorithms.
//   - [github.com/lemon4ksan/aoni/fingerprint/p0f]: OS-level TCP/IP stack signatures (TTL, DF, Window Size).
//   - [github.com/lemon4ksan/aoni/fingerprint/profiles]: Pre-packaged browser version profiles and personas.
//
// # Pre-Packaged Personas
//
// The package exposes complete personas that bundle TLS Hello IDs, HTTP/2 frames, header orders, and p0f signatures:
//   - [PersonaChromeWindows]
//   - [PersonaChromeAndroid]
//   - [PersonaFirefoxWindows]
//   - [PersonaFirefoxAndroid]
//   - [PersonaSafariMacOS]
//   - [PersonaSafariIOS]
package fingerprint

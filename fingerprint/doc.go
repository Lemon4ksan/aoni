// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fingerprint provides TLS/JA4/p0f fingerprint evasion and browser impersonation profiles.
//
// It enables the aoni HTTP client to mimic real-world browser profiles (Chrome, Firefox, Safari)
// down to TLS ClientHello extension ordering, HTTP/2 SETTINGS frames, HPACK header serialization,
// and OS-level TCP/IP p0f stack signatures.
//
// # Subpackages
//
//   - [github.com/lemon4ksan/aoni/fingerprint/h2]: HTTP/2 SETTINGS, PRIORITY, and HPACK header serialization.
//   - [github.com/lemon4ksan/aoni/fingerprint/ja4]: Pure-Go JA4 and JA4H fingerprint calculation algorithms.
//   - [github.com/lemon4ksan/aoni/fingerprint/p0f]: TCP/IP stack fingerprint signatures (TTL, DF, Window Size).
//   - [github.com/lemon4ksan/aoni/fingerprint/profiles]: Pre-packaged browser version profiles and personas.
package fingerprint

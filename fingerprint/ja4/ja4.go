// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ja4 provides pure-Go computation of JA4 (TLS) and JA4H (HTTP) client fingerprints.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/tls/ja4].
package ja4

import (
	fja4 "github.com/lemon4ksan/foundation/net/tls/ja4"
)

// ErrInvalidJA4Input indicates corrupted or truncated ClientHello byte payloads.
var ErrInvalidJA4Input = fja4.ErrInvalidJA4Input

// Report holds computed TLS (JA4) and HTTP (JA4H) fingerprints alongside TLS metadata.
type Report = fja4.Report

// ComputeJA4 evaluates a TLS client fingerprint string in 'a_b_c' format.
func ComputeJA4(
	cipherSuites []uint16,
	extensions []uint16,
	supportedVersions []uint16,
	sni bool,
	alpnProtocols []string,
	sigAlgorithms []uint16,
) string {
	return fja4.ComputeJA4(cipherSuites, extensions, supportedVersions, sni, alpnProtocols, sigAlgorithms)
}

// ComputeJA4H evaluates an HTTP client fingerprint string in 'a_b_c_d' format.
func ComputeJA4H(
	method, proto string,
	headers []string,
	hasCookie, hasReferer bool,
	acceptLanguage string,
	cookieNames, cookieValues []string,
) string {
	return fja4.ComputeJA4H(method, proto, headers, hasCookie, hasReferer, acceptLanguage, cookieNames, cookieValues)
}

// ParseExtensionsFromRaw extracts extension IDs and signature algorithms from raw ClientHello bytes.
func ParseExtensionsFromRaw(raw []byte) (extensions, sigAlgorithms []uint16) {
	return fja4.ParseExtensionsFromRaw(raw)
}

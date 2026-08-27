// Package ja4 provides pure-Go computation of JA4 (TLS ClientHello) and JA4H (HTTP Request) fingerprints.
//
// # Architectural Context
//
// JA4 is a standard fingerprinting specification that categorizes TLS and HTTP client implementations
// based on cipher suites, extensions, ALPN values, and HTTP header ordering.
//
// # RFC Compliance
//
// Conforms to the FoxIO JA4+ Network Fingerprinting Specification.
package ja4

import (
	fja4 "github.com/lemon4ksan/foundation/net/tls/ja4"
)

// ErrInvalidJA4Input indicates corrupted, incomplete, or truncated ClientHello byte payloads.
var ErrInvalidJA4Input = fja4.ErrInvalidJA4Input

// Report holds computed TLS (JA4) and HTTP (JA4H) fingerprints alongside parsed TLS metadata.
type Report = fja4.Report

// ComputeJA4 evaluates a TLS client fingerprint string in canonical 'a_b_c' format.
//
// # Wire Representation
//
//	t13d1516h2_8daaf6152771_016657550a2e
//
// # Example
//
//	rawFingerprint := ja4.ComputeJA4(ciphers, extensions, versions, true, []string{"h2", "http/1.1"}, sigAlgs)
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

// ComputeJA4H evaluates an HTTP client fingerprint string in canonical 'a_b_c_d' format based on headers and method.
//
// # Example
//
//	hFingerprint := ja4.ComputeJA4H("GET", "2.0", []string{"Host", "User-Agent", "Accept"}, true, false, "en-US", cookieNames, cookieVals)
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

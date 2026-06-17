// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const hashLen = 12

// tlsVersionMap maps TLS version wire values to JA4 version strings.
var tlsVersionMap = map[uint16]string{
	0x0304: "13",
	0x0303: "12",
	0x0302: "11",
	0x0301: "10",
	0x0300: "s3",
	0x0002: "s2",
}

// greaseValues contains the 16 GREASE (Generate Random Extensions And Sustain Extensibility) values.
// GREASE values are used by TLS implementations to ensure protocol extensibility
// and must be excluded from JA4 fingerprint computation.
var greaseValues = map[uint16]bool{
	0x0a0a: true,
	0x1a1a: true,
	0x2a2a: true,
	0x3a3a: true,
	0x4a4a: true,
	0x5a5a: true,
	0x6a6a: true,
	0x7a7a: true,
	0x8a8a: true,
	0x9a9a: true,
	0xaaaa: true,
	0xbaba: true,
	0xcaca: true,
	0xdada: true,
	0xeaea: true,
	0xfafa: true,
}

// IsGREASE reports whether v is a TLS GREASE value.
func IsGREASE(v uint16) bool {
	return greaseValues[v]
}

// Report holds both TLS (JA4) and HTTP (JA4H) fingerprints computed from a request.
type Report struct {
	// JA4 is the TLS client fingerprint (e.g. "t13d1516h2_8daaf6152771_e5627efa2ab1").
	JA4 string
	// JA4H is the HTTP client fingerprint (e.g. "ge11cn04en04_9ed1ff1f7b03_cd8dafe26982").
	JA4H string
	// Protocol is the TLS protocol prefix: "t" (TLS), "q" (QUIC), "d" (DTLS).
	Protocol string
	// Version is the negotiated TLS version code: "13" (TLS 1.3), "12" (TLS 1.2), etc.
	Version string
	// SNI indicates SNI presence: "d" (domain name) or "i" (IP address).
	SNI string
	// CipherCount is the number of cipher suites (GREASE excluded).
	CipherCount int
	// ExtCount is the number of extensions (GREASE excluded).
	ExtCount int
	// ALPN is the first and last alphanumeric characters of the first ALPN protocol.
	ALPN string
}

// ComputeJA4 computes a JA4 TLS client fingerprint.
//
// Parameters:
//   - cipherSuites: raw cipher suite IDs from ClientHello
//   - extensions: extension IDs in wire order
//   - supportedVersions: from supported_versions extension
//   - sni: whether SNI extension is present
//   - alpnProtocols: ALPN protocol strings
//   - sigAlgorithms: signature algorithm IDs in wire order (may be nil)
//
// The fingerprint format is: {protocol}{version}{sni}{cipher_count}{ext_count}{alpn}_{cipher_hash}_{ext_hash}
func ComputeJA4(
	cipherSuites []uint16,
	extensions []uint16,
	supportedVersions []uint16,
	sni bool,
	alpnProtocols []string,
	sigAlgorithms []uint16,
) string {
	protocol := "t"
	version := computeVersion(supportedVersions)

	sniChar := "i"
	if sni {
		sniChar = "d"
	}

	filteredCiphers := FilterGREASE(cipherSuites)
	cipherCount := min(len(filteredCiphers), 99)

	filteredExts := FilterGREASE(extensions)
	extCount := min(len(filteredExts), 99)

	alpn := computeALPN(alpnProtocols)

	cipherHash := computeCipherHash(filteredCiphers)

	extHash := computeExtHash(filteredExts, sigAlgorithms)

	return fmt.Sprintf("%s%s%s%02d%02d%s_%s_%s",
		protocol, version, sniChar, cipherCount, extCount, alpn,
		cipherHash, extHash,
	)
}

// ComputeJA4H computes an HTTP client fingerprint.
//
// Parameters:
//   - method: HTTP method (e.g. "GET", "POST")
//   - proto: HTTP protocol version (e.g. "HTTP/1.1", "HTTP/2")
//   - headers: header names in original order (excluding Cookie, Referer, pseudo-headers)
//   - hasCookie: whether Cookie header is present
//   - hasReferer: whether Referer header is present
//   - acceptLanguage: Accept-Language header value
//   - cookieNames: cookie names sorted by name
//   - cookieValues: cookie values in sorted-by-name order
//
// The fingerprint format is:
// {method}{version}{cookie}{referer}{header_count}{lang}_{headers_hash}_{cookie_names_hash}_{cookie_values_hash}
func ComputeJA4H(
	method, proto string,
	headers []string,
	hasCookie, hasReferer bool,
	acceptLanguage string,
	cookieNames, cookieValues []string,
) string {
	methodStr := "00"
	if len(method) >= 2 {
		methodStr = strings.ToLower(method[:2])
	}

	version := "00"
	switch proto {
	case "HTTP/1.0":
		version = "10"
	case "HTTP/1.1":
		version = "11"
	case "HTTP/2":
		version = "20"
	case "HTTP/3":
		version = "30"
	}

	cookie := "n"
	if hasCookie {
		cookie = "c"
	}

	referer := "n"
	if hasReferer {
		referer = "r"
	}

	headerCount := min(len(headers), 99)
	lang := computeLanguage(acceptLanguage)
	headersHash := computeHeadersHash(headers)

	cookieNamesHash := hash12(strings.Join(cookieNames, ","))
	if len(cookieNames) == 0 {
		cookieNamesHash = "000000000000"
	}

	cookieValuesHash := hash12(strings.Join(cookieValues, ","))
	if len(cookieValues) == 0 {
		cookieValuesHash = "000000000000"
	}

	return fmt.Sprintf("%s%s%s%s%02d%s_%s_%s_%s",
		methodStr, version, cookie, referer, headerCount, lang,
		headersHash, cookieNamesHash, cookieValuesHash,
	)
}

// FilterGREASE returns a new slice with GREASE values removed.
func FilterGREASE(vals []uint16) []uint16 {
	result := make([]uint16, 0, len(vals))
	for _, v := range vals {
		if !IsGREASE(v) {
			result = append(result, v)
		}
	}

	return result
}

// computeVersion returns the JA4 version string from supported_versions.
func computeVersion(supportedVersions []uint16) string {
	filtered := FilterGREASE(supportedVersions)
	if len(filtered) == 0 {
		return "00"
	}

	highest := filtered[0]
	for _, v := range filtered[1:] {
		if v > highest {
			highest = v
		}
	}

	if v, ok := tlsVersionMap[highest]; ok {
		return v
	}

	return "00"
}

// computeALPN returns the JA4 ALPN string (first + last alphanumeric char of first protocol).
func computeALPN(protocols []string) string {
	if len(protocols) == 0 || protocols[0] == "" {
		return "00"
	}

	first := protocols[0]
	if len(first) == 0 {
		return "00"
	}

	return string(first[0]) + string(first[len(first)-1])
}

// computeCipherHash computes the JA4 cipher hash from filtered cipher suites.
func computeCipherHash(ciphers []uint16) string {
	if len(ciphers) == 0 {
		return strings.Repeat("0", hashLen)
	}

	hexes := make([]string, len(ciphers))
	for i, c := range ciphers {
		hexes[i] = fmt.Sprintf("%04x", c)
	}

	sort.Strings(hexes)

	return hash12(strings.Join(hexes, ","))
}

// computeExtHash computes the JA4 extension hash from filtered extensions and signature algorithms.
func computeExtHash(extensions, sigAlgorithms []uint16) string {
	// Filter out SNI (0x0000) and ALPN (0x0010)
	exts := make([]string, 0, len(extensions))
	for _, e := range extensions {
		if e == 0x0000 || e == 0x0010 {
			continue
		}

		exts = append(exts, fmt.Sprintf("%04x", e))
	}

	if len(exts) == 0 && len(sigAlgorithms) == 0 {
		return strings.Repeat("0", hashLen)
	}

	sort.Strings(exts)

	// Append signature algorithms in original order (unsorted)
	if len(sigAlgorithms) > 0 {
		sigParts := make([]string, len(sigAlgorithms))
		for i, s := range sigAlgorithms {
			sigParts[i] = fmt.Sprintf("%04x", s)
		}

		return hash12(strings.Join(exts, ",") + "_" + strings.Join(sigParts, ","))
	}

	return hash12(strings.Join(exts, ","))
}

// computeLanguage returns the first 4 alphanumeric characters of the Accept-Language value.
func computeLanguage(lang string) string {
	if lang == "" {
		return "0000"
	}

	// Strip whitespace, take first 4 alphanumeric chars
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}

		return -1
	}, lang)

	if len(cleaned) == 0 {
		return "0000"
	}

	if len(cleaned) > 4 {
		cleaned = cleaned[:4]
	}

	// Pad to 4 chars
	for len(cleaned) < 4 {
		cleaned += "0"
	}

	return strings.ToLower(cleaned)
}

// computeHeadersHash computes the JA4H headers hash.
func computeHeadersHash(headers []string) string {
	if len(headers) == 0 {
		return strings.Repeat("0", hashLen)
	}

	lower := make([]string, len(headers))
	for i, h := range headers {
		lower[i] = strings.ToLower(h)
	}

	sort.Strings(lower)

	return hash12(strings.Join(lower, ","))
}

// hash12 returns the first 12 hex characters of the SHA-256 hash of s.
func hash12(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

// ParseExtensionsFromRaw parses extension IDs from a raw TLS ClientHello message
// in wire order. It also extracts signature algorithms if present.
//
// The raw format is:
//
//	2 bytes: handshake type (0x0300) + length
//	2 bytes: client version
//	32 bytes: random
//	1 byte + session ID: variable
//	2 bytes + cipher suites: variable
//	1 byte + compression methods: variable
//	2 bytes: extensions total length
//	then: extension entries (2-byte ID + 2-byte length + data)
func ParseExtensionsFromRaw(raw []byte) (extensions, sigAlgorithms []uint16) {
	if len(raw) < 38 { // minimum: type(2) + len(3) + version(2) + random(32) = 39, but we need at least the header
		return nil, nil
	}

	offset := 0

	// Client version (2 bytes)
	if offset+2 > len(raw) {
		return nil, nil
	}

	offset += 2

	// Random (32 bytes)
	if offset+32 > len(raw) {
		return nil, nil
	}

	offset += 32

	// Session ID (1 byte length + variable)
	if offset >= len(raw) {
		return nil, nil
	}

	sessionIDLen := int(raw[offset])

	offset++
	if offset+sessionIDLen > len(raw) {
		return nil, nil
	}

	offset += sessionIDLen

	// Cipher suites (2 bytes length + variable)
	if offset+2 > len(raw) {
		return nil, nil
	}

	cipherSuitesLen := int(binary.BigEndian.Uint16(raw[offset : offset+2]))

	offset += 2
	if offset+cipherSuitesLen > len(raw) {
		return nil, nil
	}

	offset += cipherSuitesLen

	// Compression methods (1 byte length + variable)
	if offset >= len(raw) {
		return nil, nil
	}

	compMethodsLen := int(raw[offset])

	offset++
	if offset+compMethodsLen > len(raw) {
		return nil, nil
	}

	offset += compMethodsLen

	// Extensions total length (2 bytes)
	if offset+2 > len(raw) {
		return nil, nil
	}

	extTotalLen := int(binary.BigEndian.Uint16(raw[offset : offset+2]))
	offset += 2

	extEnd := min(offset+extTotalLen, len(raw))

	// Parse individual extensions
	for offset+4 <= extEnd {
		extID := binary.BigEndian.Uint16(raw[offset : offset+2])
		extDataLen := int(binary.BigEndian.Uint16(raw[offset+2 : offset+4]))
		offset += 4

		extensions = append(extensions, extID)

		// Extract signature algorithms from extension 0x000d
		if extID == 0x000d && extDataLen >= 2 && offset+extDataLen <= extEnd {
			sigCount := extDataLen / 2

			sigAlgorithms = make([]uint16, sigCount)
			for i := range sigCount {
				sigAlgorithms[i] = binary.BigEndian.Uint16(raw[offset+i*2 : offset+i*2+2])
			}
		}

		offset += extDataLen
	}

	return extensions, sigAlgorithms
}

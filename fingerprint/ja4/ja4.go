// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ja4 provides pure-Go computation of JA4 (TLS) and JA4H (HTTP) client fingerprints.
package ja4

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"sync"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// ErrInvalidJA4Input indicates corrupted or truncated ClientHello byte payloads.
var ErrInvalidJA4Input = errors.New("ja4: invalid input payload for fingerprint computation")

const (
	hashLen  = 12
	hexTable = "0123456789abcdef"

	extSNI                 uint16 = 0x0000
	extALPN                uint16 = 0x0010
	extSignatureAlgorithms uint16 = 0x000d

	recordTypeHandshake      byte = 0x16
	handshakeTypeClientHello byte = 0x01

	clientHelloHeaderOffset = 34
)

var tlsVersionMap = map[uint16]string{
	0x0304: "13",
	0x0303: "12",
	0x0302: "11",
	0x0301: "10",
	0x0300: "s3",
	0x0002: "s2",
}

var greaseValues = map[uint16]struct{}{
	0x0a0a: {}, 0x1a1a: {}, 0x2a2a: {}, 0x3a3a: {},
	0x4a4a: {}, 0x5a5a: {}, 0x6a6a: {}, 0x7a7a: {},
	0x8a8a: {}, 0x9a9a: {}, 0xaaaa: {}, 0xbaba: {},
	0xcaca: {}, 0xdada: {}, 0xeaea: {}, 0xfafa: {},
}

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func acquireBuffer(capacity int) *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	if capacity > 0 {
		buf.Grow(capacity)
	}

	return buf
}

func releaseBuffer(buf *bytes.Buffer) {
	if buf != nil {
		bufferPool.Put(buf)
	}
}

// IsGREASE reports whether v matches a reserved TLS GREASE value.
func IsGREASE(v uint16) bool {
	_, ok := greaseValues[v]
	return ok
}

// Report holds computed TLS (JA4) and HTTP (JA4H) fingerprints alongside TLS metadata.
type Report struct {
	JA4         string
	JA4H        string
	Protocol    string
	Version     string
	SNI         string
	ALPN        string
	CipherCount int
	ExtCount    int
}

// ComputeJA4 evaluates a TLS client fingerprint string in 'a_b_c' format.
func ComputeJA4(
	cipherSuites []uint16,
	extensions []uint16,
	supportedVersions []uint16,
	sni bool,
	alpnProtocols []string,
	sigAlgorithms []uint16,
) string {
	sniChar := byte('i')
	if sni {
		sniChar = 'd'
	}

	filteredCiphers := FilterGREASE(cipherSuites)
	cipherCount := min(len(filteredCiphers), 99)

	filteredExts := FilterGREASE(extensions)
	extCount := min(len(filteredExts), 99)

	alpn := computeALPN(alpnProtocols)
	cipherHash := computeCipherHash(filteredCiphers)
	extHash := computeExtHash(filteredExts, sigAlgorithms)

	buf := acquireBuffer(36)
	defer releaseBuffer(buf)

	buf.WriteByte('t')
	buf.WriteString(computeVersion(supportedVersions))
	buf.WriteByte(sniChar)

	writePaddedTwoDigits(buf, cipherCount)
	writePaddedTwoDigits(buf, extCount)

	buf.WriteString(alpn)
	buf.WriteByte('_')
	buf.WriteString(cipherHash)
	buf.WriteByte('_')
	buf.WriteString(extHash)

	return buf.String()
}

// ComputeJA4H evaluates an HTTP client fingerprint string in 'a_b_c_d' format.
func ComputeJA4H(
	method, proto string,
	headers []string,
	hasCookie, hasReferer bool,
	acceptLanguage string,
	cookieNames, cookieValues []string,
) string {
	var methodBuf [2]byte
	if len(method) >= 2 {
		methodBuf[0] = bytesconv.LowercaseByte(method[0])
		methodBuf[1] = bytesconv.LowercaseByte(method[1])
	} else {
		methodBuf[0] = '0'
		methodBuf[1] = '0'
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

	cookieChar := byte('n')
	if hasCookie {
		cookieChar = 'c'
	}

	refererChar := byte('n')
	if hasReferer {
		refererChar = 'r'
	}

	buf := acquireBuffer(52)
	defer releaseBuffer(buf)

	buf.Write(methodBuf[:])
	buf.WriteString(version)
	buf.WriteByte(cookieChar)
	buf.WriteByte(refererChar)

	writePaddedTwoDigits(buf, min(len(headers), 99))

	buf.WriteString(computeLanguage(acceptLanguage))
	buf.WriteByte('_')
	buf.WriteString(computeHeadersHash(headers))
	buf.WriteByte('_')
	buf.WriteString(hashSlice(cookieNames))
	buf.WriteByte('_')
	buf.WriteString(hashSlice(cookieValues))

	return buf.String()
}

// ParseExtensionsFromRaw extracts extension IDs and signature algorithms from raw ClientHello bytes.
func ParseExtensionsFromRaw(raw []byte) (extensions, sigAlgorithms []uint16) {
	if len(raw) > 5 && raw[0] == recordTypeHandshake {
		raw = raw[5:]
	}

	if len(raw) > 4 && raw[0] == handshakeTypeClientHello {
		raw = raw[4:]
	}

	if len(raw) < 38 {
		return nil, nil
	}

	offset := clientHelloHeaderOffset
	if offset >= len(raw) {
		return nil, nil
	}

	offset += 1 + int(raw[offset])
	if offset+2 > len(raw) {
		return nil, nil
	}

	offset += 2 + int(binary.BigEndian.Uint16(raw[offset:offset+2]))
	if offset >= len(raw) {
		return nil, nil
	}

	offset += 1 + int(raw[offset])
	if offset+2 > len(raw) {
		return nil, nil
	}

	extTotalLen := int(binary.BigEndian.Uint16(raw[offset : offset+2]))
	offset += 2

	extEnd := min(offset+extTotalLen, len(raw))

	for offset+4 <= extEnd {
		extID := binary.BigEndian.Uint16(raw[offset : offset+2])
		extDataLen := int(binary.BigEndian.Uint16(raw[offset+2 : offset+4]))
		offset += 4

		extensions = append(extensions, extID)

		if extID == extSignatureAlgorithms && extDataLen >= 2 && offset+extDataLen <= extEnd {
			sigAlgorithms = parseSigAlgorithmsPayload(raw[offset : offset+extDataLen])
		}

		offset += extDataLen
	}

	return extensions, sigAlgorithms
}

func parseSigAlgorithmsPayload(payload []byte) []uint16 {
	count := len(payload) / 2
	if count == 0 {
		return nil
	}

	sigs := make([]uint16, count)
	_ = payload[count*2-1] // BCE hint: eliminate bounds checks in payload slice reads

	for i := 0; i < count; i++ {
		sigs[i] = binary.BigEndian.Uint16(payload[i*2 : i*2+2])
	}

	return sigs
}

func computeLanguage(lang string) string {
	if lang == "" {
		return "0000"
	}

	var buf [4]byte

	count := 0

	for i := range lang {
		if count == 4 {
			break
		}

		b := lang[i]
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			buf[count] = b
			count++
		} else if b >= 'A' && b <= 'Z' {
			buf[count] = bytesconv.LowercaseByte(b)
			count++
		}
	}

	if count == 0 {
		return "0000"
	}

	for count < 4 {
		buf[count] = '0'
		count++
	}

	return bytesconv.B2S(buf[:])
}

func computeHeadersHash(headers []string) string {
	if len(headers) == 0 {
		return "000000000000"
	}

	// Stack-allocated array for header key sorting to prevent heap allocations
	var (
		stackBuf [64]string
		sorted   []string
	)

	if len(headers) <= cap(stackBuf) {
		sorted = stackBuf[:len(headers)]
		copy(sorted, headers)
	} else {
		sorted = slices.Clone(headers)
	}

	slices.SortFunc(sorted, compareLowerASCII)

	buf := acquireBuffer(len(headers) * 12)
	defer releaseBuffer(buf)

	for i, h := range sorted {
		if i > 0 {
			buf.WriteByte(',')
		}

		for j := 0; j < len(h); j++ {
			buf.WriteByte(bytesconv.LowercaseByte(h[j]))
		}
	}

	return hash12Hex(buf.Bytes())
}

func compareLowerASCII(a, b string) int {
	minLen := min(len(a), len(b))
	if minLen == 0 {
		return cmp.Compare(len(a), len(b))
	}

	// BCE hints: prove slice boundaries to SSA compiler to eliminate loop bounds checks
	_ = a[minLen-1]
	_ = b[minLen-1]

	for i := 0; i < minLen; i++ {
		la := bytesconv.LowercaseByte(a[i])
		lb := bytesconv.LowercaseByte(b[i])

		if la != lb {
			if la < lb {
				return -1
			}

			return 1
		}
	}

	return cmp.Compare(len(a), len(b))
}

func hashSlice(items []string) string {
	if len(items) == 0 {
		return "000000000000"
	}

	buf := acquireBuffer(len(items) * 16)
	defer releaseBuffer(buf)

	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}

		buf.WriteString(item)
	}

	return hash12Hex(buf.Bytes())
}

func hash12Hex(b []byte) string {
	sum := sha256.Sum256(b)

	var dst [12]byte

	_ = hexTable[15]

	dst[0] = hexTable[sum[0]>>4]
	dst[1] = hexTable[sum[0]&0x0f]
	dst[2] = hexTable[sum[1]>>4]
	dst[3] = hexTable[sum[1]&0x0f]
	dst[4] = hexTable[sum[2]>>4]
	dst[5] = hexTable[sum[2]&0x0f]
	dst[6] = hexTable[sum[3]>>4]
	dst[7] = hexTable[sum[3]&0x0f]
	dst[8] = hexTable[sum[4]>>4]
	dst[9] = hexTable[sum[4]&0x0f]
	dst[10] = hexTable[sum[5]>>4]
	dst[11] = hexTable[sum[5]&0x0f]

	return bytesconv.B2S(dst[:])
}

func writePaddedTwoDigits(buf *bytes.Buffer, n int) {
	if n < 10 {
		buf.WriteByte('0')
		buf.WriteByte(byte('0' + n)) //nolint:gosec
		return
	}

	buf.WriteByte(byte('0' + n/10)) //nolint:gosec
	buf.WriteByte(byte('0' + n%10)) //nolint:gosec
}

func computeCipherHash(ciphers []uint16) string {
	if len(ciphers) == 0 {
		return "000000000000"
	}

	var (
		stackBuf      [64]uint16
		sortedCiphers []uint16
	)

	if len(ciphers) <= cap(stackBuf) {
		sortedCiphers = stackBuf[:len(ciphers)]
		copy(sortedCiphers, ciphers)
	} else {
		sortedCiphers = slices.Clone(ciphers)
	}

	slices.Sort(sortedCiphers)

	buf := acquireBuffer(len(sortedCiphers) * 5)
	defer releaseBuffer(buf)

	for i, c := range sortedCiphers {
		if i > 0 {
			buf.WriteByte(',')
		}

		writeHex4(buf, c)
	}

	return hash12Hex(buf.Bytes())
}

func computeExtHash(extensions, sigAlgorithms []uint16) string {
	filteredExts := make([]uint16, 0, len(extensions))
	for _, e := range extensions {
		if e != extSNI && e != extALPN {
			filteredExts = append(filteredExts, e)
		}
	}

	if len(filteredExts) == 0 && len(sigAlgorithms) == 0 {
		return "000000000000"
	}

	slices.Sort(filteredExts)

	buf := acquireBuffer(len(filteredExts)*5 + len(sigAlgorithms)*5)
	defer releaseBuffer(buf)

	for i, e := range filteredExts {
		if i > 0 {
			buf.WriteByte(',')
		}

		writeHex4(buf, e)
	}

	if len(sigAlgorithms) > 0 {
		if len(filteredExts) > 0 {
			buf.WriteByte('_')
		}

		for i, s := range sigAlgorithms {
			if i > 0 {
				buf.WriteByte(',')
			}

			writeHex4(buf, s)
		}
	}

	return hash12Hex(buf.Bytes())
}

func writeHex4(buf *bytes.Buffer, v uint16) {
	_ = hexTable[15] // BCE hint: prove bounds for constant hexTable lookups

	buf.WriteByte(hexTable[(v>>12)&0x0f])
	buf.WriteByte(hexTable[(v>>8)&0x0f])
	buf.WriteByte(hexTable[(v>>4)&0x0f])
	buf.WriteByte(hexTable[v&0x0f])
}

// FilterGREASE removes reserved TLS GREASE values from vals.
func FilterGREASE(vals []uint16) []uint16 {
	result := make([]uint16, 0, len(vals))
	for _, v := range vals {
		if !IsGREASE(v) {
			result = append(result, v)
		}
	}

	return result
}

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

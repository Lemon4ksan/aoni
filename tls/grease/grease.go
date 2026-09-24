// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grease implements the Generate Random Extensions And Sustain Extensibility (GREASE)
// mechanism for TLS protocol points strictly conforming to RFC 8701.
//
// It provides definitions of reserved GREASE values for cipher suites, extensions, supported groups,
// signature algorithms, TLS versions, PSK key exchange modes, and ALPN identifiers, along with
// zero-allocation bitwise identification, filtering, and randomized value selection.
package grease

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"slices"
)

// ErrNegotiatedGREASE indicates that a peer illegally selected or negotiated a reserved GREASE value (RFC 8701 §3.1 & §4.1).
var ErrNegotiatedGREASE = errors.New("grease: peer illegally negotiated a reserved GREASE value (RFC 8701 §3.1)")

// Reserved 16-bit GREASE values defined in RFC 8701 §2 (Tables 1, 2, 3).
// Applicable to Cipher Suites, Extensions, Supported Groups, Signature Schemes, and Versions.
const (
	Value0A0A uint16 = 0x0A0A // 2570
	Value1A1A uint16 = 0x1A1A // 6682
	Value2A2A uint16 = 0x2A2A // 10794
	Value3A3A uint16 = 0x3A3A // 14906
	Value4A4A uint16 = 0x4A4A // 19018
	Value5A5A uint16 = 0x5A5A // 23130
	Value6A6A uint16 = 0x6A6A // 27242
	Value7A7A uint16 = 0x7A7A // 31354
	Value8A8A uint16 = 0x8A8A // 35466
	Value9A9A uint16 = 0x9A9A // 39578
	ValueAAAA uint16 = 0xAAAA // 43690
	ValueBABA uint16 = 0xBABA // 47802
	ValueCACA uint16 = 0xCACA // 51914
	ValueDADA uint16 = 0xDADA // 56026
	ValueEAEA uint16 = 0xEAEA // 60138
	ValueFAFA uint16 = 0xFAFA // 64250
)

// All16BitValues contains all 16 reserved 16-bit GREASE values in ascending order (RFC 8701 §2).
var All16BitValues = [...]uint16{
	Value0A0A, Value1A1A, Value2A2A, Value3A3A,
	Value4A4A, Value5A5A, Value6A6A, Value7A7A,
	Value8A8A, Value9A9A, ValueAAAA, ValueBABA,
	ValueCACA, ValueDADA, ValueEAEA, ValueFAFA,
}

// Reserved 8-bit GREASE values for PskKeyExchangeModes defined in RFC 8701 §2.
const (
	PskMode0B uint8 = 0x0B // 11
	PskMode2A uint8 = 0x2A // 42
	PskMode49 uint8 = 0x49 // 73
	PskMode68 uint8 = 0x68 // 104
	PskMode87 uint8 = 0x87 // 135
	PskModeA6 uint8 = 0xA6 // 166
	PskModeC5 uint8 = 0xC5 // 197
	PskModeE4 uint8 = 0xE4 // 228
)

// AllPskModes contains all 8 reserved 8-bit GREASE values for PskKeyExchangeModes in ascending order (RFC 8701 §2).
var AllPskModes = [...]uint8{
	PskMode0B, PskMode2A, PskMode49, PskMode68,
	PskMode87, PskModeA6, PskModeC5, PskModeE4,
}

// Is reports whether the 16-bit value v matches a reserved TLS GREASE value (RFC 8701 §2).
// Executed in 1 CPU cycle using zero-allocation bitwise mask verification:
// (v & 0x0F0F) == 0x0A0A and high-byte equals low-byte.
func Is(v uint16) bool {
	return (v&0x0F0F) == 0x0A0A && byte(v) == byte(v>>8)
}

// IsUint16 is an alias for [Is].
func IsUint16(v uint16) bool {
	return Is(v)
}

// IsUint8 reports whether the 8-bit value v matches a reserved PskKeyExchangeModes GREASE value (RFC 8701 §2).
func IsUint8(v uint8) bool {
	switch v {
	case PskMode0B, PskMode2A, PskMode49, PskMode68, PskMode87, PskModeA6, PskModeC5, PskModeE4:
		return true
	default:
		return false
	}
}

// IsBytes reports whether the 2-byte slice b represents a big-endian encoded GREASE value.
func IsBytes(b []byte) bool {
	if len(b) != 2 {
		return false
	}

	return Is(binary.BigEndian.Uint16(b))
}

// IsALPN reports whether the string represents a reserved 2-byte ALPN GREASE protocol identifier (RFC 8701 §2 & Table 4).
func IsALPN(alpn string) bool {
	if len(alpn) != 2 {
		return false
	}

	return alpn[0] == alpn[1] && (alpn[0]&0x0F) == 0x0A
}

// Filter removes reserved GREASE values from the given 16-bit slice, returning a newly allocated slice.
func Filter(vals []uint16) []uint16 {
	result := make([]uint16, 0, len(vals))
	for _, v := range vals {
		if !Is(v) {
			result = append(result, v)
		}
	}

	return result
}

// FilterInPlace removes reserved GREASE values from vals in-place without heap allocations.
func FilterInPlace(vals []uint16) []uint16 {
	return slices.DeleteFunc(vals, Is)
}

// FilterALPN removes reserved GREASE protocol identifiers from an ALPN list.
func FilterALPN(alpns []string) []string {
	result := make([]string, 0, len(alpns))
	for _, a := range alpns {
		if !IsALPN(a) {
			result = append(result, a)
		}
	}

	return result
}

// RandomUint16 selects a cryptographically secure random 16-bit GREASE value from [All16BitValues] (RFC 8701 §5).
func RandomUint16() uint16 {
	var b [1]byte
	_, _ = rand.Read(b[:])
	idx := int(b[0]) % len(All16BitValues)

	return All16BitValues[idx]
}

// RandomUint8 selects a cryptographically secure random 8-bit GREASE value from [AllPskModes] (RFC 8701 §5).
func RandomUint8() uint8 {
	var b [1]byte
	_, _ = rand.Read(b[:])
	idx := int(b[0]) % len(AllPskModes)

	return AllPskModes[idx]
}

// RandomALPN returns a 2-byte string containing a random ALPN GREASE identifier (RFC 8701 Table 4).
func RandomALPN() string {
	v := RandomUint16()

	return string([]byte{byte(v >> 8), byte(v)})
}

// RandomALPNBytes returns a 2-byte slice containing a random ALPN GREASE identifier.
func RandomALPNBytes() []byte {
	v := RandomUint16()

	return []byte{byte(v >> 8), byte(v)}
}

// ValidateServerNegotiation verifies that a server did not illegally negotiate or echo any GREASE values
// in its ServerHello version, selected cipher suite, or extensions (RFC 8701 §3.1 & §4.1).
func ValidateServerNegotiation(version, cipherSuite uint16, extensions []uint16) error {
	if Is(version) || Is(cipherSuite) || slices.ContainsFunc(extensions, Is) {
		return ErrNegotiatedGREASE
	}

	return nil
}

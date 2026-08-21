// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grease implements the Generate Random Extensions And Sustain Extensibility (GREASE)
// mechanism for TLS protocol points strictly conforming to RFC 8701.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/tls/grease].
package grease

import (
	fgrease "github.com/lemon4ksan/foundation/net/tls/grease"
)

// ErrNegotiatedGREASE indicates that a peer illegally selected or negotiated a reserved GREASE value.
var ErrNegotiatedGREASE = fgrease.ErrNegotiatedGREASE

// Reserved 16-bit GREASE values defined in RFC 8701 §2.
const (
	Value0A0A = fgrease.Value0A0A
	Value1A1A = fgrease.Value1A1A
	Value2A2A = fgrease.Value2A2A
	Value3A3A = fgrease.Value3A3A
	Value4A4A = fgrease.Value4A4A
	Value5A5A = fgrease.Value5A5A
	Value6A6A = fgrease.Value6A6A
	Value7A7A = fgrease.Value7A7A
	Value8A8A = fgrease.Value8A8A
	Value9A9A = fgrease.Value9A9A
	ValueAAAA = fgrease.ValueAAAA
	ValueBABA = fgrease.ValueBABA
	ValueCACA = fgrease.ValueCACA
	ValueDADA = fgrease.ValueDADA
	ValueEAEA = fgrease.ValueEAEA
	ValueFAFA = fgrease.ValueFAFA
)

// All16BitValues contains all 16 reserved 16-bit GREASE values in ascending order.
var All16BitValues = fgrease.All16BitValues

// Reserved 8-bit GREASE values for PskKeyExchangeModes defined in RFC 8701 §2.
const (
	PskMode0B = fgrease.PskMode0B
	PskMode2A = fgrease.PskMode2A
	PskMode49 = fgrease.PskMode49
	PskMode68 = fgrease.PskMode68
	PskMode87 = fgrease.PskMode87
	PskModeA6 = fgrease.PskModeA6
	PskModeC5 = fgrease.PskModeC5
	PskModeE4 = fgrease.PskModeE4
)

// AllPskModes contains all 8 reserved 8-bit GREASE values for PskKeyExchangeModes in ascending order.
var AllPskModes = fgrease.AllPskModes

// Is reports whether the 16-bit value v matches a reserved TLS GREASE value (RFC 8701 §2).
func Is(v uint16) bool {
	return fgrease.Is(v)
}

// IsUint16 is an alias for [Is].
func IsUint16(v uint16) bool {
	return fgrease.IsUint16(v)
}

// IsUint8 reports whether the 8-bit value v matches a reserved PskKeyExchangeModes GREASE value.
func IsUint8(v uint8) bool {
	return fgrease.IsUint8(v)
}

// IsBytes reports whether the 2-byte slice b represents a big-endian encoded GREASE value.
func IsBytes(b []byte) bool {
	return fgrease.IsBytes(b)
}

// IsALPN reports whether the string represents a reserved 2-byte ALPN GREASE protocol identifier.
func IsALPN(alpn string) bool {
	return fgrease.IsALPN(alpn)
}

// Filter removes reserved GREASE values from the given 16-bit slice, returning a newly allocated slice.
func Filter(vals []uint16) []uint16 {
	return fgrease.Filter(vals)
}

// FilterInPlace removes reserved GREASE values from vals in-place without heap allocations.
func FilterInPlace(vals []uint16) []uint16 {
	return fgrease.FilterInPlace(vals)
}

// FilterALPN removes reserved GREASE protocol identifiers from an ALPN list.
func FilterALPN(alpns []string) []string {
	return fgrease.FilterALPN(alpns)
}

// RandomUint16 selects a cryptographically secure random 16-bit GREASE value.
func RandomUint16() uint16 {
	return fgrease.RandomUint16()
}

// RandomUint8 selects a cryptographically secure random 8-bit GREASE value.
func RandomUint8() uint8 {
	return fgrease.RandomUint8()
}

// RandomALPN returns a 2-byte string containing a random ALPN GREASE identifier.
func RandomALPN() string {
	return fgrease.RandomALPN()
}

// RandomALPNBytes returns a 2-byte slice containing a random ALPN GREASE identifier.
func RandomALPNBytes() []byte {
	return fgrease.RandomALPNBytes()
}

// ValidateServerNegotiation verifies that a server did not illegally negotiate or echo any GREASE values.
func ValidateServerNegotiation(version, cipherSuite uint16, extensions []uint16) error {
	return fgrease.ValidateServerNegotiation(version, cipherSuite, extensions)
}

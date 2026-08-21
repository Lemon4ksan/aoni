// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package uuid provides zero-allocation RFC 9562 Universally Unique Identifiers (UUIDv4 and UUIDv7).
// Core implementation is located in [github.com/lemon4ksan/foundation/types/uuid].
package uuid

import (
	fuuid "github.com/lemon4ksan/foundation/types/uuid"
)

// StringLength is the character length of a standard hex-and-dash UUID string (RFC 9562 §4).
const StringLength = fuuid.StringLength

// Nil represents the empty, zeroed-out nil UUID (00000000-0000-0000-0000-000000000000).
var Nil = fuuid.Nil

// Max is the special Max UUID with all 128 bits set to one (RFC 9562 §5.10).
var Max = fuuid.Max

// Standard IANA Namespaces defined in RFC 9562 §6.6.
var (
	NamespaceDNS  = fuuid.NamespaceDNS
	NamespaceURL  = fuuid.NamespaceURL
	NamespaceOID  = fuuid.NamespaceOID
	NamespaceX500 = fuuid.NamespaceX500
)

// Standard error sentinels.
var (
	ErrInvalidLength = fuuid.ErrInvalidLength
	ErrInvalidFormat = fuuid.ErrInvalidFormat
	ErrScanType      = fuuid.ErrScanType
)

// UUID represents a 128-bit (16-byte) Universally Unique Identifier conforming to RFC 9562.
type UUID = fuuid.UUID

// IsValid checks whether s conforms to standard UUID format.
func IsValid(s string) bool {
	return fuuid.IsValid(s)
}

// NewV4 generates a cryptographically random UUID version 4 conforming to RFC 9562 §5.4.
func NewV4() (UUID, error) {
	return fuuid.NewV4()
}

// MustNewV4 generates a UUIDv4, panicking on entropy failure.
func MustNewV4() UUID {
	return fuuid.MustNewV4()
}

// NewV7 generates a time-ordered UUID version 7 conforming to RFC 9562 §5.7.
func NewV7() (UUID, error) {
	return fuuid.NewV7()
}

// MustNewV7 generates a UUIDv7, panicking on entropy failure.
func MustNewV7() UUID {
	return fuuid.MustNewV7()
}

// Parse parses a 36-character hyphenated string into a [UUID].
func Parse(s string) (UUID, error) {
	return fuuid.Parse(s)
}

// MustParse parses a UUID string, panicking if the input is invalid.
func MustParse(s string) UUID {
	return fuuid.MustParse(s)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package uuid implements Universally Unique IDentifiers (UUIDs) strictly conforming to RFC 9562 (obsoletes RFC 4122).
//
// It provides zero-allocation parsing, formatting, and generation for UUIDv4 (random) and UUIDv7 (time-ordered),
// along with predefined IANA namespaces and sentinel Nil/Max UUID values.
package uuid

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// StringLength is the character length of a standard hex-and-dash UUID string (RFC 9562 §4).
const StringLength = 36

// UUID represents a 128-bit (16-octet) Universally Unique Identifier in big-endian network byte order (RFC 9562 §4).
type UUID [16]byte

// Nil is the special Nil UUID with all 128 bits set to zero (RFC 9562 §5.9).
var Nil = UUID{}

// Max is the special Max UUID with all 128 bits set to one (RFC 9562 §5.10).
var Max = UUID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// Predefined IANA UUID Namespace IDs per RFC 9562 §6.6 (Table 3).
var (
	// NamespaceDNS is the predefined namespace UUID for fully qualified domain names (RFC 9562 §6.6).
	NamespaceDNS = MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	// NamespaceURL is the predefined namespace UUID for URLs (RFC 9562 §6.6).
	NamespaceURL = MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8")

	// NamespaceOID is the predefined namespace UUID for ISO/IEC Object Identifiers (RFC 9562 §6.6).
	NamespaceOID = MustParse("6ba7b812-9dad-11d1-80b4-00c04fd430c8")

	// NamespaceX500 is the predefined namespace UUID for X.500 Distinguished Names (RFC 9562 §6.6).
	NamespaceX500 = MustParse("6ba7b814-9dad-11d1-80b4-00c04fd430c8")
)

var (
	// ErrInvalidLength is returned when a UUID string has an invalid length (RFC 9562 §4).
	ErrInvalidLength = errors.New("aoni/uuid: invalid UUID string length")

	// ErrInvalidFormat is returned when a UUID string does not conform to 8-4-4-4-12 hex format (RFC 9562 §4).
	ErrInvalidFormat = errors.New("aoni/uuid: invalid UUID hex-and-dash format")
)

// IsValid checks whether s conforms to the standard 36-character "8-4-4-4-12"
// hex-and-dash UUID string format defined in RFC 9562 §4.
func IsValid(s string) bool {
	if len(s) != StringLength {
		return false
	}

	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}

	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}

		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

// Parse parses a standard 36-character "8-4-4-4-12" hex-and-dash UUID string (RFC 9562 §4)
// into a 16-byte [UUID] value. It accepts lowercase, uppercase, and mixed-case hexadecimal characters.
func Parse(s string) (UUID, error) {
	var u UUID
	if len(s) != StringLength {
		return u, ErrInvalidLength
	}

	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, ErrInvalidFormat
	}

	// Decode 8 hex chars (4 bytes)
	if _, err := hex.Decode(u[0:4], []byte(s[0:8])); err != nil {
		return u, ErrInvalidFormat
	}

	// Decode 4 hex chars (2 bytes)
	if _, err := hex.Decode(u[4:6], []byte(s[9:13])); err != nil {
		return u, ErrInvalidFormat
	}

	// Decode 4 hex chars (2 bytes)
	if _, err := hex.Decode(u[6:8], []byte(s[14:18])); err != nil {
		return u, ErrInvalidFormat
	}

	// Decode 4 hex chars (2 bytes)
	if _, err := hex.Decode(u[8:10], []byte(s[19:23])); err != nil {
		return u, ErrInvalidFormat
	}

	// Decode 12 hex chars (6 bytes)
	if _, err := hex.Decode(u[10:16], []byte(s[24:36])); err != nil {
		return u, ErrInvalidFormat
	}

	return u, nil
}

// MustParse parses a UUID string, panicking if the string cannot be parsed.
func MustParse(s string) UUID {
	u, err := Parse(s)
	if err != nil {
		panic(fmt.Sprintf("aoni/uuid: MustParse(%q): %v", s, err))
	}

	return u
}

// NewV4 generates a cryptographically random UUID version 4 conforming to RFC 9562 §5.4.
func NewV4() (UUID, error) {
	var u UUID
	if _, err := rand.Read(u[:]); err != nil {
		return u, fmt.Errorf("aoni/uuid: uuidv4 entropy generation failed: %w", err)
	}

	// Set version 4 (0b0100) in bits 48-51 (high nibble of octet 6)
	u[6] = (u[6] & 0x0F) | 0x40

	// Set variant 10xx (0b10) in bits 64-65 (high 2 bits of octet 8)
	u[8] = (u[8] & 0x3F) | 0x80

	return u, nil
}

// MustNewV4 generates a cryptographically random UUIDv4, panicking on entropy failure.
func MustNewV4() UUID {
	u, err := NewV4()
	if err != nil {
		panic(err)
	}

	return u
}

// NewV7 generates a time-ordered Unix Epoch millisecond UUID version 7 conforming to RFC 9562 §5.7.
// It encodes the current UTC millisecond timestamp in the top 48 bits and CSPRNG entropy in the remaining 74 bits.
func NewV7(t ...time.Time) (UUID, error) {
	now := time.Now().UTC()
	if len(t) > 0 && !t[0].IsZero() {
		now = t[0].UTC()
	}

	var u UUID
	if _, err := rand.Read(u[6:]); err != nil {
		return u, fmt.Errorf("aoni/uuid: uuidv7 entropy generation failed: %w", err)
	}

	// Encode 48-bit big-endian millisecond timestamp (bits 0-47)
	ms := uint64(now.UnixMilli())
	binary.BigEndian.PutUint32(u[0:4], uint32(ms>>16))
	binary.BigEndian.PutUint16(u[4:6], uint16(ms&0xFFFF))

	// Set version 7 (0b0111) in bits 48-51 (high nibble of octet 6)
	u[6] = (u[6] & 0x0F) | 0x70

	// Set variant 10xx (0b10) in bits 64-65 (high 2 bits of octet 8)
	u[8] = (u[8] & 0x3F) | 0x80

	return u, nil
}

// MustNewV7 generates a time-ordered UUIDv7, panicking on entropy failure.
func MustNewV7(t ...time.Time) UUID {
	u, err := NewV7(t...)
	if err != nil {
		panic(err)
	}

	return u
}

// String returns the canonical lowercase 36-character "8-4-4-4-12" hex-and-dash string representation (RFC 9562 §4).
func (u UUID) String() string {
	var buf [StringLength]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])

	return string(buf[:])
}

// Append appends the canonical lowercase 36-character hex-and-dash string representation
// to dst with 0 heap allocations and returns the resulting slice.
func (u UUID) Append(dst []byte) []byte {
	var buf [StringLength]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])

	return append(dst, buf[:]...)
}

// Version extracts the 4-bit version number from the UUID (RFC 9562 §4.2).
// Bits 48 through 51 (high nibble of octet 6).
func (u UUID) Version() int {
	return int(u[6] >> 4)
}

// Variant extracts the variant number from the UUID (RFC 9562 §4.1).
// For the RFC 9562 standard variant (10xx), returns 2.
func (u UUID) Variant() int {
	switch {
	case (u[8] & 0x80) == 0x00:
		return 0 // NCS backward compatibility (0xxx)
	case (u[8] & 0xC0) == 0x80:
		return 2 // RFC 9562 / RFC 4122 (10xx)
	case (u[8] & 0xE0) == 0xC0:
		return 6 // Microsoft backward compatibility (110x)
	default:
		return 7 // Reserved for future definition (111x)
	}
}

// Time extracts the embedded UTC timestamp from a UUIDv7 (RFC 9562 §5.7).
// Returns zero time if the UUID version is not 7.
func (u UUID) Time() time.Time {
	if u.Version() != 7 {
		return time.Time{}
	}

	hi := uint64(binary.BigEndian.Uint32(u[0:4]))
	lo := uint64(binary.BigEndian.Uint16(u[4:6]))
	ms := (hi << 16) | lo

	return time.UnixMilli(int64(ms)).UTC()
}

// IsNil returns true if all 128 bits are zero (RFC 9562 §5.9).
func (u UUID) IsNil() bool {
	return u == Nil
}

// IsMax returns true if all 128 bits are one (RFC 9562 §5.10).
func (u UUID) IsMax() bool {
	return u == Max
}

// MarshalText implements [encoding.TextMarshaler] for JSON, XML, and text serialization.
func (u UUID) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler] for JSON, XML, and text deserialization.
func (u *UUID) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}

	*u = parsed

	return nil
}

// MarshalBinary implements [encoding.BinaryMarshaler].
func (u UUID) MarshalBinary() ([]byte, error) {
	return u[:], nil
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler].
func (u *UUID) UnmarshalBinary(data []byte) error {
	if len(data) != 16 {
		return ErrInvalidLength
	}

	copy(u[:], data)

	return nil
}

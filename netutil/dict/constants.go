// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict

const (
	// HeaderUseAsDictionary is the response header used by servers to advertise a response
	// as a compression dictionary (RFC 9842 §2.1).
	HeaderUseAsDictionary = "Use-As-Dictionary"

	// HeaderAvailableDictionary is the request header containing the SHA-256 hash of the
	// available dictionary in RFC 8941 Structured Field Byte Sequence format (RFC 9842 §2.2).
	HeaderAvailableDictionary = "Available-Dictionary"

	// HeaderDictionaryID is the request header echoing the server-provided dictionary identifier (RFC 9842 §2.3).
	HeaderDictionaryID = "Dictionary-ID"

	// ContentEncodingDCB is the Content-Encoding token for Dictionary-Compressed Brotli (RFC 9842 §4 & §7.1).
	ContentEncodingDCB = "dcb"

	// ContentEncodingDCZ is the Content-Encoding token for Dictionary-Compressed Zstandard (RFC 9842 §5 & §7.1).
	ContentEncodingDCZ = "dcz"

	// LinkRelCompressionDictionary is the Link relation type for prefetching compression dictionaries (RFC 9842 §3).
	LinkRelCompressionDictionary = "compression-dictionary"

	// TypeRaw is the default dictionary format representing an unformatted byte blob (RFC 9842 §2.1.4).
	TypeRaw = "raw"

	// MaxIDLength is the maximum permitted length for a Dictionary-ID string (RFC 9842 §2.1.3 & §2.3).
	MaxIDLength = 1024

	// DefaultMaxDictionarySize is the default maximum dictionary size (16 MB) to prevent resource exhaustion.
	DefaultMaxDictionarySize = 16 * 1024 * 1024

	// DefaultMaxStoreBytes is the default maximum memory capacity for cached dictionaries (64 MB).
	DefaultMaxStoreBytes = 64 * 1024 * 1024
)

var (
	// MagicDCB is the 4-byte magic sequence for Dictionary-Compressed Brotli (RFC 9842 §4).
	// Fixed bytes: 0xff, 0x44, 0x43, 0x42 ('\xffDCB').
	MagicDCB = [4]byte{0xff, 0x44, 0x43, 0x42}

	// MagicDCZ is the 8-byte magic sequence for Dictionary-Compressed Zstandard (RFC 9842 §5).
	// Little-endian 0x184D2A5E (skippable frame) with 32-byte length (0x00000020).
	// Fixed bytes: 0x5e, 0x2a, 0x4d, 0x18, 0x20, 0x00, 0x00, 0x00.
	MagicDCZ = [8]byte{0x5e, 0x2a, 0x4d, 0x18, 0x20, 0x00, 0x00, 0x00}
)

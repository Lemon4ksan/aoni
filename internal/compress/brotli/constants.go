// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

// RFC 7932 Brotli protocol constants.
const (
	// Specification: 3.3. Alphabet sizes: insert-and-copy length
	numLiteralSymbols  = 256
	numCommandSymbols  = 704
	numBlockLenSymbols = 26

	// Specification: 3.5. Complex prefix codes
	repeatPreviousCodeLength  = 16
	repeatZeroCodeLength      = 17
	codeLengthCodes           = repeatZeroCodeLength + 1
	initialRepeatedCodeLength = 8

	// Large Window Brotli extension constants
	largeMaxDistanceBits = 62
	largeMinWbits        = 10
	largeMaxWbits        = 30

	// Specification: 4. Encoding of distances
	numDistanceShortCodes = 16
	maxNpostfix           = 3
	maxDistanceBits       = 24
	numDistanceSymbols    = 1128
	maxDistance           = 0x3FFFFFC
	maxAllowedDistance    = 0x7FFFFFFC

	// Specification: 7.1 & 7.2 Context modes and context IDs
	literalContextBits  = 6
	distanceContextBits = 2

	// Specification: 9.1. Format of the Stream Header
	windowGap = 16
)

// distanceAlphabetSize calculates the alphabet size for distance prefix codes.
func distanceAlphabetSize(nPostfix, nDirect, maxNBits uint32) uint32 {
	return numDistanceShortCodes + nDirect + (maxNBits << (nPostfix + 1))
}

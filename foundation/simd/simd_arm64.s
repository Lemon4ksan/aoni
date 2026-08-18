// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build arm64 && !purego

#include "textflag.h"

// func indexByteNEON(b []byte, c byte) int
// b_ptr: R0, b_len: R1, c: R3
TEXT ·indexByteNEON(SB), NOSPLIT, $0-40
	MOVD b_ptr+0(FP), R0
	MOVD b_len+8(FP), R1
	MOVBU c+24(FP), R3

	CMP $16, R1
	BLT fallback

	// Duplicate byte c across 16-byte V0 register
	VMOV R3, V0.B16

loop16:
	CMP $16, R1
	BLT fallback

	// Load 16 bytes into V1
	VLD1 (R0), [V1.B16]

	// Compare equal V1 == V0 -> V2
	VCMEQ V0.B16, V1.B16, V2.B16

	// Check if any lane matched
	VMAXV V2.B16, V3.B16
	VMOV V3.B[0], R4
	CBZ R4, next16

	// Match found in current 16-byte window, scan scalar
	MOVD b_ptr+0(FP), R5
	SUB R5, R0, R6

scalar_scan:
	MOVBU (R0), R7
	CMP R3, R7
	BEQ found
	ADD $1, R0
	SUB $1, R1
	B scalar_scan

next16:
	ADD $16, R0
	SUB $16, R1
	B loop16

fallback:
	MOVD $-1, R0
	MOVD R0, ret+32(FP)
	RET

found:
	MOVD b_ptr+0(FP), R5
	SUB R5, R0, R0
	MOVD R0, ret+32(FP)
	RET

// func indexTwoBytesNEON(b []byte, c1, c2 byte) int
// b_ptr: R0, b_len: R1, c1: R3, c2: R4
TEXT ·indexTwoBytesNEON(SB), NOSPLIT, $0-48
	MOVD b_ptr+0(FP), R0
	MOVD b_len+8(FP), R1
	MOVBU c1+24(FP), R3
	MOVBU c2+25(FP), R4

	CMP $16, R1
	BLT fallback2

	// Duplicate c1 and c2 across V0 and V1
	VMOV R3, V0.B16
	VMOV R4, V1.B16

loop2_16:
	CMP $16, R1
	BLT fallback2

	VLD1 (R0), [V2.B16]

	VCMEQ V0.B16, V2.B16, V3.B16
	VCMEQ V1.B16, V2.B16, V4.B16
	VORR V3.B16, V4.B16, V5.B16

	VMAXV V5.B16, V6.B16
	VMOV V6.B[0], R5
	CBZ R5, next2_16

scalar_scan2:
	MOVBU (R0), R7
	CMP R3, R7
	BEQ found2
	CMP R4, R7
	BEQ found2
	ADD $1, R0
	SUB $1, R1
	B scalar_scan2

next2_16:
	ADD $16, R0
	SUB $16, R1
	B loop2_16

fallback2:
	MOVD $-1, R0
	MOVD R0, ret+32(FP)
	RET

found2:
	MOVD b_ptr+0(FP), R6
	SUB R6, R0, R0
	MOVD R0, ret+32(FP)
	RET

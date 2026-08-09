// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

#include "textflag.h"

// func indexByteAVX2(b []byte, c byte) int
// Registers:
//   AX = b_base
//   BX = b_len
//   CX = target byte
TEXT ·indexByteAVX2(SB), NOSPLIT, $0-40
	MOVQ b_base+0(FP), AX
	MOVQ b_len+8(FP), BX
	MOVB c+24(FP), CX

	CMPQ BX, $32
	JL fallback_small

	// Broadcast target search byte across 256-bit XMM0 -> YMM0 vector register
	MOVD CX, X0
	VPBROADCASTB X0, Y0

loop32:
	VMOVDQU (AX), Y1
	VPCMPEQB Y0, Y1, Y2
	VPMOVMSKB Y2, DX

	TESTL DX, DX
	JNZ found

	ADDQ $32, AX
	SUBQ $32, BX
	CMPQ BX, $32
	JGE loop32

fallback_small:
	VZEROUPPER
	MOVQ $-1, ret+32(FP)
	RET

found:
	BSFL DX, DX
	MOVQ b_base+0(FP), CX
	SUBQ CX, AX
	ADDQ DX, AX
	VZEROUPPER
	MOVQ AX, ret+32(FP)
	RET

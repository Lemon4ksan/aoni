// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

#include "textflag.h"

// func matchLenAVX2(a, b []byte) int
// Registers:
//   AX = a_base
//   BX = a_len
//   CX = b_base
//   DX = b_len
//   SI = match count n
TEXT ·matchLenAVX2(SB), NOSPLIT, $0-56
	MOVQ a_base+0(FP), AX
	MOVQ a_len+8(FP), BX
	MOVQ b_base+24(FP), CX
	MOVQ b_len+32(FP), DX

	// BX = min(a_len, b_len)
	CMPQ BX, DX
	CMOVQGT DX, BX

	XORQ SI, SI

	CMPQ BX, $32
	JL tail

loop32:
	VMOVDQU (AX)(SI*1), Y0
	VMOVDQU (CX)(SI*1), Y1
	VPCMPEQB Y0, Y1, Y2
	VPMOVMSKB Y2, DX

	NOTL DX
	TESTL DX, DX
	JNZ found

	ADDQ $32, SI
	SUBQ $32, BX
	CMPQ BX, $32
	JGE loop32

tail:
	VZEROUPPER
	MOVQ SI, ret+48(FP)
	RET

found:
	BSFL DX, DX
	ADDQ DX, SI
	VZEROUPPER
	MOVQ SI, ret+48(FP)
	RET

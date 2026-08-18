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

// func indexTwoBytesAVX2(b []byte, c1, c2 byte) int
TEXT ·indexTwoBytesAVX2(SB), NOSPLIT, $0-48
	MOVQ b_base+0(FP), AX
	MOVQ b_len+8(FP), BX
	MOVB c1+24(FP), CX
	MOVB c2+25(FP), DX

	CMPQ BX, $32
	JL fallback_two_small

	MOVD CX, X0
	VPBROADCASTB X0, Y0

	MOVD DX, X1
	VPBROADCASTB X1, Y1

loop32_two:
	VMOVDQU (AX), Y2
	VPCMPEQB Y0, Y2, Y3
	VPCMPEQB Y1, Y2, Y4
	VPOR Y3, Y4, Y5
	VPMOVMSKB Y5, DX

	TESTL DX, DX
	JNZ found_two

	ADDQ $32, AX
	SUBQ $32, BX
	CMPQ BX, $32
	JGE loop32_two

fallback_two_small:
	VZEROUPPER
	MOVQ $-1, ret+32(FP)
	RET

found_two:
	BSFL DX, DX
	MOVQ b_base+0(FP), CX
	SUBQ CX, AX
	ADDQ DX, AX
	VZEROUPPER
	MOVQ AX, ret+32(FP)
	RET

// func applyFastMaskAVX2(b []byte, mask uint32)
TEXT ·applyFastMaskAVX2(SB), NOSPLIT, $0-28
	MOVQ b_base+0(FP), AX
	MOVQ b_len+8(FP), BX
	MOVL mask+24(FP), CX

	CMPQ BX, $32
	JL mask_fallback

	// Broadcast 4-byte mask across 256-bit YMM0 vector register
	MOVD CX, X0
	VPBROADCASTD X0, Y0

mask_loop32:
	VMOVDQU (AX), Y1
	VPXOR Y0, Y1, Y2
	VMOVDQU Y2, (AX)

	ADDQ $32, AX
	SUBQ $32, BX
	CMPQ BX, $32
	JGE mask_loop32

mask_fallback:
	VZEROUPPER
	RET

// func pext64(val, mask uint64) uint64
TEXT ·pext64(SB), NOSPLIT, $0-24
	MOVQ val+0(FP), AX
	MOVQ mask+8(FP), CX
	PEXTQ CX, AX, DX
	MOVQ DX, ret+16(FP)
	RET

// func pdep64(val, mask uint64) uint64
TEXT ·pdep64(SB), NOSPLIT, $0-24
	MOVQ val+0(FP), AX
	MOVQ mask+8(FP), CX
	PDEPQ CX, AX, DX
	MOVQ DX, ret+16(FP)
	RET

// func prefetchL1(ptr unsafe.Pointer)
TEXT ·prefetchL1(SB), NOSPLIT, $0-8
	MOVQ ptr+0(FP), AX
	PREFETCHT0 (AX)
	RET

// func streamCopy256(dst, src []byte)
TEXT ·streamCopy256(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), AX
	MOVQ src_base+24(FP), BX
	MOVQ src_len+32(FP), CX

	CMPQ CX, $32
	JL stream_fallback

stream_loop32:
	VMOVDQU (BX), Y0
	VMOVNTDQ Y0, (AX)

	ADDQ $32, AX
	ADDQ $32, BX
	SUBQ $32, CX
	CMPQ CX, $32
	JGE stream_loop32

	SFENCE

stream_fallback:
	VZEROUPPER
	RET

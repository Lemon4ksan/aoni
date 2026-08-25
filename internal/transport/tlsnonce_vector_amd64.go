// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package transport

import (
	"unsafe"
)

const hasVectorTLSNonce = true

func vectorComputeNonceXOR(iv []byte, seq uint64, dst *[12]byte) {
	tls13_compute_nonce(
		uint64(uintptr(unsafe.Pointer(&iv[0]))),
		seq,
		uint64(uintptr(unsafe.Pointer(&dst[0]))),
		0,
		0,
		0,
	)
}

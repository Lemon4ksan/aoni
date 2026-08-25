// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package transport

import "encoding/binary"

const hasVectorTLSNonce = false

func vectorComputeNonceXOR(iv []byte, seq uint64, dst *[12]byte) {
	copy(dst[:], iv)
	binary.BigEndian.PutUint64(dst[4:12], binary.BigEndian.Uint64(dst[4:12])^seq)
}

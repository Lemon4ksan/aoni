// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package transport

import (
	"unsafe"
)

const hasVectorBase64 = true

var b64URLCharset = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")

func vectorBase64EncodeURL(src, dst []byte) int {
	n := len(src)
	if n == 0 {
		return 0
	}

	res := base64_encode_url(
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(n),
		uint64(uintptr(unsafe.Pointer(&dst[0]))),
		uint64(uintptr(unsafe.Pointer(&b64URLCharset[0]))),
		0,
		0,
	)

	return int(res)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"encoding/base64"
)

// Base64URLEncodedLen returns the unpadded URL-safe Base64 encoded length for n source bytes.
func Base64URLEncodedLen(n int) int {
	return (n*8 + 5) / 6
}

// Base64EncodeURL writes the unpadded URL-safe Base64 representation of src into dst.
// Returns the number of bytes written to dst.
func Base64EncodeURL(src, dst []byte) int {
	reqLen := Base64URLEncodedLen(len(src))
	if len(dst) < reqLen {
		panic("transport: dst buffer too small for Base64EncodeURL")
	}

	if hasVectorBase64 {
		return vectorBase64EncodeURL(src, dst)
	}

	enc := base64.RawURLEncoding
	enc.Encode(dst, src)

	return reqLen
}

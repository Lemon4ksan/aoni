// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package transport

import "encoding/base64"

const hasVectorBase64 = false

func vectorBase64EncodeURL(src, dst []byte) int {
	enc := base64.RawURLEncoding
	enc.Encode(dst, src)
	return enc.EncodedLen(len(src))
}

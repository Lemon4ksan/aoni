// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

func addLeadingSlash(dst, src []byte) []byte {
	// zero length 、"C:/" and "a" case
	isDisk := len(src) > 2 && src[1] == ':'
	if len(src) == 0 || (!isDisk && src[0] != '/') {
		dst = append(dst, '/')
	}

	return dst
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package h1engine

func addLeadingSlash(dst, src []byte) []byte {
	// add leading slash for unix paths
	if len(src) == 0 || src[0] != '/' {
		dst = append(dst, '/')
	}

	return dst
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package qpack

const hasVectorInt = false

func vectorAppendInt(dst []byte, prefixLen uint8, val uint64) []byte {
	return dst
}

func vectorReadInt(prefixLen uint8, data []byte) (uint64, int, error) {
	return 0, 0, nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package h2engine

const hasVectorInt = false

func vectorAppendInt(dst []byte, bits uint8, index uint64) []byte {
	return dst
}

func vectorReadInt(n int, b []byte) ([]byte, uint64) {
	return b, 0
}

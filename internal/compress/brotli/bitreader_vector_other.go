// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package brotli

const hasVectorBitReader = false

func (br *bitReader) vectorFillBitWindow() {
}

func (br *bitReader) vectorReadBits(nBits uint32) uint32 {
	return 0
}

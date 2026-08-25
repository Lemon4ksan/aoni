// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package fse

const hasVectorFSE = false

func vectorDecodeQuad(s1, s2 *decoder, dt []decSymbol, tmp []byte, off uint8) uint8 {
	return off
}

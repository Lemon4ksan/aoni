// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !appengine && gc && !purego && !noasm
// +build amd64 arm64
// +build !appengine
// +build gc
// +build !purego
// +build !noasm

package xxhash

// Sum64 computes the 64-bit xxHash digest of b.
//
//go:noescape
func Sum64(b []byte) uint64

//go:noescape
func writeBlocks(s *Digest, b []byte) int

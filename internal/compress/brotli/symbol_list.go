// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

// symbolList provides a memory-efficient indexed window over a symbol slice
// used during Huffman decoding table construction.
type symbolList struct {
	storage []uint16
	offset  int
}

func (s symbolList) get(i int) uint16 {
	return s.storage[i+s.offset]
}

func (s symbolList) put(i int, val uint16) {
	s.storage[i+s.offset] = val
}

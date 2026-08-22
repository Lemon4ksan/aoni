// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xxhash

// Sum64String computes the 64-bit xxHash digest of s.
func Sum64String(s string) uint64 {
	return Sum64([]byte(s))
}

// WriteString adds more data to d. It always returns len(s), nil.
func (d *Digest) WriteString(s string) (n int, err error) {
	return d.Write([]byte(s))
}

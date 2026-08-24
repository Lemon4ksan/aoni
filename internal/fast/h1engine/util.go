// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// b2s converts byte slice to a string without memory allocation using foundation.
func b2s(b []byte) string {
	return bytesconv.B2S(b)
}

// s2b converts string to a byte slice without memory allocation using foundation.
func s2b(s string) []byte {
	return bytesconv.S2B(s)
}

// noCopy may be embedded into structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

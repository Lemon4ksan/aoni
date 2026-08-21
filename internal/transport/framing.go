// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	fgrpcweb "github.com/lemon4ksan/foundation/net/grpcweb"
)

var (
	ErrPayloadTooLarge  = fgrpcweb.ErrPayloadTooLarge
	ErrTruncatedHeader  = fgrpcweb.ErrTruncatedHeader
	ErrTruncatedPayload = fgrpcweb.ErrTruncatedPayload
)

// LengthPrefixedFramer decodes and encodes 5-byte length-prefixed frames:
// [1 byte flags][4 bytes length (BigEndian)][payload bytes].
type LengthPrefixedFramer = fgrpcweb.Framer

// NewLengthPrefixedFramer initializes a [LengthPrefixedFramer].
func NewLengthPrefixedFramer(maxPayloadSize uint32) *LengthPrefixedFramer {
	return fgrpcweb.NewFramer(maxPayloadSize)
}

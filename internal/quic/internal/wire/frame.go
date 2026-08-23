// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

// A Frame in QUIC
type Frame interface {
	Append(b []byte, version protocol.Version) ([]byte, error)
	Length(version protocol.Version) protocol.ByteCount
}

// IsProbingFrame returns true if the frame is a probing frame.
// See section 9.1 of RFC 9000.
func IsProbingFrame(f Frame) bool {
	switch f.(type) {
	case *PathChallengeFrame, *PathResponseFrame, *NewConnectionIDFrame:
		return true
	}

	return false
}

// IsProbingFrameType returns true if the FrameType is a probing frame.
// See section 9.1 of RFC 9000.
func IsProbingFrameType(f FrameType) bool {
	//nolint:exhaustive // PATH_CHALLENGE, PATH_RESPONSE and NEW_CONNECTION_ID are the only probing frames
	switch f {
	case FrameTypePathChallenge, FrameTypePathResponse, FrameTypeNewConnectionID:
		return true
	default:
		return false
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestProbingFrames(t *testing.T) {
	testCases := map[Frame]bool{
		&AckFrame{}:             false,
		&ConnectionCloseFrame{}: false,
		&DataBlockedFrame{}:     false,
		&PingFrame{}:            false,
		&ResetStreamFrame{}:     false,
		&StreamFrame{}:          false,
		&DatagramFrame{}:        false,
		&MaxDataFrame{}:         false,
		&MaxStreamDataFrame{}:   false,
		&StopSendingFrame{}:     false,
		&PathChallengeFrame{}:   true,
		&PathResponseFrame{}:    true,
		&NewConnectionIDFrame{}: true,
	}

	for f, expected := range testCases {
		require.Equal(t, expected, IsProbingFrame(f))
	}
}

func TestIsProbingFrameType(t *testing.T) {
	tests := map[FrameType]bool{
		FrameTypePathChallenge:   true,
		FrameTypePathResponse:    true,
		FrameTypeNewConnectionID: true,
		FrameType(0x01):          false,
		FrameType(0xFF):          false,
	}
	for ft, expected := range tests {
		require.Equal(t, expected, IsProbingFrameType(ft))
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

// A PathChallengeFrame is a PATH_CHALLENGE frame
type PathChallengeFrame struct {
	Data [8]byte
}

func parsePathChallengeFrame(b []byte, _ protocol.Version) (*PathChallengeFrame, int, error) {
	f := &PathChallengeFrame{}

	if len(b) < 8 {
		return nil, 0, io.EOF
	}

	copy(f.Data[:], b)

	return f, 8, nil
}

func (f *PathChallengeFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = append(b, byte(FrameTypePathChallenge))
	b = append(b, f.Data[:]...)
	return b, nil
}

// Length of a written frame
func (f *PathChallengeFrame) Length(_ protocol.Version) protocol.ByteCount {
	return 1 + 8
}

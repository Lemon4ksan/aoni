// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

// A HandshakeDoneFrame is a HANDSHAKE_DONE frame
type HandshakeDoneFrame struct{}

func (f *HandshakeDoneFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	return append(b, byte(FrameTypeHandshakeDone)), nil
}

// Length of a written frame
func (f *HandshakeDoneFrame) Length(_ protocol.Version) protocol.ByteCount {
	return 1
}

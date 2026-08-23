// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestWriteHandshakeDoneSampleFrame(t *testing.T) {
	frame := HandshakeDoneFrame{}
	b, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, []byte{byte(FrameTypeHandshakeDone)}, b)
	require.Equal(t, protocol.ByteCount(1), frame.Length(protocol.Version1))
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestImmediateAckFrame(t *testing.T) {
	frame := ImmediateAckFrame{}
	b, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)

	val, l, err := quicvarint.Parse(b)
	require.NoError(t, err)
	require.Equal(t, uint64(FrameTypeImmediateAck), val)
	require.Equal(t, len(b), l)

	require.Len(t, b, int(frame.Length(protocol.Version1)))
}

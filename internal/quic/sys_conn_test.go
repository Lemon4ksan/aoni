// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/gomock"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestBasicConn(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	c := NewMockPacketConn(mockCtrl)
	addr := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234}
	c.EXPECT().ReadFrom(gomock.Any()).DoAndReturn(func(b []byte) (int, net.Addr, error) {
		data := []byte("foobar")

		require.Equal(t, protocol.MaxPacketBufferSize, len(b))

		return copy(b, data), addr, nil
	})

	conn, err := wrapConn(c)
	require.NoError(t, err)
	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("foobar"), p.data)
	require.WithinDuration(t, time.Now(), p.rcvTime.ToTime(), scaleDuration(100*time.Millisecond))
	require.Equal(t, addr, p.remoteAddr)
}

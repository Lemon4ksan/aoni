// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"net"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestRepro_H3Client_RemoveConnClosesConnection(t *testing.T) {
	t.Parallel()

	client := NewClient(nil, nil)
	defer client.Close()

	// Mock or allocate a ClientConn
	udpConn, err := net.ListenUDP("udp", nil)
	require.NoError(t, err)

	defer udpConn.Close()

	cc := &ClientConn{
		closed: make(chan struct{}),
	}

	client.mutex.Lock()
	client.conns["example.com:443"] = cc
	client.mutex.Unlock()

	// When removeConn is called, the connection must be closed
	client.removeConn("example.com:443")

	assert.True(t, cc.isClosed())
}

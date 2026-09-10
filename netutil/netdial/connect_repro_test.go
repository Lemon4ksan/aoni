// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial

import (
	"net"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestConnectProxy_ExtraBufferedDataRejection_Repro(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	go func() {
		buf := make([]byte, 1024)
		_, _ = serverConn.Read(buf)
		_, _ = serverConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\nEVIL_PAYLOAD"))
	}()

	conn, err := handshakeHTTPProxy(clientConn, "example.com", "443")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProxyConnectFailed)
	assert.Nil(t, conn)
}

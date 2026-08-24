// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/fragment"
)

func TestWrapWithMSSLimit_NegativeMSS(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })

	wrapped := applyMSSLimit(c1, -100)
	assert.NotNil(t, wrapped, "negative MSS size should be ignored gracefully")
}

func TestFragmentedConn_Write(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })

	cfg := fragment.Config{
		ChunkSize: 2,
	}

	fragmented := fragment.NewFragmentedConn(c1, &cfg)

	type writeResult struct {
		n   int
		err error
	}

	ch := make(chan writeResult, 1)

	go func() {
		n, err := fragmented.Write([]byte("test"))
		ch <- writeResult{n: n, err: err}
	}()

	buf := make([]byte, 2)
	n, err := io.ReadFull(c2, buf)
	require.NoError(t, err)
	assert.Equal(t, "te", string(buf[:n]))

	n, err = io.ReadFull(c2, buf)
	require.NoError(t, err)
	assert.Equal(t, "st", string(buf[:n]))

	res := <-ch
	require.NoError(t, res.err)
	assert.Equal(t, 4, res.n)
}

func TestApplyMSSLimit(t *testing.T) {
	t.Parallel()

	t.Run("zero_or_negative_mss", func(t *testing.T) {
		t.Parallel()

		_, client := net.Pipe()
		res := applyMSSLimit(client, 0)
		assert.Equal(t, client, res)
		_ = client.Close()
	})

	t.Run("non_tcp_conn", func(t *testing.T) {
		t.Parallel()

		_, client := net.Pipe()
		res := applyMSSLimit(client, 512)
		assert.Equal(t, client, res)
		_ = client.Close()
	})

	t.Run("real_tcp_conn", func(t *testing.T) {
		t.Parallel()

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })

		var (
			serverConn net.Conn
			acceptErr  error
		)

		done := make(chan struct{})

		go func() {
			defer close(done)

			serverConn, acceptErr = listener.Accept()
		}()

		clientConn, err := net.Dial("tcp", listener.Addr().String())
		require.NoError(t, err)
		t.Cleanup(func() { _ = clientConn.Close() })

		<-done
		require.NoError(t, acceptErr)
		t.Cleanup(func() { _ = serverConn.Close() })

		// Apply MSS limit to the client connection
		res := applyMSSLimit(clientConn, 512)
		assert.NotNil(t, res)
		assert.Equal(t, clientConn, res)
	})
}

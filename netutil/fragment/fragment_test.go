// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fragment

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestNewFragmentedConn_NilConfig(t *testing.T) {
	t.Parallel()

	_, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	conn := NewFragmentedConn(client, nil)
	assert.Same(t, client, conn, "Nil config should return original connection directly")
}

func TestFragmentedConn_SmallWrite(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	frag := &Config{
		ChunkSize: 10,
		MaxDelay:  -1,
	}

	fragConn := NewFragmentedConn(client, frag)

	data := []byte("hello")
	go func() {
		buf := make([]byte, 1024)
		n, _ := server.Read(buf)

		if !bytes.Equal(buf[:n], data) {
			t.Errorf("got %q, want %q", buf[:n], data)
		}
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
}

func TestFragmentedConn_SmallWrite_WithDelay(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	frag := &Config{
		ChunkSize: 20,
		MaxDelay:  10 * time.Millisecond,
	}

	fragConn := NewFragmentedConn(client, frag)

	data := []byte("short")

	go func() {
		buf := make([]byte, 1024)
		_, _ = server.Read(buf)
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
}

func TestFragmentedConn_LargeWrite(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	frag := &Config{
		ChunkSize: 5,
		MaxDelay:  -1,
	}

	fragConn := NewFragmentedConn(client, frag)

	data := []byte("hello world test data")

	var received []byte

	done := make(chan struct{})
	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				received = append(received, buf[:n]...)
			}

			if err != nil || len(received) >= len(data) {
				break
			}
		}
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	select {
	case <-done:
		assert.Equal(t, data, received)
	case <-time.After(1 * time.Second):
		t.Fatal("read timeout")
	}
}

func TestFragmentedConn_LimitBytes(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	frag := &Config{
		ChunkSize:  2,
		LimitBytes: 5,
	}

	fragConn := NewFragmentedConn(client, frag)

	var received bytes.Buffer

	done := make(chan struct{})

	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				received.Write(buf[:n])
			}

			if err != nil {
				return
			}
		}
	}()

	// Write 1: 4 bytes (within LimitBytes = 5)
	payload1 := []byte("1234")
	n1, err := fragConn.Write(payload1)
	require.NoError(t, err)
	assert.Equal(t, 4, n1)

	// Write 2: 10 bytes (LimitBytes = 5 exceeded, should bypass chunking)
	payload2 := []byte("5678901234")
	n2, err := fragConn.Write(payload2)
	require.NoError(t, err)
	assert.Equal(t, 10, n2)

	_ = fragConn.Close()
	_ = server.Close()

	<-done
	assert.Equal(t, "12345678901234", received.String())
}

func TestFragmentedConn_DynamicChunkSizeAndJitter(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	cfg := &Config{
		MinChunkSize: 2,
		MaxChunkSize: 5,
		MinDelay:     1 * time.Millisecond,
		MaxDelay:     3 * time.Millisecond,
	}

	fragConn := NewFragmentedConn(client, cfg)

	data := []byte("dynamic chunking test payload")

	var received []byte

	done := make(chan struct{})
	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				received = append(received, buf[:n]...)
			}

			if err != nil || len(received) >= len(data) {
				break
			}
		}
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	select {
	case <-done:
		assert.Equal(t, data, received)
	case <-time.After(2 * time.Second):
		t.Fatal("read timeout")
	}
}

func TestFragmentedConn_Write_Error(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	frag := &Config{
		ChunkSize: 5,
		MaxDelay:  -1,
	}

	fragConn := NewFragmentedConn(client, frag)

	// Close server side so the next write inside loop fails
	_ = server.Close()

	data := []byte("hello world")
	_, err := fragConn.Write(data)
	assert.Error(t, err)

	_ = fragConn.Close()
}

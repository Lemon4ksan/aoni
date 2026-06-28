// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFragmentedConn_SmallWrite(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	frag := &FragmentConfig{
		ChunkSize: 10,
		MaxDelay:  -1,
	}

	fragConn := NewFragmentedConn(client, frag)

	data := []byte("hello")
	go func() {
		buf := make([]byte, 1024)
		n, _ := server.Read(buf)
		_ = server.Close()

		if !bytes.Equal(buf[:n], data) {
			t.Errorf("got %q, want %q", buf[:n], data)
		}
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	_ = fragConn.Close()
}

func TestFragmentedConn_SmallWrite_WithDelay(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	frag := &FragmentConfig{
		ChunkSize: 20,
		MaxDelay:  10 * time.Millisecond,
	}

	fragConn := NewFragmentedConn(client, frag)

	data := []byte("short")

	go func() {
		buf := make([]byte, 1024)
		_, _ = server.Read(buf)
		_ = server.Close()
	}()

	n, err := fragConn.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	_ = fragConn.Close()
}

func TestFragmentedConn_LargeWrite(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	frag := &FragmentConfig{
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

			if err != nil {
				break
			}
		}
	}()

	_, err := fragConn.Write(data)
	require.NoError(t, err)

	_ = fragConn.Close()

	<-done

	assert.Equal(t, data, received)
}

func TestFragmentedConn_Write_Error(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	frag := &FragmentConfig{
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

func TestWithFragmentation_ContextVerification(t *testing.T) {
	t.Parallel()

	cfg := FragmentConfig{
		ChunkSize: 42,
		MaxDelay:  15 * time.Millisecond,
	}

	mod := WithFragmentation(cfg)
	require.NotNil(t, mod)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	mod(req)

	val := req.Context().Value(fragmentCtxKey{})
	require.NotNil(t, val)

	extracted, ok := val.(FragmentConfig)
	require.True(t, ok)
	assert.Equal(t, 42, extracted.ChunkSize)
	assert.Equal(t, 15*time.Millisecond, extracted.MaxDelay)
}

func TestNewFragmentedConn(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	cfg := &FragmentConfig{
		ChunkSize: 10,
		MaxDelay:  5 * time.Millisecond,
	}

	fragConn := NewFragmentedConn(client, cfg)

	data := []byte("test data for fragmentation")

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

			if err != nil {
				break
			}
		}
	}()

	_, err := fragConn.Write(data)
	require.NoError(t, err)

	_ = fragConn.Close()
	_ = server.Close()

	<-done

	assert.Equal(t, data, received)
}

func TestWrapWithFragmentation(t *testing.T) {
	t.Parallel()

	_, client := net.Pipe()
	cfg := FragmentConfig{
		ChunkSize: 15,
		MaxDelay:  2 * time.Millisecond,
	}

	conn := wrapWithFragmentation(client, cfg)
	require.NotNil(t, conn)

	// Assert internal fields using type assertion since we are in the package aoni
	fc, ok := conn.(*fragmentedConn)
	require.True(t, ok)
	assert.Equal(t, 15, fc.chunkSize)
	assert.Equal(t, 2*time.Millisecond, fc.maxDelay)

	_ = conn.Close()
}

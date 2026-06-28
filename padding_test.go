// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/hex"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaddingHeaderName(t *testing.T) {
	t.Parallel()

	t.Run("default_when_empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "X-Padding", PaddingHeaderName(PaddingConfig{}))
	})

	t.Run("uses_custom_header", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{PaddingHeader: "X-Custom"}
		assert.Equal(t, "X-Custom", PaddingHeaderName(cfg))
	})

	t.Run("header_pool_overrides_custom", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			PaddingHeader: "X-ShouldBeIgnored",
			HeaderPool:    []string{"X-Amz-Trace-Id", "CF-RAY", "X-Request-ID"},
		}

		seen := make(map[string]bool)
		for range 100 {
			name := PaddingHeaderName(cfg)
			seen[name] = true
		}

		assert.Equal(t, 3, len(seen), "all pool entries should be selected")
	})

	t.Run("single_entry_pool", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{HeaderPool: []string{"CF-RAY"}}
		for range 50 {
			assert.Equal(t, "CF-RAY", PaddingHeaderName(cfg))
		}
	})
}

func TestGeneratePadding(t *testing.T) {
	t.Parallel()

	t.Run("returns_nil_when_disabled", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, GeneratePadding(PaddingConfig{}))
	})

	t.Run("returns_bytes_in_range", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{MinPaddingBytes: 10, MaxPaddingBytes: 20}
		for range 50 {
			padding := GeneratePadding(cfg)
			assert.GreaterOrEqual(t, len(padding), 10)
			assert.LessOrEqual(t, len(padding), 20)
		}
	})

	t.Run("min_eq_max", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{MinPaddingBytes: 8, MaxPaddingBytes: 8}
		padding := GeneratePadding(cfg)
		assert.Len(t, padding, 8)
	})

	t.Run("min_and_max_boundary_adjustments", func(t *testing.T) {
		t.Parallel()

		// Min is negative -> should default to 1
		cfg1 := PaddingConfig{MinPaddingBytes: -5, MaxPaddingBytes: 10}
		padding1 := GeneratePadding(cfg1)
		assert.GreaterOrEqual(t, len(padding1), 1)

		// Max is less than min -> should default max to min
		cfg2 := PaddingConfig{MinPaddingBytes: 5, MaxPaddingBytes: 2}
		padding2 := GeneratePadding(cfg2)
		assert.Len(t, padding2, 5)
	})
}

func TestRandIntn_Boundaries(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, randIntn(0))
	assert.Equal(t, 0, randIntn(-10))
}

func TestApplyRequestPadding(t *testing.T) {
	t.Parallel()

	cfg := PaddingConfig{
		MinPaddingBytes: 8,
		MaxPaddingBytes: 8,
		PaddingHeader:   "X-Padding-Test",
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://localhost", nil)
	require.NoError(t, err)

	mod := WithPadding(cfg)
	require.NotNil(t, mod)

	mod(req)

	headerVal := req.Header.Get("X-Padding-Test")
	require.NotEmpty(t, headerVal)

	decoded, err := hex.DecodeString(headerVal)
	require.NoError(t, err)
	assert.Len(t, decoded, 8)
}

func TestWrapWithMSSLimit(t *testing.T) {
	t.Parallel()

	t.Run("zero_or_negative_mss", func(t *testing.T) {
		t.Parallel()

		_, client := net.Pipe()
		res := wrapWithMSSLimit(client, 0)
		assert.Equal(t, client, res)
		_ = client.Close()
	})

	t.Run("non_tcp_conn", func(t *testing.T) {
		t.Parallel()

		_, client := net.Pipe()
		res := wrapWithMSSLimit(client, 512)
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
		res := wrapWithMSSLimit(clientConn, 512)
		assert.NotNil(t, res)
		assert.Equal(t, clientConn, res)
	})
}

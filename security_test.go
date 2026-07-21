// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	stdio "io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"zero v4", "0.0.0.0", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"unique-local v6", "fd00::1", true},
		{"public v4", "8.8.8.8", false},
		{"public v6", "2001:4860:4860::8888", false},
		{"cloudflare", "1.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "invalid IP: %s", tt.ip)
			assert.Equal(t, tt.blocked, IsBlockedIP(ip))
		})
	}
}

func TestIsBlockedIP_AdvancedIPv6AndObfuscation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1", true},
		{"IPv4-mapped IPv6 private 10.x", "::ffff:10.0.0.1", true},
		{"IPv4-mapped IPv6 public", "::ffff:8.8.8.8", false},
		{"Link-local unicast v6", "fe80::1", true},
		{"Multicast v6 node-local", "ff01::1", true},
		{"Unique local v6 fc00::/8 boundary", "fc00::1", true},
		{"Unique local v6 fd00::/8 boundary", "fd00::1", true},
		{"IPv4 loopback range 127.0.0.2", "127.0.0.2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.ip)
			assert.Equal(t, tt.blocked, IsBlockedIP(ip))
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	input := "Authorization: Bearer secret-token\r\nCookie: session=abc123\r\nContent-Type: text/plain\r\n"
	result := string(redactHeaders([]byte(input)))

	assert.Contains(t, result, "authorization: <redacted>")
	assert.Contains(t, result, "cookie: <redacted>")
	assert.Contains(t, result, "Content-Type: text/plain")
	assert.NotContains(t, result, "secret-token")
	assert.NotContains(t, result, "session=abc123")
}

func TestRetryOnTransientErrors(t *testing.T) {
	t.Parallel()

	cond := RetryOnTransientErrors()

	t.Run("nil_error_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cond(nil, nil))
	})

	t.Run("net_error_returns_true", func(t *testing.T) {
		t.Parallel()

		_, err := net.DialTimeout("tcp", "192.0.2.1:1", time.Nanosecond)
		if !assert.True(t, cond(nil, err)) {
			t.Logf("Actual error: %v (type: %T)", err, err)
		}
	})

	t.Run("connection_refused_string_returns_true", func(t *testing.T) {
		t.Parallel()
		assert.True(t, cond(nil, errors.New("dial tcp: connection refused")))
	})

	t.Run("io_eof_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cond(nil, stdio.ErrUnexpectedEOF))
	})
}

func TestLimitDecoder_BombPrevention(t *testing.T) {
	t.Parallel()

	type SimpleData struct {
		Field string `json:"field"`
	}

	payload := `{"field": "this data exceeds the safe read limit set on the decoder"}`
	reader := strings.NewReader(payload)

	limited := LimitDecoder(JSONDecoder, 15)

	var target SimpleData

	err := limited.Decode(reader, &target)
	require.Error(t, err, "expected decoder to hit the limits boundary and return error")
}

func TestGeneratePadding_SafetyLimits(t *testing.T) {
	t.Parallel()

	t.Run("negative padding ranges", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			MinPaddingBytes: -20,
			MaxPaddingBytes: -10,
		}
		res := GeneratePadding(cfg)
		assert.Nil(t, res)
	})

	t.Run("inverted padding ranges", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			MinPaddingBytes: 30,
			MaxPaddingBytes: 10,
		}
		res := GeneratePadding(cfg)
		require.NotNil(t, res)
		assert.Equal(t, 30, len(res), "expected range inversion to automatically align size to minimum boundary")
	})
}

func TestWrapWithMSSLimit_NegativeMSS(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })

	wrapped := connWrapper{}.WithMSSLimit(c1, -100)
	assert.NotNil(t, wrapped, "negative MSS size should be ignored gracefully without breaking the stream")
}

func TestFragmentedConn_Write(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })

	cfg := FragmentConfig{
		ChunkSize: 2,
	}

	fragmented := NewFragmentedConn(c1, &cfg)

	type writeResult struct {
		n   int
		err error
	}

	ch := make(chan writeResult, 1)

	go func() {
		n, err := fragmentedConnWrite(fragmented, []byte("test"))
		ch <- writeResult{n: n, err: err}
	}()

	buf := make([]byte, 2)
	n, err := stdio.ReadFull(c2, buf)
	require.NoError(t, err)
	assert.Equal(t, "te", string(buf[:n]))

	n, err = stdio.ReadFull(c2, buf)
	require.NoError(t, err)
	assert.Equal(t, "st", string(buf[:n]))

	res := <-ch
	require.NoError(t, res.err)
	assert.Equal(t, 4, res.n)
}

func fragmentedConnWrite(conn net.Conn, b []byte) (int, error) {
	return conn.Write(b)
}

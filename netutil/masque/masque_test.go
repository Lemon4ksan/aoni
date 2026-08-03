// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/mod"
)

type mockWSDialer struct {
	dialTLSFunc   func(ctx context.Context, addr string) (net.Conn, error)
	dialPlainFunc func(ctx context.Context, addr string) (net.Conn, error)
}

func (m *mockWSDialer) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if m.dialTLSFunc != nil {
		return m.dialTLSFunc(ctx, addr)
	}

	return nil, errors.New("mock dial tls not implemented")
}

func (m *mockWSDialer) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	if m.dialPlainFunc != nil {
		return m.dialPlainFunc(ctx, addr)
	}

	return nil, errors.New("mock dial plain not implemented")
}

func TestBuildIPProxyURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		port     int
		target   string
		ipproto  string
		expected string
	}{
		{
			name:     "IPv4 target and UDP protocol",
			host:     "proxy.example.com",
			port:     443,
			target:   "192.168.1.1",
			ipproto:  "17",
			expected: "https://proxy.example.com:443/.well-known/masque/ip/192.168.1.1/17/",
		},
		{
			name:     "IPv6 target with colons",
			host:     "proxy.example.com",
			port:     8443,
			target:   "2001:db8::1",
			ipproto:  "6",
			expected: "https://proxy.example.com:8443/.well-known/masque/ip/2001%3Adb8%3A%3A1/6/",
		},
		{
			name:     "CIDR subnet with slashes",
			host:     "proxy.example.com",
			port:     443,
			target:   "10.0.0.0/16",
			ipproto:  "17",
			expected: "https://proxy.example.com:443/.well-known/masque/ip/10.0.0.0%2F16/17/",
		},
		{
			name:     "Empty target and ipproto fallback to wildcard",
			host:     "proxy.example.com",
			port:     443,
			target:   "",
			ipproto:  "",
			expected: "https://proxy.example.com:443/.well-known/masque/ip/*/*/",
		},
		{
			name:     "Whitespace target and ipproto fallback to wildcard",
			host:     "proxy.example.com",
			port:     443,
			target:   "   ",
			ipproto:  "   ",
			expected: "https://proxy.example.com:443/.well-known/masque/ip/*/*/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildIPProxyURI(tt.host, tt.port, tt.target, tt.ipproto)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDialIPProxy(t *testing.T) {
	t.Parallel()

	t.Run("successful 101 upgrade handshake", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				assert.Equal(t, "proxy.example.com:443", addr)
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)

			req, err := http.ReadRequest(br)
			if err != nil {
				_ = serverConn.Close()
				return
			}

			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, "connect-ip", req.Header.Get("Upgrade"))
			assert.Equal(t, "Upgrade", req.Header.Get("Connection"))
			assert.Equal(t, "custom-agent", req.Header.Get("User-Agent"))

			resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: connect-ip\r\nConnection: Upgrade\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com/.well-known/masque/ip/*/*/"
		conn, resp, err := DialIPProxy(t.Context(), dialer, targetURL, mod.WithHeader("User-Agent", "custom-agent"))

		require.NoError(t, err)
		require.NotNil(t, conn)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

		t.Cleanup(func() {
			_ = conn.Close()
			_ = serverConn.Close()
		})
	})

	t.Run("successful 200 OK response", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)

			_, err := http.ReadRequest(br)
			if err != nil {
				_ = serverConn.Close()
				return
			}

			resp := "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com:8443/.well-known/masque/ip/*/*/"
		conn, resp, err := DialIPProxy(t.Context(), dialer, targetURL)

		require.NoError(t, err)
		require.NotNil(t, conn)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		t.Cleanup(func() {
			_ = conn.Close()
			_ = serverConn.Close()
		})
	})

	t.Run("handshake failed non-200 non-101 status code", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)

			_, err := http.ReadRequest(br)
			if err != nil {
				_ = serverConn.Close()
				return
			}

			resp := "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com/.well-known/masque/ip/*/*/"
		conn, resp, err := DialIPProxy(t.Context(), dialer, targetURL)

		assert.ErrorIs(t, err, ErrHandshakeFailed)
		assert.Nil(t, conn)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("handshake failed 101 missing valid upgrade headers", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)

			_, err := http.ReadRequest(br)
			if err != nil {
				_ = serverConn.Close()
				return
			}

			// Upgrade header is websocket instead of connect-ip
			resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com/.well-known/masque/ip/*/*/"
		conn, resp, err := DialIPProxy(t.Context(), dialer, targetURL)

		assert.ErrorIs(t, err, ErrHandshakeFailed)
		assert.Nil(t, conn)
		require.NotNil(t, resp)
	})

	t.Run("dialer error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("dial tls failed")
		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return nil, expectedErr
			},
		}

		targetURL := "https://proxy.example.com/.well-known/masque/ip/*/*/"
		conn, resp, err := DialIPProxy(t.Context(), dialer, targetURL)

		assert.ErrorIs(t, err, expectedErr)
		assert.Nil(t, conn)
		assert.Nil(t, resp)
	})

	t.Run("invalid URI template", func(t *testing.T) {
		t.Parallel()

		dialer := &mockWSDialer{}
		_, _, err := DialIPProxy(t.Context(), dialer, ":invalid-uri")

		assert.ErrorIs(t, err, ErrInvalidURITemplate)
	})

	t.Run("context with deadline", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)
			_, _ = http.ReadRequest(br)
			resp := "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		conn, resp, err := DialIPProxy(ctx, dialer, "https://proxy.example.com/.well-known/masque/ip/*/*/")
		require.NoError(t, err)
		assert.NotNil(t, conn)
		assert.NotNil(t, resp)

		_ = conn.Close()
		_ = serverConn.Close()
	})

	t.Run("write request error", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()
		_ = serverConn.Close() // Close server end immediately to trigger write error

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		_, _, err := DialIPProxy(t.Context(), dialer, "https://proxy.example.com/.well-known/masque/ip/*/*/")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aoni masque: write request:")
	})

	t.Run("read response error", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		go func() {
			br := bufio.NewReader(serverConn)
			_, _ = http.ReadRequest(br)
			_ = serverConn.Close() // Close without writing response
		}()

		_, _, err := DialIPProxy(t.Context(), dialer, "https://proxy.example.com/.well-known/masque/ip/*/*/")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aoni masque: read response:")
	})
}

func TestTokenContainsValue(t *testing.T) {
	t.Parallel()

	h := make(http.Header)
	h.Add("Connection", "keep-alive, Upgrade, foo")
	h.Add("Upgrade", "CONNECT-IP")

	assert.True(t, tokenContainsValue(h, "Connection", "upgrade"))
	assert.True(t, tokenContainsValue(h, "Upgrade", "connect-ip"))
	assert.False(t, tokenContainsValue(h, "Connection", "websocket"))
	assert.False(t, tokenContainsValue(h, "Non-Existent", "val"))
}

func TestErrorConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "aoni masque: connect-ip handshake failed", ErrHandshakeFailed.Error())
	assert.Equal(t, "aoni masque: invalid capsule format", ErrInvalidCapsule.Error())
	assert.Equal(t, "aoni masque: invalid uri template", ErrInvalidURITemplate.Error())
	assert.Equal(t, "aoni masque: unsupported http version for connect-ip", ErrUnsupportedHTTPVersion.Error())
	assert.Equal(t, "aoni masque: address request capsule cannot be empty", ErrEmptyAddressRequest.Error())
}

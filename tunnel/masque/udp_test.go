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

func TestBuildUDPProxyURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		host       string
		port       int
		targetHost string
		targetPort int
		expected   string
	}{
		{
			name:       "domain target and DNS port",
			host:       "proxy.example.com",
			port:       443,
			targetHost: "dns.google",
			targetPort: 53,
			expected:   "https://proxy.example.com:443/.well-known/masque/udp/dns.google/53/",
		},
		{
			name:       "IP target and custom port",
			host:       "127.0.0.1",
			port:       8443,
			targetHost: "1.1.1.1",
			targetPort: 853,
			expected:   "https://127.0.0.1:8443/.well-known/masque/udp/1.1.1.1/853/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildUDPProxyURI(tt.host, tt.port, tt.targetHost, tt.targetPort)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDialUDPProxy(t *testing.T) {
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
			assert.Equal(t, ConnectUDPUpgradeToken, req.Header.Get("Upgrade"))
			assert.Equal(t, "Upgrade", req.Header.Get("Connection"))
			assert.Equal(t, "udp-agent", req.Header.Get("User-Agent"))

			resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: connect-udp\r\nConnection: Upgrade\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com/.well-known/masque/udp/dns.google/53/"
		conn, resp, err := DialUDPProxy(t.Context(), dialer, targetURL, mod.WithHeader("User-Agent", "udp-agent"))

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

		targetURL := "https://proxy.example.com:8443/.well-known/masque/udp/1.1.1.1/53/"
		conn, resp, err := DialUDPProxy(t.Context(), dialer, targetURL)

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

			resp := "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"
			_, _ = serverConn.Write([]byte(resp))
		}()

		targetURL := "https://proxy.example.com/.well-known/masque/udp/dns.google/53/"
		conn, resp, err := DialUDPProxy(t.Context(), dialer, targetURL)

		assert.ErrorIs(t, err, ErrHandshakeFailed)
		assert.Nil(t, conn)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	})

	t.Run("dialer error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("udp dial tls failed")
		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return nil, expectedErr
			},
		}

		targetURL := "https://proxy.example.com/.well-known/masque/udp/dns.google/53/"
		conn, resp, err := DialUDPProxy(t.Context(), dialer, targetURL)

		assert.ErrorIs(t, err, expectedErr)
		assert.Nil(t, conn)
		assert.Nil(t, resp)
	})

	t.Run("invalid URI template", func(t *testing.T) {
		t.Parallel()

		dialer := &mockWSDialer{}
		_, _, err := DialUDPProxy(t.Context(), dialer, ":invalid-udp-uri")

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

		conn, resp, err := DialUDPProxy(ctx, dialer, "https://proxy.example.com/.well-known/masque/udp/dns.google/53/")
		require.NoError(t, err)
		assert.NotNil(t, conn)
		assert.NotNil(t, resp)

		_ = conn.Close()
		_ = serverConn.Close()
	})

	t.Run("write request error", func(t *testing.T) {
		t.Parallel()

		clientConn, serverConn := net.Pipe()
		_ = serverConn.Close()

		dialer := &mockWSDialer{
			dialTLSFunc: func(ctx context.Context, addr string) (net.Conn, error) {
				return clientConn, nil
			},
		}

		_, _, err := DialUDPProxy(
			t.Context(),
			dialer,
			"https://proxy.example.com/.well-known/masque/udp/dns.google/53/",
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aoni/masque: write udp request:")
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
			_ = serverConn.Close()
		}()

		_, _, err := DialUDPProxy(
			t.Context(),
			dialer,
			"https://proxy.example.com/.well-known/masque/udp/dns.google/53/",
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aoni/masque: read udp response:")
	})
}

func BenchmarkCONNECTUDPHandshake(b *testing.B) {
	req, _ := http.NewRequest(http.MethodGet, "https://proxy.example.com/.well-known/masque/udp/dns.google/53/", nil)
	req.Header.Set("Host", "proxy.example.com")
	req.Header.Set("Upgrade", ConnectUDPUpgradeToken)
	req.Header.Set("Connection", "Upgrade")

	respData := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: connect-udp\r\nConnection: Upgrade\r\n\r\n")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		clientConn, serverConn := net.Pipe()

		done := make(chan struct{})
		go func() {
			defer close(done)

			var buf [512]byte

			n, err := serverConn.Read(buf[:])
			if err != nil || n == 0 {
				return
			}

			_, _ = serverConn.Write(respData)
		}()

		// Pass clientConn directly to performCONNECTUDPHandshake avoiding real network dials
		resp, err := performCONNECTUDPHandshake(b.Context(), clientConn, req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}

		_ = clientConn.Close()
		_ = serverConn.Close()

		<-done
	}
}

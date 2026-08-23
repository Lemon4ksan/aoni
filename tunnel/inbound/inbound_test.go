// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golangproxy "golang.org/x/net/proxy"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tunnel/inbound"
)

func TestSniffProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		payload  []byte
		expected inbound.ProtocolType
	}{
		{
			name:     "socks5_header",
			payload:  []byte{0x05, 0x01, 0x00},
			expected: inbound.ProtocolSOCKS5,
		},
		{
			name:     "socks4_header",
			payload:  []byte{0x04, 0x01, 0x00, 0x50},
			expected: inbound.ProtocolSOCKS4,
		},
		{
			name:     "http_get_header",
			payload:  []byte("GET / HTTP/1.1\r\n"),
			expected: inbound.ProtocolHTTP,
		},
		{
			name:     "http_connect_header",
			payload:  []byte("CONNECT example.com:443 HTTP/1.1\r\n"),
			expected: inbound.ProtocolHTTP,
		},
		{
			name:     "unknown_protocol_bytes",
			payload:  []byte{0xFF, 0xFE, 0xFD},
			expected: inbound.ProtocolUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			br := bufio.NewReader(bytes.NewReader(tt.payload))
			proto, err := inbound.SniffProtocol(br)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, proto)
		})
	}
}

func TestInboundServer_SOCKS5_NoAuth(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("socks5_target_ok"))
	}))
	t.Cleanup(targetServer.Close)

	// Start inbound proxy server
	srv, err := inbound.NewServer("127.0.0.1:0")
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	// Connect using SOCKS5 proxy
	proxyURL, err := url.Parse("socks5://" + ln.Addr().String())
	require.NoError(t, err)

	dialer, err := golangproxy.FromURL(proxyURL, golangproxy.Direct)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if cd, ok := dialer.(golangproxy.ContextDialer); ok {
					return cd.DialContext(ctx, network, addr)
				}

				return dialer.Dial(network, addr)
			},
		},
	}

	resp, err := client.Get(targetServer.URL + "/test")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "socks5_target_ok", string(body))
}

func TestInboundServer_SOCKS5_WithAuth(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("socks5_auth_ok"))
	}))
	t.Cleanup(targetServer.Close)

	srv, err := inbound.NewServer("127.0.0.1:0",
		inbound.WithAuthenticator(func(username, password string) bool {
			return username == "proxyuser" && password == "proxypass"
		}),
	)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	t.Run("valid_credentials_succeeds", func(t *testing.T) {
		t.Parallel()

		proxyURL, _ := url.Parse("socks5://proxyuser:proxypass@" + ln.Addr().String())
		dialer, err := golangproxy.FromURL(proxyURL, golangproxy.Direct)
		require.NoError(t, err)

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					if cd, ok := dialer.(golangproxy.ContextDialer); ok {
						return cd.DialContext(ctx, network, addr)
					}

					return dialer.Dial(network, addr)
				},
			},
		}

		resp, err := client.Get(targetServer.URL + "/test")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("invalid_credentials_fails", func(t *testing.T) {
		t.Parallel()

		proxyURL, _ := url.Parse("socks5://wronguser:wrongpass@" + ln.Addr().String())
		dialer, err := golangproxy.FromURL(proxyURL, golangproxy.Direct)
		require.NoError(t, err)

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					if cd, ok := dialer.(golangproxy.ContextDialer); ok {
						return cd.DialContext(ctx, network, addr)
					}

					return dialer.Dial(network, addr)
				},
			},
		}

		_, err = client.Get(targetServer.URL + "/test")
		require.Error(t, err)
	})
}

func TestInboundServer_HTTPProxy_Plain(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain_http_proxy_ok"))
	}))
	t.Cleanup(targetServer.Close)

	srv, err := inbound.NewServer("127.0.0.1:0")
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	resp, err := client.Get(targetServer.URL + "/plain")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "plain_http_proxy_ok", string(body))
}

func TestInboundServer_HTTPProxy_Connect(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("https_connect_ok"))
	}))
	t.Cleanup(targetServer.Close)

	srv, err := inbound.NewServer("127.0.0.1:0")
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}

	resp, err := client.Get(targetServer.URL + "/secure")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "https_connect_ok", string(body))
}

func TestInboundServer_HTTPProxy_Auth(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("http_auth_ok"))
	}))
	t.Cleanup(targetServer.Close)

	srv, err := inbound.NewServer("127.0.0.1:0",
		inbound.WithAuthenticator(func(username, password string) bool {
			return username == "httpuser" && password == "httppass"
		}),
	)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	t.Run("unauthorized_request_returns_407", func(t *testing.T) {
		t.Parallel()

		proxyURL, _ := url.Parse("http://" + ln.Addr().String())
		client := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}

		resp, err := client.Get(targetServer.URL + "/auth")
		if err == nil {
			defer resp.Body.Close()

			assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
		} else {
			assert.Error(t, err)
		}
	})

	t.Run("authorized_request_succeeds", func(t *testing.T) {
		t.Parallel()

		proxyURL, _ := url.Parse("http://httpuser:httppass@" + ln.Addr().String())
		client := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, targetServer.URL+"/auth", nil)
		require.NoError(t, err)

		authVal := base64.StdEncoding.EncodeToString([]byte("httpuser:httppass"))
		req.Header.Set("Proxy-Authorization", "Basic "+authVal)

		resp, err := client.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestInboundServer_TLS_MITM(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mitm_intercepted_ok"))
	}))
	t.Cleanup(targetServer.Close)

	outboundEngine := aoni.NewClient(targetServer.Client(), option.WithInsecureSkipVerify())

	srv, err := inbound.NewServer("127.0.0.1:0",
		inbound.WithMITM(true),
		inbound.WithEngine(outboundEngine),
	)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	t.Cleanup(func() { _ = srv.Close() })

	proxyURL, err := url.Parse("http://" + ln.Addr().String())
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}

	resp, err := client.Get(targetServer.URL + "/mitm")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "mitm_intercepted_ok", string(body))

	// Second request to verify cached certificate reuse
	resp2, err := client.Get(targetServer.URL + "/mitm-cached")
	require.NoError(t, err)

	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, "mitm_intercepted_ok", string(body2))
}

func TestInboundServer_ShutdownAndClose(t *testing.T) {
	t.Parallel()

	srv, err := inbound.NewServer("127.0.0.1:0")
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(context.Background(), ln)
	}()

	// Shutdown server
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	err = srv.Shutdown(ctx)
	require.NoError(t, err)

	// Subsequent operations return ErrServerClosed
	errServe := srv.Serve(ctx, ln)
	assert.ErrorIs(t, errServe, inbound.ErrServerClosed)
}

func TestInboundServer_Options(t *testing.T) {
	t.Parallel()

	var authCalled bool

	srv, err := inbound.NewServer("127.0.0.1:1080",
		inbound.WithListenAddr("127.0.0.1:8080"),
		inbound.WithMITM(true),
		inbound.WithAuthenticator(func(_, _ string) bool {
			authCalled = true
			return true
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8080", srv.Addr)
	assert.True(t, srv.EnableMITM)
	assert.NotNil(t, srv.Auth)

	srv.Auth("u", "p")
	assert.True(t, authCalled)
}

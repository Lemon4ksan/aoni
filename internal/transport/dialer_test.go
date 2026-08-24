// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/transport"
)

func TestUniversalDialer_DialContext(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	dialer := transport.NewUniversalDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := transport.DialConfig{
		HostRewriteRules: map[string]string{
			"custom.local": l.Addr().String(),
		},
	}

	conn, err := dialer.DialContext(ctx, "tcp", "custom.local:80", cfg)
	require.NoError(t, err)
	assert.NotNil(t, conn)

	_ = conn.Close()
}

func TestUniversalDialer_DialTLSContext(t *testing.T) {
	t.Parallel()

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	dialer := transport.NewUniversalDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := transport.DialConfig{
		InsecureSkipVerify: true,
	}

	conn, err := dialer.DialTLSContext(ctx, "tcp", u.Host, cfg)
	require.NoError(t, err)
	assert.NotNil(t, conn)

	_ = conn.Close()
}

func TestUniversalDialer_DialH2_FallbackError(t *testing.T) {
	t.Parallel()

	// Standard TLS server without H2 ALPN
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	dialer := transport.NewUniversalDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := transport.DialConfig{
		InsecureSkipVerify: true,
		BaseTLSConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}

	_, err = dialer.DialH2(ctx, u.Host, cfg)
	assert.ErrorIs(t, err, transport.ErrServerH2NotSupported)
}

func TestUniversalDialer_TCPDelayContextCancelled(t *testing.T) {
	t.Parallel()

	dialer := transport.NewUniversalDialer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := transport.DialConfig{
		TCPDelay: 5 * time.Second,
	}

	_, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:80", cfg)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHeaderOrderingConn(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer l.Close()

	receivedCh := make(chan []byte, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)

		n, _ := conn.Read(buf)
		receivedCh <- buf[:n]
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)

	orderedConn := &transport.HeaderOrderingConn{
		Conn:        clientConn,
		OrderedKeys: []string{"Host", "User-Agent", "Accept"},
	}

	rawHTTP := []byte("GET / HTTP/1.1\r\nAccept: */*\r\nUser-Agent: test\r\nHost: example.com\r\n\r\n")
	_, err = orderedConn.Write(rawHTTP)
	require.NoError(t, err)

	select {
	case received := <-receivedCh:
		wireStr := string(received)
		assert.Contains(t, wireStr, "Host: example.com")
		assert.Contains(t, wireStr, "User-Agent: test")
		assert.Contains(t, wireStr, "Accept: */*")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ordered wire bytes")
	}

	_ = orderedConn.Close()
}

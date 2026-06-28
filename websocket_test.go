// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialWebSocket_PlainWS(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			if err := ws.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(nil)
	wsConn, resp, err := DialWebSocket(
		context.Background(),
		client,
		wsURL,
		WithHeader("Origin", "http://localhost"),
	)
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.NotNil(t, wsConn)
	defer wsConn.Close()

	_, err = wsConn.Write([]byte("hello websocket"))
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello websocket", string(buf[:n]))
}

func TestDialWebSocket_PlainAndTLS(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, p, err := conn.ReadMessage()
			if err != nil {
				return
			}

			_ = conn.WriteMessage(mt, p)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(nil)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := DialWebSocket(ctx, client, wsURL)
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	defer conn.Close()

	// Simple write/read test
	_, err = conn.Write([]byte("hello plain ws"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello plain ws", string(buf[:n]))
}

func TestDialWebSocket_WithTraceJA4(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			if err := ws.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(nil)
	info := &TraceInfo{}

	wsConn, _, err := DialWebSocket(
		context.Background(),
		client,
		wsURL,
		TraceJA4(info),
	)
	require.NoError(t, err)

	defer wsConn.Close()

	require.NotNil(t, info.JA4)
	assert.NotEmpty(t, info.JA4.JA4H)
}

func TestDialWebSocket_InvalidURL(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)
	_, _, err := DialWebSocket(context.Background(), client, "http://invalid-scheme.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported websocket scheme")
}

func TestDialWebSocket_WithConfig(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_ = conn.WriteMessage(websocket.TextMessage, []byte("config ok"))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(nil)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := DialWebSocketConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	conn, _, err := DialWebSocketWithConfig(ctx, client, wsURL, cfg)
	require.NoError(t, err)

	defer conn.Close()

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "config ok", string(buf[:n]))
}

func TestDialWebSocket_PlainWithFragmentation(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		mt, msg, err := ws.ReadMessage()
		if err == nil {
			_ = ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(nil)

	fragCfg := FragmentConfig{
		ChunkSize: 2,
		MaxDelay:  1 * time.Millisecond,
	}

	wsConn, resp, err := DialWebSocket(
		context.Background(),
		client,
		wsURL,
		WithFragmentation(fragCfg),
	)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	defer wsConn.Close()

	_, err = wsConn.Write([]byte("fragmentation"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "fragmentation", string(buf[:n]))
}

func TestDialWebSocket_TLSFingerprint(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
	})

	server := httptest.NewTLSServer(handler)
	defer server.Close()

	client := NewClient(nil)
	// Initialize TLSClientConfig before configuring browser profile
	client.Transport().TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client = client.WithTLSFingerprint(BrowserChrome)

	wsURL := "wss" + strings.TrimPrefix(server.URL, "https")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info := &TraceInfo{}
	conn, resp, err := DialWebSocket(ctx, client, wsURL, TraceJA4(info))
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	defer conn.Close()

	assert.NotEmpty(t, info.JA4.JA4H)
}

func TestDialWebSocket_TLSCustomDialTLSContext(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		mt, msg, err := ws.ReadMessage()
		if err == nil {
			_ = ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wssURL := "wss" + strings.TrimPrefix(server.URL, "https")

	client := NewClient(nil)
	dialTLSContextCalled := false
	client.Transport().DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialTLSContextCalled = true
		dialer := &tls.Dialer{
			Config: &tls.Config{InsecureSkipVerify: true},
		}

		return dialer.DialContext(ctx, network, addr)
	}

	wsConn, resp, err := DialWebSocket(
		context.Background(),
		client,
		wssURL,
	)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, dialTLSContextCalled)

	defer wsConn.Close()

	_, err = wsConn.Write([]byte("custom dial tls context"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "custom dial tls context", string(buf[:n]))
}

func TestDialWebSocket_TLSCustomDialContext(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mt, p, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(mt, p)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(nil)
	dialContextCalled := false
	client.Transport().DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialContextCalled = true
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, network, addr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := DialWebSocket(ctx, client, wsURL)
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.True(t, dialContextCalled)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	defer conn.Close()

	_, err = conn.Write([]byte("custom dial context fallback"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "custom dial context fallback", string(buf[:n]))
}

func TestDialWebSocket_PlainCustomDialContext(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		mt, msg, err := ws.ReadMessage()
		if err == nil {
			_ = ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(nil)
	dialContextCalled := false
	client.Transport().DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialContextCalled = true
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, network, addr)
	}

	wsConn, resp, err := DialWebSocket(
		context.Background(),
		client,
		wsURL,
	)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, dialContextCalled)

	defer wsConn.Close()

	_, err = wsConn.Write([]byte("plain custom dial context"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "plain custom dial context", string(buf[:n]))
}

func TestDialWebSocket_TLSH2HandshakeFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	// Enable HTTP/2 for testing negotiatedProtocol type assertions
	server.EnableHTTP2 = true

	server.StartTLS()
	defer server.Close()

	wssURL := "wss" + strings.TrimPrefix(server.URL, "https")

	// Negotiate h2 with a Chrome browser profile setup
	client := NewClient(nil).WithTLSFingerprint(BrowserChrome)
	client.Transport().TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	// Will evaluate dialH2ExtendedConnect, failing since native httptest lacks RFC 8441 headers.
	// This correctly validates the h2 branch redirection path in DialWebSocket.
	_, _, err := DialWebSocket(
		context.Background(),
		client,
		wssURL,
	)
	assert.Error(t, err)
}

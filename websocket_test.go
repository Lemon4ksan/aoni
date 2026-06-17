// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialWebSocket_PlainWS(t *testing.T) {
	t.Parallel()

	// Create a WebSocket echo server using gorilla
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

	// Convert http:// to ws://
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

	// Send and receive a message
	_, err = wsConn.Write([]byte("hello websocket"))
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello websocket", string(buf[:n]))
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

	// JA4H should be computed from the request headers
	require.NotNil(t, info.JA4)
	assert.NotEmpty(t, info.JA4.JA4H)
}

func TestWSConn_NetConnInterface(t *testing.T) {
	t.Parallel()
	// This is tested in wsconn_test.go
}

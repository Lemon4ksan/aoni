// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSIOServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	onConnect func(conn *websocket.Conn)
}

func newMockSIOServer(t *testing.T, onConnect func(conn *websocket.Conn)) *mockSIOServer {
	t.Helper()

	m := &mockSIOServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		onConnect: onConnect,
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		if m.onConnect != nil {
			m.onConnect(conn)
		}
	}))
	t.Cleanup(m.server.Close)

	return m
}

func TestEncodeSIOPacket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pkt      sioPacket
		expected string
	}{
		{
			name:     "connect to default namespace",
			pkt:      sioPacket{Type: sioConnect, Namespace: "/"},
			expected: "0",
		},
		{
			name:     "connect with auth",
			pkt:      sioPacket{Type: sioConnect, Namespace: "/", Data: json.RawMessage(`{"token":"abc"}`)},
			expected: `0{"token":"abc"}`,
		},
		{
			name:     "connect to custom namespace",
			pkt:      sioPacket{Type: sioConnect, Namespace: "/admin", Data: json.RawMessage(`{"token":"abc"}`)},
			expected: `0/admin,{"token":"abc"}`,
		},
		{
			name:     "disconnect from default namespace",
			pkt:      sioPacket{Type: sioDisconnect, Namespace: "/"},
			expected: "1",
		},
		{
			name:     "disconnect from custom namespace",
			pkt:      sioPacket{Type: sioDisconnect, Namespace: "/admin"},
			expected: "1/admin,",
		},
		{
			name:     "event on default namespace",
			pkt:      sioPacket{Type: sioEvent, Namespace: "/", Data: json.RawMessage(`["hello","world"]`)},
			expected: `2["hello","world"]`,
		},
		{
			name:     "event on custom namespace",
			pkt:      sioPacket{Type: sioEvent, Namespace: "/chat", Data: json.RawMessage(`["message","hi"]`)},
			expected: `2/chat,["message","hi"]`,
		},
		{
			name:     "ack with id",
			pkt:      sioPacket{Type: sioAck, Namespace: "/", ID: int64Ptr(42), Data: json.RawMessage(`["response"]`)},
			expected: `342["response"]`,
		},
		{
			name: "connect error",
			pkt: sioPacket{
				Type:      sioConnectError,
				Namespace: "/",
				Data:      json.RawMessage(`{"message":"unauthorized"}`),
			},
			expected: `4{"message":"unauthorized"}`,
		},
		{
			name: "binary event with 2 attachments",
			pkt: sioPacket{
				Type:        sioBinaryEvent,
				Namespace:   "/",
				Attachments: 2,
				Data:        json.RawMessage(`["upload",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`),
			},
			expected: `52-["upload",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`,
		},
		{
			name: "binary ack with id",
			pkt: sioPacket{
				Type:        sioBinaryAck,
				Namespace:   "/",
				Attachments: 1,
				ID:          int64Ptr(7),
				Data:        json.RawMessage(`[{"_placeholder":true,"num":0}]`),
			},
			expected: `61-7[{"_placeholder":true,"num":0}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := encodeSIOPacket(tt.pkt)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestDecodeSIOPacket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected sioPacket
	}{
		{
			name:  "connect to default namespace",
			input: "0",
			expected: sioPacket{
				Type:      sioConnect,
				Namespace: "/",
			},
		},
		{
			name:  "connect with auth",
			input: `0{"token":"abc"}`,
			expected: sioPacket{
				Type:      sioConnect,
				Namespace: "/",
				Data:      json.RawMessage(`{"token":"abc"}`),
			},
		},
		{
			name:  "connect to custom namespace",
			input: `0/admin,{"token":"abc"}`,
			expected: sioPacket{
				Type:      sioConnect,
				Namespace: "/admin",
				Data:      json.RawMessage(`{"token":"abc"}`),
			},
		},
		{
			name:  "disconnect from default namespace",
			input: "1",
			expected: sioPacket{
				Type:      sioDisconnect,
				Namespace: "/",
			},
		},
		{
			name:  "disconnect from custom namespace",
			input: "1/admin,",
			expected: sioPacket{
				Type:      sioDisconnect,
				Namespace: "/admin",
			},
		},
		{
			name:  "event on default namespace",
			input: `2["hello","world"]`,
			expected: sioPacket{
				Type:      sioEvent,
				Namespace: "/",
				Data:      json.RawMessage(`["hello","world"]`),
			},
		},
		{
			name:  "event on custom namespace",
			input: `2/chat,["message","hi"]`,
			expected: sioPacket{
				Type:      sioEvent,
				Namespace: "/chat",
				Data:      json.RawMessage(`["message","hi"]`),
			},
		},
		{
			name:  "ack with id",
			input: `342["response"]`,
			expected: sioPacket{
				Type:      sioAck,
				Namespace: "/",
				ID:        int64Ptr(42),
				Data:      json.RawMessage(`["response"]`),
			},
		},
		{
			name:  "binary event with 2 attachments",
			input: `52-["upload",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`,
			expected: sioPacket{
				Type:        sioBinaryEvent,
				Namespace:   "/",
				Attachments: 2,
				Data:        json.RawMessage(`["upload",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`),
			},
		},
		{
			name:  "binary ack with id",
			input: `61-7[{"_placeholder":true,"num":0}]`,
			expected: sioPacket{
				Type:        sioBinaryAck,
				Namespace:   "/",
				Attachments: 1,
				ID:          int64Ptr(7),
				Data:        json.RawMessage(`[{"_placeholder":true,"num":0}]`),
			},
		},
		{
			name:  "connect error",
			input: `4{"message":"unauthorized"}`,
			expected: sioPacket{
				Type:      sioConnectError,
				Namespace: "/",
				Data:      json.RawMessage(`{"message":"unauthorized"}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkt, err := decodeSIOPacket([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, pkt.Type)
			assert.Equal(t, tt.expected.Namespace, pkt.Namespace)
			assert.Equal(t, tt.expected.Attachments, pkt.Attachments)
			assert.Equal(t, tt.expected.ID, pkt.ID)
			assert.Equal(t, tt.expected.Data, pkt.Data)
		})
	}
}

func TestDecodeSIOPacketEmpty(t *testing.T) {
	t.Parallel()

	_, err := decodeSIOPacket([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkt  sioPacket
	}{
		{
			name: "connect with auth",
			pkt:  sioPacket{Type: sioConnect, Namespace: "/admin", Data: json.RawMessage(`{"token":"abc"}`)},
		},
		{
			name: "event on default namespace",
			pkt:  sioPacket{Type: sioEvent, Namespace: "/", Data: json.RawMessage(`["hello","world"]`)},
		},
		{
			name: "ack with id",
			pkt:  sioPacket{Type: sioAck, Namespace: "/", ID: int64Ptr(42), Data: json.RawMessage(`["response"]`)},
		},
		{
			name: "binary event",
			pkt: sioPacket{
				Type:        sioBinaryEvent,
				Namespace:   "/chat",
				Attachments: 1,
				Data:        json.RawMessage(`["upload",{"_placeholder":true,"num":0}]`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded := encodeSIOPacket(tt.pkt)
			decoded, err := decodeSIOPacket(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.pkt.Type, decoded.Type)
			assert.Equal(t, tt.pkt.Namespace, decoded.Namespace)
			assert.Equal(t, tt.pkt.Attachments, decoded.Attachments)
			assert.Equal(t, tt.pkt.ID, decoded.ID)
			assert.Equal(t, tt.pkt.Data, decoded.Data)
		})
	}
}

func TestHasBinary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     any
		expected bool
	}{
		{
			name:     "no binary",
			data:     []any{"hello", "world"},
			expected: false,
		},
		{
			name:     "with binary",
			data:     []any{"hello", []byte{1, 2, 3}},
			expected: true,
		},
		{
			name:     "nested binary",
			data:     []any{"hello", map[string]any{"data": []byte{1, 2}}},
			expected: true,
		},
		{
			name:     "empty slice",
			data:     []any{},
			expected: false,
		},
		{
			name:     "nil",
			data:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, hasBinary(tt.data))
		})
	}
}

func TestDeconstructReconstructBinary(t *testing.T) {
	t.Parallel()

	original := []any{
		"hello",
		[]byte{1, 2, 3},
		map[string]any{"nested": []byte{4, 5}},
		[]byte{6},
	}

	deconstructed, buffers := deconstructBinary(original)

	// Verify placeholders exist.
	deconstructedJSON, err := json.Marshal(deconstructed)
	require.NoError(t, err)
	assert.Contains(t, string(deconstructedJSON), "_placeholder")
	assert.Contains(t, string(deconstructedJSON), "num")
	assert.Equal(t, 3, len(buffers))

	// Reconstruct.
	reconstructed := reconstructBinary(deconstructed, buffers)

	// Verify binary data is restored.
	reconstructedSlice, ok := reconstructed.([]any)
	require.True(t, ok)
	assert.Equal(t, "hello", reconstructedSlice[0])
	assert.Equal(t, []byte{1, 2, 3}, reconstructedSlice[1])

	nestedMap, ok := reconstructedSlice[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []byte{4, 5}, nestedMap["nested"])

	assert.Equal(t, []byte{6}, reconstructedSlice[3])
}

func TestDeconstructBinaryNoBinary(t *testing.T) {
	t.Parallel()

	original := []any{"hello", "world"}
	deconstructed, buffers := deconstructBinary(original)

	assert.Equal(t, 0, len(buffers))
	assert.Equal(t, original, deconstructed)
}

func TestBackoffDuration(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    100 * time.Millisecond,
		ReconnectionDelayMax: 5 * time.Second,
		JitterFactor:         0, // no jitter for deterministic test
	}

	b := newBackoff(cfg)

	// Attempt 0: 100ms * 2^0 = 100ms
	d := b.Next()
	assert.Equal(t, 100*time.Millisecond, d)

	// Attempt 1: 100ms * 2^1 = 200ms
	d = b.Next()
	assert.Equal(t, 200*time.Millisecond, d)

	// Attempt 2: 100ms * 2^2 = 400ms
	d = b.Next()
	assert.Equal(t, 400*time.Millisecond, d)

	// Attempt 3: 100ms * 2^3 = 800ms
	d = b.Next()
	assert.Equal(t, 800*time.Millisecond, d)
}

func TestBackoffMaxCap(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    1 * time.Second,
		ReconnectionDelayMax: 5 * time.Second,
		JitterFactor:         0,
	}

	b := newBackoff(cfg)

	// Skip to attempt where delay exceeds max.
	for range 5 {
		b.Next()
	}

	// Attempt 5: 1s * 2^5 = 32s, but capped at 5s.
	d := b.Next()
	assert.Equal(t, 5*time.Second, d)
}

func TestBackoffReset(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    100 * time.Millisecond,
		ReconnectionDelayMax: 5 * time.Second,
		JitterFactor:         0,
	}

	b := newBackoff(cfg)

	b.Next()
	b.Next()
	b.Next()

	b.Reset()

	d := b.Next()
	assert.Equal(t, 100*time.Millisecond, d)
}

func TestBackoffJitterBounds(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    1 * time.Second,
		ReconnectionDelayMax: 30 * time.Second,
		JitterFactor:         0.5,
	}

	// Run many times with fresh backoff to verify jitter stays within bounds.
	for range 100 {
		b := newBackoff(cfg)
		d := b.Next()
		// With jitter 0.5, deviation can be up to 50% of base (1s).
		// So range should be roughly 500ms-1500ms.
		assert.True(t, d >= 0, "duration must be non-negative: %v", d)
		assert.True(t, d <= 2*time.Second, "duration must not exceed 2x base: %v", d)
	}
}

func TestSocketIOConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{}
	cfg.resolveDefaults()

	assert.Equal(t, 1*time.Second, cfg.ReconnectionDelay)
	assert.Equal(t, 30*time.Second, cfg.ReconnectionDelayMax)
	assert.Equal(t, 0.5, cfg.JitterFactor)
	assert.Equal(t, 20*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 20*time.Second, cfg.PingTimeout)
	assert.Equal(t, "/", cfg.Namespace)
}

func TestSocketIOConfigCustom(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    2 * time.Second,
		ReconnectionDelayMax: 60 * time.Second,
		JitterFactor:         0.3,
		ConnectTimeout:       30 * time.Second,
		PingTimeout:          10 * time.Second,
		Namespace:            "/admin",
	}

	cfg.resolveDefaults()

	assert.Equal(t, 2*time.Second, cfg.ReconnectionDelay)
	assert.Equal(t, 60*time.Second, cfg.ReconnectionDelayMax)
	assert.Equal(t, 0.3, cfg.JitterFactor)
	assert.Equal(t, 30*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 10*time.Second, cfg.PingTimeout)
	assert.Equal(t, "/admin", cfg.Namespace)
}

func TestBinaryReconstructor(t *testing.T) {
	t.Parallel()

	pkt := &sioPacket{
		Type: sioBinaryEvent,
		Data: json.RawMessage(`["upload",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`),
	}

	br := newBinaryReconstructor(2, pkt)

	// Add first buffer.
	assert.False(t, br.addBuffer([]byte{1, 2, 3}))

	// Add second buffer.
	assert.True(t, br.addBuffer([]byte{4, 5, 6}))

	// Reconstruct.
	result, err := br.reconstruct()
	require.NoError(t, err)
	assert.Equal(t, sioEvent, result.Type)
}

func TestBinaryReconstructorWrongCount(t *testing.T) {
	t.Parallel()

	pkt := &sioPacket{
		Type: sioBinaryEvent,
		Data: json.RawMessage(`["upload",{"_placeholder":true,"num":0}]`),
	}

	br := newBinaryReconstructor(2, pkt)
	br.addBuffer([]byte{1, 2, 3})

	// Try to reconstruct with wrong count.
	_, err := br.reconstruct()
	assert.Error(t, err)
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestDecodeSIOPacketEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "empty",
			input:       "",
			expectError: true,
		},
		{
			name:        "type only",
			input:       "0",
			expectError: false,
		},
		{
			name:        "large ack id",
			input:       "3999999999[\"ok\"]",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeSIOPacket([]byte(tt.input))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEncodeSIOPacketBinaryAttachmentCount(t *testing.T) {
	t.Parallel()

	pkt := sioPacket{
		Type:        sioBinaryEvent,
		Namespace:   "/",
		Attachments: 10,
		Data:        json.RawMessage(`["data"]`),
	}

	result := encodeSIOPacket(pkt)
	assert.Equal(t, "510-[\"data\"]", string(result))
}

func TestHasBinaryRaw(t *testing.T) {
	t.Parallel()

	// Binary data.
	raw := json.RawMessage(`[{"_placeholder":true,"num":0}]`)
	assert.False(t, hasBinaryRaw(raw))

	// Non-binary data.
	raw2 := json.RawMessage(`["hello","world"]`)
	assert.False(t, hasBinaryRaw(raw2))

	// Invalid JSON.
	raw3 := json.RawMessage(`not json`)
	assert.False(t, hasBinaryRaw(raw3))
}

func TestReconstructBinaryEdgeCases(t *testing.T) {
	t.Parallel()

	// Non-placeholder map.
	data := map[string]any{"key": "value"}
	result := reconstructBinary(data, nil)
	assert.Equal(t, data, result)

	// Slice with non-binary items.
	slice := []any{"hello", 42}
	result = reconstructBinary(slice, nil)
	assert.Equal(t, slice, result)

	// Primitive value.
	result = reconstructBinary("hello", nil)
	assert.Equal(t, "hello", result)
}

func TestDeconstructBinaryMap(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"name":  "test",
		"bytes": []byte{1, 2, 3},
	}

	deconstructed, buffers := deconstructBinary(data)

	result, ok := deconstructed.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, 1, len(buffers))

	// Verify placeholder.
	bytesVal := result["bytes"]
	placeholder, ok := bytesVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, placeholder["_placeholder"])
	assert.Equal(t, 0, placeholder["num"])
}

func TestBackoffNegativeDuration(t *testing.T) {
	t.Parallel()

	cfg := SocketIOConfig{
		ReconnectionDelay:    0,
		ReconnectionDelayMax: 0,
		JitterFactor:         0.5,
	}

	b := newBackoff(cfg)

	// min=1s, max=30s, but with jitter, duration should still be non-negative.
	for range 10 {
		d := b.Next()
		assert.True(t, d >= 0, "duration must be non-negative: %v", d)
	}
}

func TestReadSingleEIOPacket_GorillaNoEOF(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)

		defer conn.Close()

		err = conn.WriteMessage(websocket.TextMessage, []byte("42[\"test\"]"))
		require.NoError(t, err)

		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	client := NewClient(server.Client())

	conn, _, err := DialWebSocket(t.Context(), client, wsURL)
	require.NoError(t, err)

	defer conn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	type readResult struct {
		pType   byte
		payload []byte
		err     error
	}

	ch := make(chan readResult, 1)

	go func() {
		pType, payload, err := readSingleEIOPacket(conn)
		ch <- readResult{pType, payload, err}
	}()

	select {
	case <-ctx.Done():
		t.Fatal("timed out: readSingleEIOPacket blocked waiting for connection EOF (io.ReadAll bug)!")
	case res := <-ch:
		require.NoError(t, res.err)
		assert.Equal(t, byte('4'), res.pType)
		assert.Equal(t, []byte("2[\"test\"]"), res.payload)
	}
}

type mockCustomConn struct {
	net.Conn
	reader io.Reader
}

func (m *mockCustomConn) Read(b []byte) (int, error) {
	if m.reader == nil {
		select {}
	}

	n, err := m.reader.Read(b)
	if err == io.EOF {
		m.reader = nil
		return n, io.EOF
	}

	return n, err
}

func TestReadSingleEIOPacket_StreamNoEOF(t *testing.T) {
	t.Parallel()

	mockConn := &mockCustomConn{
		reader: bytes.NewReader([]byte("42[\"custom_frame\"]")),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	type readResult struct {
		pType   byte
		payload []byte
		err     error
	}

	ch := make(chan readResult, 1)

	go func() {
		pType, payload, err := readSingleEIOPacket(mockConn)
		ch <- readResult{pType, payload, err}
	}()

	select {
	case <-ctx.Done():
		t.Fatal("timed out: readSingleEIOPacket blocked on second Read call (io.ReadAll bug)!")
	case res := <-ch:
		require.NoError(t, res.err)
		assert.Equal(t, byte('4'), res.pType)
		assert.Equal(t, []byte("2[\"custom_frame\"]"), res.payload)
	}
}

func TestSocketIO_SuccessfulFlow(t *testing.T) {
	t.Parallel()

	eventCh := make(chan string, 1)

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		// 1. Send EIO open
		err := conn.WriteMessage(
			websocket.TextMessage,
			[]byte(`0{"sid":"session-abc","pingInterval":100,"pingTimeout":100}`),
		)
		require.NoError(t, err)

		// 2. Read EIO connect message
		mt, data, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.TextMessage, mt)
		assert.Equal(t, "40", string(data))

		// 3. Send SIO connect response
		err = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"session-abc","pid":"pid-123"}`))
		require.NoError(t, err)

		// 4. Send custom event from server to client
		time.Sleep(10 * time.Millisecond)

		err = conn.WriteMessage(websocket.TextMessage, []byte(`42["message",{"content":"hello"}]`))
		require.NoError(t, err)
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 1 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	assert.True(t, sio.Connected())
	assert.Equal(t, "session-abc", sio.SID())

	sio.On("message", func(args []json.RawMessage) {
		require.Len(t, args, 1)

		eventCh <- string(args[0])
	})

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event")
	case content := <-eventCh:
		assert.Contains(t, content, "hello")
	}

	// Close connection
	err = sio.Close()
	require.NoError(t, err)
}

func TestSocketIO_ConnectRejected(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		// Send EIO open
		_ = conn.WriteMessage(
			websocket.TextMessage,
			[]byte(`0{"sid":"session-abc","pingInterval":100,"pingTimeout":100}`),
		)
		// Read connect
		_, _, _ = conn.ReadMessage()
		// Send SIO connect error
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`44{"message":"unauthorized"}`))
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	_, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connect rejected: {\"message\":\"unauthorized\"}")
}

func TestSocketIO_Reconnection(t *testing.T) {
	t.Parallel()

	var connCount int32

	reconnectedCh := make(chan struct{})

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		count := atomic.AddInt32(&connCount, 1)

		switch count {
		case 1:
			// First connection: open and then drop
			_ = conn.WriteMessage(
				websocket.TextMessage,
				[]byte(`0{"sid":"session-1","pingInterval":200,"pingTimeout":200}`),
			)
			_, _, _ = conn.ReadMessage()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"session-1"}`))

			// Disconnect abruptly
			time.Sleep(50 * time.Millisecond)

			_ = conn.Close()

		case 2:
			// Second connection (reconnect)
			_ = conn.WriteMessage(
				websocket.TextMessage,
				[]byte(`0{"sid":"session-2","pingInterval":200,"pingTimeout":200}`),
			)
			_, _, _ = conn.ReadMessage()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"session-2"}`))
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Reconnection:      true,
		ReconnectionDelay: 10 * time.Millisecond,
		ConnectTimeout:    500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	var reconnectCount int32
	sio.OnReconnecting(func(attempt int) {
		atomic.StoreInt32(&reconnectCount, int32(attempt))
	})

	sio.OnReconnected(func() {
		close(reconnectedCh)
	})

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnection")
	case <-reconnectedCh:
		// Reconnection succeeded!
		assert.Equal(t, "session-2", sio.SID())
		assert.GreaterOrEqual(t, atomic.LoadInt32(&reconnectCount), int32(1))
		assert.Equal(t, 0, sio.ReconnectionAttempts())
	}
}

func TestSocketIO_Acknowledgment(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		// Wait for the client to send an event with ACK ID
		mt, data, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.TextMessage, mt)

		// The message should be like `421["ask",...]`
		msgStr := string(data)
		if strings.Contains(msgStr, "ask") {
			err = conn.WriteMessage(websocket.TextMessage, []byte(`431["response_val"]`))
			require.NoError(t, err)
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	resp, err := sio.EmitWithAck(ctx, "ask", "payload")
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, `"response_val"`, string(resp[0]))
}

func TestSocketIO_Namespaces(t *testing.T) {
	t.Parallel()

	adminConnected := make(chan struct{})

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		// Open handshake
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))

		// Read "/" connect
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		// Читаем событие на "/admin" напрямую (передается через то же соединение)
		_, data, _ := conn.ReadMessage()
		if strings.Contains(string(data), "/admin,") && strings.Contains(string(data), "foo") {
			close(adminConnected)
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Namespace:      "/",
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	// Connect to /admin
	adminSocket := sio.OnNamespace("/admin")
	require.NotNil(t, adminSocket)

	// Emit some event from /admin
	err = adminSocket.Emit("foo", "bar")
	require.NoError(t, err)

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for namespace connect")
	case <-adminConnected:
		// Success
	}
}

func TestSocketIO_OnAny(t *testing.T) {
	t.Parallel()

	anyCh := make(chan string, 1)

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		time.Sleep(10 * time.Millisecond)

		_ = conn.WriteMessage(websocket.TextMessage, []byte(`42["any_event","any_payload"]`))
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	sio.OnAny(func(event string, args []json.RawMessage) {
		if event == "any_event" {
			anyCh <- string(args[0])
		}
	})

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for catch-all event")
	case payload := <-anyCh:
		assert.Equal(t, `"any_payload"`, payload)
	}
}

func TestNamespaceSocket_AllMethods(t *testing.T) {
	t.Parallel()

	sio := &SocketIOConn{
		namespace: "/",
		nsEvents:  make(map[string]map[string]func(args []json.RawMessage)),
	}
	sio.stateMu.Lock()
	sio.state = sioStateOpen
	sio.stateMu.Unlock()

	ns := sio.OnNamespace("/chat")
	require.NotNil(t, ns)

	calledOn := false
	ns.On("join", func(args []json.RawMessage) {
		calledOn = true
	})
	sio.mu.RLock()
	handler := sio.nsEvents["/chat"]["join"]
	sio.mu.RUnlock()
	require.NotNil(t, handler)
	handler(nil)
	assert.True(t, calledOn)

	calledAny := false
	ns.OnAny(func(event string, args []json.RawMessage) {
		calledAny = true

		assert.Equal(t, "hello", event)
	})
	sio.mu.RLock()
	handlerAny := sio.nsEvents["/chat"]["*"]
	sio.mu.RUnlock()
	require.NotNil(t, handlerAny)
	handlerAny([]json.RawMessage{json.RawMessage(`"hello"`)})
	assert.True(t, calledAny)
}

func TestHasBinaryAdvanced(t *testing.T) {
	t.Parallel()

	mRaw := map[string]json.RawMessage{
		"bin": json.RawMessage(`"hello"`),
	}
	assert.False(t, hasBinary(mRaw))

	mRawBin := map[string]json.RawMessage{
		"bin": json.RawMessage(`{"_placeholder":true,"num":0}`),
	}
	assert.False(t, hasBinary(mRawBin))
}

func TestReconstructBinaryTypes(t *testing.T) {
	t.Parallel()

	buffers := [][]byte{{9, 9, 9}}

	res1 := reconstructBinary(map[string]any{"_placeholder": true, "num": float64(0)}, buffers)
	assert.Equal(t, []byte{9, 9, 9}, res1)

	res2 := reconstructBinary(map[string]any{"_placeholder": true, "num": int(0)}, buffers)
	assert.Equal(t, []byte{9, 9, 9}, res2)

	res3 := reconstructBinary(map[string]any{"_placeholder": true, "num": json.Number("0")}, buffers)
	assert.Equal(t, []byte{9, 9, 9}, res3)

	res4 := reconstructBinary(map[string]any{"_placeholder": false, "num": int(0)}, buffers)
	assert.Equal(t, map[string]any{"_placeholder": false, "num": int(0)}, res4)

	res5 := reconstructBinary(map[string]any{"_placeholder": true, "num": int(5)}, buffers)
	assert.Equal(t, map[string]any{"_placeholder": true, "num": int(5)}, res5)
}

func TestBinaryReconstructorLimits(t *testing.T) {
	t.Parallel()

	pkt := &sioPacket{Type: sioBinaryEvent}
	br := newBinaryReconstructor(100, pkt)
	assert.Equal(t, 64, br.attachments)

	br2 := newBinaryReconstructor(2, pkt)
	largeBuf := make([]byte, 33*1024*1024)
	assert.True(t, br2.addBuffer(largeBuf))
}

func TestDecodeSIOPacketErrorsAndEdges(t *testing.T) {
	t.Parallel()

	_, err := decodeSIOPacket([]byte("5abc-"))
	assert.Error(t, err)

	_, err2 := decodeSIOPacket([]byte("5-1-"))
	assert.Error(t, err2)

	_, err3 := decodeSIOPacket([]byte("565-"))
	assert.Error(t, err3)

	pkt, err4 := decodeSIOPacket([]byte("2/chat"))
	assert.NoError(t, err4)
	assert.Equal(t, "/chat", pkt.Namespace)

	pkt2, err5 := decodeSIOPacket([]byte("2/chat,data"))
	assert.NoError(t, err5)
	assert.Equal(t, "/chat", pkt2.Namespace)
	assert.Equal(t, json.RawMessage("data"), pkt2.Data)

	pkt3, err6 := decodeSIOPacket([]byte("3a"))
	assert.NoError(t, err6)
	assert.Nil(t, pkt3.ID)
}

type mockSizeLimitConn struct {
	net.Conn
}

func (m *mockSizeLimitConn) Read(b []byte) (int, error) {
	return copy(b, strings.Repeat("a", len(b))), nil
}

func TestReadSingleEIOPacket_TooLarge(t *testing.T) {
	t.Parallel()

	conn := &mockSizeLimitConn{}
	_, _, err := readSingleEIOPacket(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "packet too large")
}

func TestEmitVolatile(t *testing.T) {
	t.Parallel()

	sio := &SocketIOConn{
		namespace: "/",
	}

	sio.stateMu.Lock()
	sio.state = sioStateClosed
	sio.stateMu.Unlock()

	err := sio.EmitVolatile("test", "data")
	assert.NoError(t, err)
}

func TestCallbacksSetting(t *testing.T) {
	t.Parallel()

	sio := &SocketIOConn{}

	var closed, reconnected, failed bool

	sio.OnClose(func() { closed = true })
	sio.OnReconnected(func() { reconnected = true })
	sio.OnReconnectFailed(func() { failed = true })

	sio.mu.RLock()
	assert.NotNil(t, sio.onClose)
	assert.NotNil(t, sio.onReconnected)
	assert.NotNil(t, sio.onReconnectFailed)
	sio.mu.RUnlock()

	sio.onClose()
	sio.onReconnected()
	sio.onReconnectFailed()

	assert.True(t, closed)
	assert.True(t, reconnected)
	assert.True(t, failed)
}

func TestReadLoopUnrecognizedEIOType(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		time.Sleep(10 * time.Millisecond)

		_ = conn.WriteMessage(websocket.TextMessage, []byte("6"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("1"))
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	time.Sleep(100 * time.Millisecond)
	assert.False(t, sio.Connected())
}

func TestReadLoopBinaryBufNil(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		time.Sleep(10 * time.Millisecond)

		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3})
		_ = conn.WriteMessage(websocket.TextMessage, []byte("1"))
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	time.Sleep(100 * time.Millisecond)
}

func TestDispatchPacketErrors(t *testing.T) {
	t.Parallel()

	sio := &SocketIOConn{}

	sio.handleSIOPacket([]byte("2"))
	sio.handleSIOPacket([]byte("2{}"))
	sio.handleSIOPacket([]byte("2[]"))
	sio.handleSIOPacket([]byte("2[123]"))
}

func TestReadEIOPacketCtxCancellation(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()

			select {}
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	defer conn.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err = readEIOPacketCtx(ctx, conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestNamespaceSocket_EmitAndAck(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		_, data, _ := conn.ReadMessage()
		if strings.Contains(string(data), "/admin,") && strings.Contains(string(data), "foo") {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`43/admin,1["ack_foo"]`))
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	ns := sio.OnNamespace("/admin")
	resp, err := ns.EmitWithAck(ctx, "foo", "bar")
	require.NoError(t, err)
	require.Len(t, resp, 1)
	assert.Equal(t, `"ack_foo"`, string(resp[0]))

	err = ns.EmitVolatile("foo_vol", "bar_vol")
	assert.NoError(t, err)
}

func TestNamespaceSocket_EmitCallback(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		_, data, _ := conn.ReadMessage()
		msg := string(data)

		idx := strings.Index(msg, "/admin,")
		if idx != -1 {
			start := idx + len("/admin,")

			end := start
			for end < len(msg) && msg[end] >= '0' && msg[end] <= '9' {
				end++
			}

			idStr := msg[start:end]

			_ = conn.WriteMessage(websocket.TextMessage, []byte(`43/admin,`+idStr+`["ack_cb"]`))
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Namespace:      "/",
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	ns := sio.OnNamespace("/admin")

	cbCalled := make(chan struct{})
	err = ns.Emit("foo_cb", "bar", func(args []json.RawMessage) {
		assert.Len(t, args, 1)
		assert.Equal(t, `"ack_cb"`, string(args[0]))
		close(cbCalled)
	})
	require.NoError(t, err)

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("callback not called")
	case <-cbCalled:
	}
}

func TestNamespaceSocket_EmitBinary(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		var (
			headerData     []byte
			attachmentData []byte
		)

		for range 5 {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}

			if strings.Contains(string(data), "51-/admin,") {
				headerData = data
			} else if len(data) > 0 && data[0] == 'b' {
				attachmentData = data
			}
		}

		assert.NotEmpty(t, headerData, "binary event header not found")
		assert.NotEmpty(t, attachmentData, "binary attachment not found")

		if len(attachmentData) > 0 {
			assert.Equal(t, []byte{1, 2, 3}, attachmentData[1:])
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Namespace:      "/",
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	ns := sio.OnNamespace("/admin")
	err = ns.Emit("bin_event", []byte{1, 2, 3})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

func TestSocketIO_PingTimeout(t *testing.T) {
	t.Parallel()

	reconnectCh := make(chan struct{})

	var reconnectedClosed int32

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":100,"pingTimeout":100}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

		_, _, err := conn.ReadMessage()
		if err == nil {
			_ = conn.Close()
		}

		if atomic.CompareAndSwapInt32(&reconnectedClosed, 0, 1) {
			close(reconnectCh)
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Reconnection:      true,
		ReconnectionDelay: 10 * time.Millisecond,
		ConnectTimeout:    500 * time.Millisecond,
		PingTimeout:       100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ping timeout reconnection")
	case <-reconnectCh:
	}
}

func TestSocketIO_ReconnectFailed(t *testing.T) {
	t.Parallel()

	var connCount int32

	failedCh := make(chan struct{})

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		count := atomic.AddInt32(&connCount, 1)

		if count == 1 {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":100,"pingTimeout":100}`))
			_, _, _ = conn.ReadMessage()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))

			time.Sleep(20 * time.Millisecond)

			_ = conn.Close()
		}
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		Reconnection:         true,
		ReconnectionAttempts: 1,
		ReconnectionDelay:    10 * time.Millisecond,
		ConnectTimeout:       200 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sio.Close() })

	sio.OnReconnectFailed(func() {
		close(failedCh)
	})

	sio.mu.Lock()
	sio.targetURL = "ws://127.0.0.1:1"
	sio.mu.Unlock()

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect failed")
	case <-failedCh:
	}
}

func TestSocketIO_CloseMultipleTimes(t *testing.T) {
	t.Parallel()

	server := newMockSIOServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s","pingInterval":500,"pingTimeout":500}`))
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s"}`))
	})

	wsURL := strings.Replace(server.server.URL, "http://", "ws://", 1)
	client := NewClient(server.server.Client())

	sio, err := DialSocketIO(t.Context(), client, wsURL, SocketIOConfig{
		ConnectTimeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)

	err = sio.Close()
	assert.NoError(t, err)

	err = sio.Close()
	assert.NoError(t, err)
}

func TestReadEIOPacketCtx_ConnClosed(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	defer conn.Close()

	_, _, err = readEIOPacketCtx(t.Context(), conn)
	assert.Error(t, err)
}

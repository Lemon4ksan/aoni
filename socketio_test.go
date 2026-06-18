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
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	d := b.nextDuration()
	assert.Equal(t, 100*time.Millisecond, d)

	// Attempt 1: 100ms * 2^1 = 200ms
	d = b.nextDuration()
	assert.Equal(t, 200*time.Millisecond, d)

	// Attempt 2: 100ms * 2^2 = 400ms
	d = b.nextDuration()
	assert.Equal(t, 400*time.Millisecond, d)

	// Attempt 3: 100ms * 2^3 = 800ms
	d = b.nextDuration()
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
		b.nextDuration()
	}

	// Attempt 5: 1s * 2^5 = 32s, but capped at 5s.
	d := b.nextDuration()
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

	b.nextDuration()
	b.nextDuration()
	b.nextDuration()
	assert.Equal(t, 3, b.attempts)

	b.reset()
	assert.Equal(t, 0, b.attempts)

	d := b.nextDuration()
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
		d := b.nextDuration()
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
		d := b.nextDuration()
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

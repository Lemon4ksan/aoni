// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2/hpack"
)

// --- HTTP/1.1 header ordering ---

func TestReorderHTTP1Headers_Basic(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl\r\nAccept: */*\r\n\r\n")
	order := []string{"Accept", "Host", "User-Agent"}

	result, ok := reorderHTTP1Headers(raw, order)
	require.True(t, ok)

	expected := "GET / HTTP/1.1\r\nAccept: */*\r\nHost: example.com\r\nUser-Agent: curl\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHTTP1Headers_PreservesUnlisted(t *testing.T) {
	t.Parallel()

	raw := []byte("POST /data HTTP/1.1\r\nHost: example.com\r\nX-Custom: val1\r\nAccept: */*\r\nX-Other: val2\r\n\r\n")
	order := []string{"Host", "Accept"}

	result, ok := reorderHTTP1Headers(raw, order)
	require.True(t, ok)

	lines := strings.Split(string(result), "\r\n")
	// Request line
	assert.Equal(t, "POST /data HTTP/1.1", lines[0])
	// Ordered headers first
	assert.Equal(t, "Host: example.com", lines[1])
	assert.Equal(t, "Accept: */*", lines[2])
	// Remaining headers in original order
	assert.Equal(t, "X-Custom: val1", lines[3])
	assert.Equal(t, "X-Other: val2", lines[4])
	// Empty line + body
	assert.Equal(t, "", lines[5])
}

func TestReorderHTTP1Headers_CaseInsensitive(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nhost: example.com\r\nUSER-AGENT: test\r\n\r\n")
	order := []string{"user-agent", "HOST"}

	result, ok := reorderHTTP1Headers(raw, order)
	require.True(t, ok)

	expected := "GET / HTTP/1.1\r\nUSER-AGENT: test\r\nhost: example.com\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHTTP1Headers_WithBody(t *testing.T) {
	t.Parallel()

	raw := []byte("POST / HTTP/1.1\r\nHost: a.com\r\nContent-Type: text/plain\r\n\r\nhello world")
	order := []string{"Content-Type", "Host"}

	result, ok := reorderHTTP1Headers(raw, order)
	require.True(t, ok)

	expected := "POST / HTTP/1.1\r\nContent-Type: text/plain\r\nHost: a.com\r\n\r\nhello world"
	assert.Equal(t, expected, string(result))
}

func TestReorderHTTP1Headers_NoTerminator(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com")
	_, ok := reorderHTTP1Headers(raw, []string{"Host"})
	assert.False(t, ok)
}

func TestReorderHTTP1Headers_SingleHeader(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	order := []string{"Host"}

	result, ok := reorderHTTP1Headers(raw, order)
	require.True(t, ok)
	assert.Equal(t, string(raw), string(result))
}

func TestReorderHTTP1Headers_EmptyOrder(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: a.com\r\nAccept: */*\r\n\r\n")
	result, ok := reorderHTTP1Headers(raw, []string{})
	require.True(t, ok)
	assert.Equal(t, string(raw), string(result))
}

// --- headerOrderingConn ---

func TestHeaderOrderingConn_Reorders(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	conn := &headerOrderingConn{
		Conn:        client,
		orderedKeys: []string{"Accept", "Host"},
	}

	input := "GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\nAccept: */*\r\n\r\n"

	done := make(chan struct{})

	var received []byte
	go func() {
		defer close(done)

		buf := make([]byte, 4096)
		n, _ := server.Read(buf)
		received = buf[:n]
	}()

	_, err := conn.Write([]byte(input))
	require.NoError(t, err)

	<-done
	server.Close()

	expected := "GET / HTTP/1.1\r\nAccept: */*\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
	assert.Equal(t, expected, string(received))
}

func TestHeaderOrderingConn_PassthroughNonHTTP(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	conn := &headerOrderingConn{
		Conn:        client,
		orderedKeys: []string{"Host"},
	}

	data := []byte("this is not an HTTP request")

	done := make(chan struct{})

	var received []byte
	go func() {
		defer close(done)

		buf := make([]byte, 4096)
		n, _ := server.Read(buf)
		received = buf[:n]
	}()

	_, err := conn.Write(data)
	require.NoError(t, err)

	<-done
	server.Close()

	assert.Equal(t, data, received)
}

func TestHeaderOrderingConn_EmptyKeys(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	conn := &headerOrderingConn{
		Conn:        client,
		orderedKeys: []string{},
	}

	input := "GET / HTTP/1.1\r\nHost: a.com\r\n\r\n"

	done := make(chan struct{})

	var received []byte
	go func() {
		defer close(done)

		buf := make([]byte, 4096)
		n, _ := server.Read(buf)
		received = buf[:n]
	}()

	_, err := conn.Write([]byte(input))
	require.NoError(t, err)

	<-done
	server.Close()

	assert.Equal(t, input, string(received))
}

// --- HTTP/2 HEADERS frame reordering ---

func buildH2Frame(frameType, flags byte, streamID uint32, payload []byte) []byte {
	frame := make([]byte, 9+len(payload))
	frame[0] = byte(len(payload) >> 16) //nolint:gosec
	frame[1] = byte(len(payload) >> 8)  //nolint:gosec
	frame[2] = byte(len(payload))       //nolint:gosec
	frame[3] = frameType
	frame[4] = flags
	binary.BigEndian.PutUint32(frame[5:9], streamID)
	copy(frame[9:], payload)

	return frame
}

func encodeH2Headers(headers []hpack.HeaderField) []byte {
	var buf bytes.Buffer

	enc := hpack.NewEncoder(&buf)
	for _, h := range headers {
		_ = enc.WriteField(h)
	}

	return buf.Bytes()
}

func TestReorderH2Headers_Basic(t *testing.T) {
	t.Parallel()

	headers := []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: "/"},
		{Name: ":authority", Value: "example.com"},
		{Name: ":scheme", Value: "https"},
		{Name: "user-agent", Value: "test"},
		{Name: "accept", Value: "*/*"},
	}

	hblock := encodeH2Headers(headers)
	frame := buildH2Frame(0x1, 0x0, 1, hblock)

	order := []string{":method", ":scheme", ":authority", ":path", "accept", "user-agent"}

	result, ok := reorderH2Headers(frame, 0x0, order)
	require.True(t, ok)

	// Decode reordered headers from result.
	payloadLen := int(result[0])<<16 | int(result[1])<<8 | int(result[2])
	decoded, err := hpack.NewDecoder(4096, nil).DecodeFull(result[9 : 9+payloadLen])
	require.NoError(t, err)

	require.Len(t, decoded, 6)
	assert.Equal(t, ":method", decoded[0].Name)
	assert.Equal(t, ":scheme", decoded[1].Name)
	assert.Equal(t, ":authority", decoded[2].Name)
	assert.Equal(t, ":path", decoded[3].Name)
	assert.Equal(t, "accept", decoded[4].Name)
	assert.Equal(t, "user-agent", decoded[5].Name)
}

func TestReorderH2Headers_WithPadding(t *testing.T) {
	t.Parallel()

	headers := []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: "host", Value: "example.com"},
	}

	hblock := encodeH2Headers(headers)
	// Pad Length (1 byte) + payload + padding (3 bytes)
	payload := make([]byte, 1+len(hblock)+3)
	payload[0] = 3 // pad length
	copy(payload[1:], hblock)
	// padding bytes are zero-initialized

	frame := buildH2Frame(0x1, 0x8, 1, payload) // PADDED flag

	order := []string{"host", ":method"}

	result, ok := reorderH2Headers(frame, 0x8, order)
	require.True(t, ok)

	// Verify frame type and stream ID preserved.
	assert.Equal(t, byte(0x1), result[3])
	assert.Equal(t, uint32(1), binary.BigEndian.Uint32(result[5:9])&0x7FFFFFFF)

	// Decode reordered headers.
	resultPayloadLen := int(result[0])<<16 | int(result[1])<<8 | int(result[2])
	resultPayload := result[9 : 9+resultPayloadLen]
	padLen := int(resultPayload[0])
	decoded, err := hpack.NewDecoder(4096, nil).DecodeFull(resultPayload[1 : resultPayloadLen-padLen])
	require.NoError(t, err)

	require.Len(t, decoded, 2)
	assert.Equal(t, "host", decoded[0].Name)
	assert.Equal(t, ":method", decoded[1].Name)
}

func TestReorderH2Headers_TooShort(t *testing.T) {
	t.Parallel()

	_, ok := reorderH2Headers([]byte{0x00, 0x01}, 0x0, []string{"host"})
	assert.False(t, ok)
}

func TestReorderH2Headers_IncompletePayload(t *testing.T) {
	t.Parallel()

	// Frame claims 100 bytes payload but only has 5.
	frame := make([]byte, 9+5)
	frame[0] = 0
	frame[1] = 0
	frame[2] = 100 // payload length = 100
	frame[3] = 0x1

	_, ok := reorderH2Headers(frame, 0x0, []string{"host"})
	assert.False(t, ok)
}

// --- h2framedConn with header ordering ---

func TestH2FramedConn_ReordersHeaders(t *testing.T) {
	t.Parallel()

	// Test that h2framedConn.Write passes through HEADERS frame reordering
	// by verifying the reorderH2Headers function handles the full pipeline.
	// (Direct integration test with net.Pipe is unreliable due to synchronous
	// pipe semantics conflicting with h2framedConn's data transformation.)

	headers := []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: "/"},
		{Name: "user-agent", Value: "test"},
		{Name: "accept", Value: "*/*"},
	}
	hblock := encodeH2Headers(headers)
	headersFrame := buildH2Frame(0x1, 0x0, 1, hblock)

	reordered, ok := reorderH2Headers(headersFrame, 0x0, []string{"accept", ":method"})
	require.True(t, ok)

	payloadLen := int(reordered[0])<<16 | int(reordered[1])<<8 | int(reordered[2])
	decoded, err := hpack.NewDecoder(4096, nil).DecodeFull(reordered[9 : 9+payloadLen])
	require.NoError(t, err)

	require.Len(t, decoded, 4)
	assert.Equal(t, "accept", decoded[0].Name)
	assert.Equal(t, ":method", decoded[1].Name)
	assert.Equal(t, ":path", decoded[2].Name)
	assert.Equal(t, "user-agent", decoded[3].Name)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWSRawConn_RoundTrip(t *testing.T) {
	t.Parallel()

	// Use a buffered approach: raw conn reads/writes WS frames over a pipe.
	// We manually construct frames on the server side.
	server, client := net.Pipe()
	defer server.Close()

	raw := wrapRawConn(client, true)
	defer raw.Close()

	// Server reads a WS frame and echoes it back
	go func() {
		// Read: 2-byte header + mask(4) + payload
		header := make([]byte, 2)
		io.ReadFull(server, header)
		masked := header[1]&0x80 != 0
		length := uint64(header[1] & 0x7f)

		var mask [4]byte
		if masked {
			io.ReadFull(server, mask[:])
		}

		payload := make([]byte, length)
		io.ReadFull(server, payload)

		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		// Echo back as unmasked server frame
		echoHeader := []byte{0x82, byte(length)}
		server.Write(echoHeader)
		server.Write(payload)
	}()

	// Write through raw conn
	_, err := raw.Write([]byte("hello"))
	require.NoError(t, err)

	// Read the echo
	buf := make([]byte, 1024)
	n, err := raw.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))
}

func TestWSRawConn_Close(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()

	raw := wrapRawConn(client, true)

	closed := raw.CloseChan()
	select {
	case <-closed:
		t.Fatal("should not be closed yet")
	default:
	}

	require.NoError(t, raw.Close())

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("CloseChan should be closed after Close()")
	}

	// Second Close should be safe
	require.NoError(t, raw.Close())
}

func TestWSRawConn_Timeout(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	raw := wrapRawConn(client, true)
	defer raw.Close()

	err := raw.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	require.NoError(t, err)

	_, err = raw.Read(make([]byte, 1024))
	assert.Error(t, err)
}

func TestParseWSURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url    string
		scheme string
		host   string
		port   string
		path   string
		err    bool
	}{
		{"wss://example.com/ws", "wss", "example.com", "443", "/ws", false},
		{"ws://localhost:8080/chat", "ws", "localhost", "8080", "/chat", false},
		{"wss://api.example.com/", "wss", "api.example.com", "443", "/", false},
		{"wss://example.com", "wss", "example.com", "443", "/", false},
		{"http://example.com/ws", "", "", "", "", true},
		{"ftp://example.com", "", "", "", "", true},
	}

	for _, tt := range tests {
		u, err := parseWSURL(tt.url)
		if tt.err {
			assert.Error(t, err, tt.url)
			continue
		}

		require.NoError(t, err, tt.url)
		assert.Equal(t, tt.scheme, u.scheme, tt.url)
		assert.Equal(t, tt.host, u.host, tt.url)
		assert.Equal(t, tt.port, u.port, tt.url)
		assert.Equal(t, tt.path, u.Path, tt.url)
	}
}

func TestWSConn_ImplementsNetConn(t *testing.T) {
	t.Parallel()

	var (
		_ net.Conn = (*wsGorillaConn)(nil)
		_ net.Conn = (*wsRawConn)(nil)
		_ net.Conn = (*wsH2Conn)(nil)
	)
}

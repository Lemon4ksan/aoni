// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1_test

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/h1"
)

func TestReorderHeaders_Basic(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl\r\nAccept: */*\r\n\r\n")
	order := []string{"Accept", "Host", "User-Agent"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	expected := "GET / HTTP/1.1\r\nAccept: */*\r\nHost: example.com\r\nUser-Agent: curl\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHeaders_AuthorityMapping(t *testing.T) {
	t.Parallel()

	// HTTP/2 and HTTP/3 profiles use :authority which maps to Host in HTTP/1.1
	raw := []byte("GET /api/v1 HTTP/1.1\r\nUser-Agent: curl\r\nHost: example.com\r\nAccept: */*\r\n\r\n")
	order := []string{":authority", "User-Agent"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	expected := "GET /api/v1 HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl\r\nAccept: */*\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHeaders_DuplicateHeaders(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nSet-Cookie: a=1\r\nHost: example.com\r\nSet-Cookie: b=2\r\n\r\n")
	order := []string{"Host", "Set-Cookie"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	lines := strings.Split(string(result), "\r\n")
	assert.Equal(t, "GET / HTTP/1.1", lines[0])
	assert.Equal(t, "Host: example.com", lines[1])
	assert.Equal(t, "Set-Cookie: a=1", lines[2])
	assert.Equal(t, "Set-Cookie: b=2", lines[3])
}

func TestReorderHeaders_ComplexHeaderValues(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nReferer: http://example.com:8080/path?a=1:2\r\nHost: example.com:8080\r\n\r\n")
	order := []string{"Host", "Referer"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	expected := "GET / HTTP/1.1\r\nHost: example.com:8080\r\nReferer: http://example.com:8080/path?a=1:2\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHeaders_PreservesUnlisted(t *testing.T) {
	t.Parallel()

	raw := []byte("POST /data HTTP/1.1\r\nHost: example.com\r\nX-Custom: val1\r\nAccept: */*\r\nX-Other: val2\r\n\r\n")
	order := []string{"Host", "Accept"}

	result, ok := h1.ReorderHeaders(raw, order)
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
	// Empty line
	assert.Equal(t, "", lines[5])
}

func TestReorderHeaders_CaseInsensitive(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nhost: example.com\r\nUSER-AGENT: test\r\n\r\n")
	order := []string{"user-agent", "HOST"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	expected := "GET / HTTP/1.1\r\nUSER-AGENT: test\r\nhost: example.com\r\n\r\n"
	assert.Equal(t, expected, string(result))
}

func TestReorderHeaders_WithBody(t *testing.T) {
	t.Parallel()

	raw := []byte("POST / HTTP/1.1\r\nHost: a.com\r\nContent-Type: text/plain\r\n\r\nhello world")
	order := []string{"Content-Type", "Host"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)

	expected := "POST / HTTP/1.1\r\nContent-Type: text/plain\r\nHost: a.com\r\n\r\nhello world"
	assert.Equal(t, expected, string(result))
}

func TestReorderHeaders_NoTerminator(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com")
	_, ok := h1.ReorderHeaders(raw, []string{"Host"})
	assert.False(t, ok)
}

func TestReorderHeaders_SingleHeader(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	order := []string{"Host"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)
	assert.Equal(t, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", string(result))
}

func TestReorderHeaders_EmptyOrder(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: a.com\r\nAccept: */*\r\n\r\n")
	result, ok := h1.ReorderHeaders(raw, []string{})
	require.True(t, ok)
	assert.Equal(t, "GET / HTTP/1.1\r\nHost: a.com\r\nAccept: */*\r\n\r\n", string(result))
}

func TestReorderHeaders_LargeHeaderCount(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("GET / HTTP/1.1\r\n")

	for i := range 70 {
		fmt.Fprintf(&sb, "X-Header-%d: val-%d\r\n", i, i)
	}

	sb.WriteString("Host: example.com\r\n\r\n")

	raw := []byte(sb.String())
	order := []string{"Host", "X-Header-69"}

	result, ok := h1.ReorderHeaders(raw, order)
	require.True(t, ok)
	assert.NotEmpty(t, result)
}

func TestReorderHeaders_MalformedInputs(t *testing.T) {
	t.Parallel()

	t.Run("empty payload", func(t *testing.T) {
		t.Parallel()

		res, ok := h1.ReorderHeaders([]byte(""), []string{"Host"})
		assert.False(t, ok)
		assert.Nil(t, res)
	})

	t.Run("missing colon in header lines", func(t *testing.T) {
		t.Parallel()

		malformed := []byte("GET / HTTP/1.1\r\nHost 127.0.0.1\r\n\r\n")
		res, ok := h1.ReorderHeaders(malformed, []string{"Host"})
		assert.True(t, ok)
		assert.Equal(t, []byte("GET / HTTP/1.1\r\n\r\n"), res)
	})

	t.Run("missing CRLF boundaries", func(t *testing.T) {
		t.Parallel()

		malformed := []byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n")
		res, ok := h1.ReorderHeaders(malformed, []string{"Host"})
		assert.False(t, ok)
		assert.Nil(t, res)
	})
}

func TestHeaderOrderingConn_Reorders(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &h1.HeaderOrderingConn{
		Conn:        client,
		OrderedKeys: []string{"Accept", "Host"},
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

	expected := "GET / HTTP/1.1\r\nAccept: */*\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
	assert.Equal(t, expected, string(received))
}

func TestHeaderOrderingConn_PassthroughNonHTTP(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &h1.HeaderOrderingConn{
		Conn:        client,
		OrderedKeys: []string{"Host"},
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

	assert.Equal(t, data, received)
}

func TestHeaderOrderingConn_EmptyKeys(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &h1.HeaderOrderingConn{
		Conn:        client,
		OrderedKeys: []string{},
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

	assert.Equal(t, input, string(received))
}

func TestHeaderOrderingConn_ChunkedWrites(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &h1.HeaderOrderingConn{
		Conn:        client,
		OrderedKeys: []string{"Host"},
	}

	chunk1 := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n")

	done := make(chan struct{})

	var received []byte

	go func() {
		defer close(done)

		buf := make([]byte, 4096)
		n, _ := server.Read(buf)
		received = buf[:n]
	}()

	_, err := conn.Write(chunk1)
	require.NoError(t, err)

	<-done

	assert.Equal(t, chunk1, received)
}

func BenchmarkReorderHeaders(b *testing.B) {
	raw := []byte(
		"POST /v1/users HTTP/1.1\r\nHost: api.example.com\r\nUser-Agent: Mozilla/5.0\r\nAccept: application/json\r\nContent-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"status\":\"ok\"}",
	)
	order := []string{"Content-Type", ":authority", "User-Agent", "Accept"}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, _ = h1.ReorderHeaders(raw, order)
	}
}

func FuzzReorderHeaders(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl\r\n\r\n"))
	f.Add([]byte("POST /api HTTP/1.1\r\nContent-Length: 4\r\n\r\ntest"))
	f.Add([]byte("invalid wire format"))

	f.Fuzz(func(_ *testing.T, raw []byte) {
		order := []string{"Host", ":authority", "User-Agent", "Content-Type", "Accept"}
		_, _ = h1.ReorderHeaders(raw, order)
	})
}

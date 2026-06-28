// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2/hpack"

	"github.com/lemon4ksan/aoni/profiles"
)

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
	assert.Equal(t, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", string(result))
}

func TestReorderHTTP1Headers_EmptyOrder(t *testing.T) {
	t.Parallel()

	raw := []byte("GET / HTTP/1.1\r\nHost: a.com\r\nAccept: */*\r\n\r\n")
	result, ok := reorderHTTP1Headers(raw, []string{})
	require.True(t, ok)
	assert.Equal(t, "GET / HTTP/1.1\r\nHost: a.com\r\nAccept: */*\r\n\r\n", string(result))
}

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

func TestH2SettingsFromProfile(t *testing.T) {
	t.Parallel()

	profSettings := profiles.H2Settings{
		HeaderTableSize:      4096,
		EnablePush:           0,
		MaxConcurrentStreams: 100,
		InitialWindowSize:    65535,
		MaxFrameSize:         16384,
		MaxHeaderListSize:    8192,
		ConnectionFlow:       1048576,
		InitialStreamID:      3,
		PriorityStreamDep:    1,
		PriorityExclusive:    true,
		PriorityWeight:       16,
	}

	settings := H2SettingsFromProfile(profSettings)
	assert.Equal(t, uint32(4096), settings.HeaderTableSize)
	assert.Equal(t, uint32(0), settings.EnablePush)
	assert.Equal(t, uint32(100), settings.MaxConcurrentStreams)
	assert.Equal(t, uint32(65535), settings.InitialWindowSize)
	assert.Equal(t, uint32(16384), settings.MaxFrameSize)
	assert.Equal(t, uint32(8192), settings.MaxHeaderListSize)
	assert.Equal(t, uint32(1048576), settings.ConnectionFlow)
	assert.Equal(t, uint32(3), settings.InitialStreamID)
	assert.Equal(t, uint32(1), settings.PriorityStreamDep)
	assert.True(t, settings.PriorityExclusive)
	assert.Equal(t, uint8(16), settings.PriorityWeight)
}

func TestH2FramedConn_PrefaceChecks(t *testing.T) {
	t.Parallel()

	t.Run("too_short_or_invalid_preface", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		conn := &h2framedConn{
			Conn: client,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, []byte("short"), buf[:n])
		}()

		_, err := conn.Write([]byte("short"))
		require.NoError(t, err)
		<-done

		_ = conn.Close()
		_ = server.Close()
	})

	t.Run("incomplete_preface_less_than_33_bytes", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		conn := &h2framedConn{
			Conn: client,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, 30, n)
		}()

		// Exactly 30 bytes starting with preface
		preface := append([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"), []byte("abcdef")...)
		_, err := conn.Write(preface)
		require.NoError(t, err)
		<-done

		_ = conn.Close()
		_ = server.Close()
	})

	t.Run("invalid_settings_frame_length", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		conn := &h2framedConn{
			Conn: client,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, 31, n)
		}()

		// Preface (24) + settings length (claims 10 bytes) but only 7 bytes provided
		preface := append(
			[]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"),
			[]byte{0x00, 0x00, 0x0a, 0x04, 0x00, 0x00, 0x00}...)
		_, err := conn.Write(preface)
		require.NoError(t, err)
		<-done

		_ = conn.Close()
		_ = server.Close()
	})

	t.Run("subsequent_write_passthrough", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		conn := &h2framedConn{
			Conn:        client,
			prefaceSent: true,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, []byte("subsequent"), buf[:n])
		}()

		_, err := conn.Write([]byte("subsequent"))
		require.NoError(t, err)
		<-done

		_ = conn.Close()
		_ = server.Close()
	})
}

func TestH2FramedConn_WithPriorityFrame(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	conn := &h2framedConn{
		Conn: client,
		settings: HTTP2Settings{
			HeaderTableSize:   65536,
			PriorityStreamDep: 13,
			PriorityExclusive: true,
			PriorityWeight:    16,
		},
	}

	// Preface (24) + SETTINGS (9-byte header + 6-byte payload) + PRIORITY (9-byte header + 5-byte payload)
	settingsPayload := []byte{0x00, 0x01, 0x00, 0x01, 0x00, 0x00} // ID:1, VAL:65536
	settingsFrame := buildH2Frame(0x4, 0x0, 0, settingsPayload)

	// Priority frame header claims 5 bytes payload
	priorityFrame := buildH2Frame(0x2, 0x0, 1, []byte{0x00, 0x00, 0x00, 0x00, 0x00})

	prefaceAndFrames := append([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"), settingsFrame...)
	prefaceAndFrames = append(prefaceAndFrames, priorityFrame...)

	done := make(chan struct{})

	var received []byte
	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		n, _ := server.Read(buf)
		received = make([]byte, n)
		copy(received, buf[:n])
	}()

	_, err := conn.Write(prefaceAndFrames)
	require.NoError(t, err)
	<-done

	assert.True(t, conn.prefaceSent)
	assert.NotEmpty(t, received)

	// Confirm we re-wrote the Priority frame payload to include Weight (16)
	assert.Contains(t, received, byte(16))

	_ = conn.Close()
	_ = server.Close()
}

func TestH2FramedConn_BuildPriorityFrame_TooShort(t *testing.T) {
	t.Parallel()

	conn := &h2framedConn{}
	res := conn.buildPriorityFrame([]byte{0x00, 0x01})
	assert.Nil(t, res)
}

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

func TestReorderH2Headers_WithPriority(t *testing.T) {
	t.Parallel()

	headers := []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: "host", Value: "example.com"},
	}

	hblock := encodeH2Headers(headers)
	// Priority fields (5 bytes) + payload
	payload := make([]byte, 5+len(hblock))
	binary.BigEndian.PutUint32(payload[0:4], 3|0x80000000) // stream dep + exclusive
	payload[4] = 16                                        // weight
	copy(payload[5:], hblock)

	frame := buildH2Frame(0x1, 0x20, 1, payload) // PRIORITY flag (0x20)

	order := []string{"host", ":method"}
	result, ok := reorderH2Headers(frame, 0x20, order)
	require.True(t, ok)

	// Decode reordered headers from result
	resultPayloadLen := int(result[0])<<16 | int(result[1])<<8 | int(result[2])
	resultPayload := result[9 : 9+resultPayloadLen]
	decoded, err := hpack.NewDecoder(4096, nil).DecodeFull(resultPayload[5:]) // skip 5 bytes priority
	require.NoError(t, err)

	require.Len(t, decoded, 2)
	assert.Equal(t, "host", decoded[0].Name)
	assert.Equal(t, ":method", decoded[1].Name)
}

func TestReorderH2Headers_BadFrames(t *testing.T) {
	t.Parallel()

	t.Run("too_short_header", func(t *testing.T) {
		t.Parallel()

		_, ok := reorderH2Headers([]byte{0x00, 0x01}, 0x0, []string{"host"})
		assert.False(t, ok)
	})

	t.Run("incomplete_payload", func(t *testing.T) {
		t.Parallel()
		// Frame claims 100 bytes payload but only has 5.
		frame := make([]byte, 9+5)
		frame[0] = 0
		frame[1] = 0
		frame[2] = 100 // payload length = 100
		frame[3] = 0x1
		_, ok := reorderH2Headers(frame, 0x0, []string{"host"})
		assert.False(t, ok)
	})

	t.Run("offset_exceeds_payload", func(t *testing.T) {
		t.Parallel()
		// PADDED flag (0x8), but only 1 byte payload (which is just pad length)
		// So there's no space for prefix, which triggers: if offset >= len(payload)
		payload := []byte{10} // claims 10 bytes padding, but total length is 1
		frame := buildH2Frame(0x1, 0x8, 1, payload)
		_, ok := reorderH2Headers(frame, 0x8, []string{"host"})
		assert.False(t, ok)
	})

	t.Run("hblockEnd_before_offset", func(t *testing.T) {
		t.Parallel()
		// PADDED flag (0x8) and PRIORITY flag (0x20) -> total flags = 0x28
		// Payload: Pad Length (1 byte) + Priority (5 bytes) = 6 bytes offset.
		// If pad length claims 10 bytes, then hblockEnd = len(payload) - 10 = 6 - 10 = -4.
		// This triggers: if hblockEnd <= offset
		payload := make([]byte, 6)
		payload[0] = 10 // pad length = 10
		frame := buildH2Frame(0x1, 0x28, 1, payload)
		_, ok := reorderH2Headers(frame, 0x28, []string{"host"})
		assert.False(t, ok)
	})

	t.Run("empty_headers", func(t *testing.T) {
		t.Parallel()
		// Decodes empty list of headers (0 length payload)
		frame := buildH2Frame(0x1, 0x0, 1, []byte{})
		_, ok := reorderH2Headers(frame, 0x0, []string{"host"})
		assert.False(t, ok)
	})
}

func TestH2FramedConn_ReordersHeaders(t *testing.T) {
	t.Parallel()

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

func TestH2FramedTransport_Constructor(t *testing.T) {
	t.Parallel()

	base := &http.Transport{}
	settings := HTTP2Settings{HeaderTableSize: 4096}
	tr := NewH2FramedTransport(base, settings, "Host", "User-Agent")
	require.NotNil(t, tr)

	assert.Equal(t, settings, tr.settings)
	assert.Equal(t, []string{"Host", "User-Agent"}, tr.orderedKeys)
	assert.NotNil(t, tr.DialTLSContext)
}

func TestParseHTTP2Settings(t *testing.T) {
	t.Parallel()

	t.Run("parse_snake_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"header_table_size":65536,"initial_window_size":6291456,"priority_weight":255,"priority_exclusive":true}`

		settings, err := ParseHTTP2Settings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(65536), settings.HeaderTableSize)
		assert.Equal(t, uint32(6291456), settings.InitialWindowSize)
		assert.Equal(t, uint8(255), settings.PriorityWeight)
		assert.True(t, settings.PriorityExclusive)
	})

	t.Run("parse_camel_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"headerTableSize":4096,"initialWindowSize":131072,"priorityWeight":41}`

		settings, err := ParseHTTP2Settings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(4096), settings.HeaderTableSize)
		assert.Equal(t, uint32(131072), settings.InitialWindowSize)
		assert.Equal(t, uint8(41), settings.PriorityWeight)
	})

	t.Run("parse_pascal_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"HeaderTableSize":16384,"InitialWindowSize":262144}`

		settings, err := ParseHTTP2Settings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(16384), settings.HeaderTableSize)
		assert.Equal(t, uint32(262144), settings.InitialWindowSize)
	})

	t.Run("invalid_json_returns_error", func(t *testing.T) {
		t.Parallel()

		_, err := ParseHTTP2Settings(`{"header_table_size": "not_a_number"}`)
		assert.Error(t, err)
	})
}

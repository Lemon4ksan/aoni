// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	impl "github.com/lemon4ksan/aoni/internal/fingerprint/h2"
)

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

	settings := SettingsFromProfile(profSettings)
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

func TestParseSettings(t *testing.T) {
	t.Parallel()

	t.Run("parse_snake_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"header_table_size":65536,"initial_window_size":6291456,"priority_weight":255,"priority_exclusive":true}`

		settings, err := ParseSettings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(65536), settings.HeaderTableSize)
		assert.Equal(t, uint32(6291456), settings.InitialWindowSize)
		assert.Equal(t, uint8(255), settings.PriorityWeight)
		assert.True(t, settings.PriorityExclusive)
	})

	t.Run("parse_camel_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"headerTableSize":4096,"initialWindowSize":131072,"priorityWeight":41}`

		settings, err := ParseSettings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(4096), settings.HeaderTableSize)
		assert.Equal(t, uint32(131072), settings.InitialWindowSize)
		assert.Equal(t, uint8(41), settings.PriorityWeight)
	})

	t.Run("parse_pascal_case", func(t *testing.T) {
		t.Parallel()

		jsonStr := `{"HeaderTableSize":16384,"InitialWindowSize":262144}`

		settings, err := ParseSettings(jsonStr)
		require.NoError(t, err)

		assert.Equal(t, uint32(16384), settings.HeaderTableSize)
		assert.Equal(t, uint32(262144), settings.InitialWindowSize)
	})

	t.Run("invalid_json_returns_error", func(t *testing.T) {
		t.Parallel()

		_, err := ParseSettings(`{"header_table_size": "not_a_number"}`)
		assert.Error(t, err)
	})
}

func TestH2FramedConn_PrefaceChecks(t *testing.T) {
	t.Parallel()

	t.Run("too_short_or_invalid_preface", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		t.Cleanup(func() {
			_ = server.Close()
			_ = client.Close()
		})

		conn := &impl.FramedConn{
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
	})

	t.Run("incomplete_preface_less_than_33_bytes", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		t.Cleanup(func() {
			_ = server.Close()
			_ = client.Close()
		})

		conn := &impl.FramedConn{
			Conn: client,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, 30, n)
		}()

		preface := append([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"), []byte("abcdef")...)
		_, err := conn.Write(preface)
		require.NoError(t, err)
		<-done
	})

	t.Run("invalid_settings_frame_length", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		t.Cleanup(func() {
			_ = server.Close()
			_ = client.Close()
		})

		conn := &impl.FramedConn{
			Conn: client,
		}

		done := make(chan struct{})
		go func() {
			defer close(done)

			buf := make([]byte, 100)
			n, _ := server.Read(buf)
			assert.Equal(t, 31, n)
		}()

		preface := append(
			[]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"),
			[]byte{0x00, 0x00, 0x0a, 0x04, 0x00, 0x00, 0x00}...)
		_, err := conn.Write(preface)
		require.NoError(t, err)
		<-done
	})

	t.Run("subsequent_write_passthrough", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		t.Cleanup(func() {
			_ = server.Close()
			_ = client.Close()
		})

		conn := impl.WrapConn(client, impl.SettingsDTO{}, nil)

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
	})
}

func TestH2FramedConn_WithPriorityFrame(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &impl.FramedConn{
		Conn: client,
		Settings: impl.SettingsDTO{
			HeaderTableSize:   65536,
			PriorityStreamDep: 13,
			PriorityExclusive: true,
			PriorityWeight:    16,
		},
	}

	settingsPayload := []byte{0x00, 0x01, 0x00, 0x01, 0x00, 0x00} // ID:1, VAL:65536
	settingsFrame := buildH2Frame(0x4, 0x0, 0, settingsPayload)

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

	assert.NotEmpty(t, received)

	// Confirm we re-wrote the Priority frame payload to include Weight (16)
	assert.Contains(t, received, byte(16))
}

func TestH2FramedConn_WithWindowUpdate(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	conn := &impl.FramedConn{
		Conn: client,
		Settings: impl.SettingsDTO{
			HeaderTableSize: 65536,
			ConnectionFlow:  15663105, // > 65535, triggers WINDOW_UPDATE frame (0x8)
		},
	}

	settingsPayload := []byte{0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
	settingsFrame := buildH2Frame(0x4, 0x0, 0, settingsPayload)
	prefaceAndSettings := append([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"), settingsFrame...)

	done := make(chan struct{})

	var received []byte
	go func() {
		defer close(done)

		buf := make([]byte, 2048)
		n, _ := server.Read(buf)
		received = make([]byte, n)
		copy(received, buf[:n])
	}()

	_, err := conn.Write(prefaceAndSettings)
	require.NoError(t, err)
	<-done

	assert.NotEmpty(t, received)

	// Check for WINDOW_UPDATE frame (type 0x8) in wire bytes
	assert.True(t, bytes.Contains(received, []byte{0x00, 0x00, 0x04, 0x08}))
}

func TestH2FramedConn_BuildPriorityFrame_TooShort(t *testing.T) {
	t.Parallel()

	conn := &impl.FramedConn{}
	res := conn.BuildPriorityFrame([]byte{0x00, 0x01})
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

func TestFramedTransport_Constructor(t *testing.T) {
	t.Parallel()

	base := &http.Transport{}
	settings := Settings{HeaderTableSize: 4096}
	tr := NewFramedTransport(base, settings, "Host", "User-Agent")
	require.NotNil(t, tr)

	assert.Equal(t, settings, tr.settings)
	assert.Equal(t, []string{"Host", "User-Agent"}, tr.orderedKeys)
	assert.NotNil(t, tr.Transport)
}

func TestFramedTransport_Clone(t *testing.T) {
	t.Parallel()

	base1 := &http.Transport{}
	settings := Settings{HeaderTableSize: 65536, PriorityWeight: 255}
	tr1 := NewFramedTransport(base1, settings, "Host", "User-Agent")

	base2 := &http.Transport{}
	tr2 := tr1.Clone(base2)

	require.NotNil(t, tr2)
	assert.Equal(t, settings, tr2.settings)
	assert.Equal(t, []string{"Host", "User-Agent"}, tr2.orderedKeys)
	assert.Same(t, base2, tr2.Transport)
}

func TestFramedTransport_Constructor_NilBase(t *testing.T) {
	t.Parallel()

	settings := Settings{HeaderTableSize: 4096}
	tr := NewFramedTransport(nil, settings)
	require.NotNil(t, tr)
	assert.Nil(t, tr.Transport)

	assert.Nil(t, (*FramedTransport)(nil).Clone(&http.Transport{}))
}

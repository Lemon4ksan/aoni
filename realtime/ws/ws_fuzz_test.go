// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

type mockFuzzConn struct {
	readBuf  *bytes.Reader
	writeBuf *bytes.Buffer
}

func (m *mockFuzzConn) Read(b []byte) (int, error)         { return m.readBuf.Read(b) }
func (m *mockFuzzConn) Write(b []byte) (int, error)        { return m.writeBuf.Write(b) }
func (m *mockFuzzConn) Close() error                       { return nil }
func (m *mockFuzzConn) LocalAddr() net.Addr                { return &net.IPAddr{} }
func (m *mockFuzzConn) RemoteAddr() net.Addr               { return &net.IPAddr{} }
func (m *mockFuzzConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockFuzzConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockFuzzConn) SetWriteDeadline(t time.Time) error { return nil }

func FuzzWSFrameParse(f *testing.F) {
	// Seed 1: Unmasked Text frame "Hello"
	f.Add([]byte{0x81, 0x05, 'H', 'e', 'l', 'l', 'o'})
	// Seed 2: Masked Text frame "Hello" with mask 0x37 0xfa 0x21 0x3d
	f.Add([]byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58})
	// Seed 3: Fragmented text frame (FIN=0)
	f.Add([]byte{0x01, 0x03, 'h', 'e', 'l', 0x80, 0x02, 'l', 'o'})
	// Seed 4: Ping frame
	f.Add([]byte{0x89, 0x04, 'p', 'i', 'n', 'g'})
	// Seed 5: Close frame 1000
	f.Add([]byte{0x88, 0x02, 0x03, 0xe8})
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		mock := &mockFuzzConn{
			readBuf:  bytes.NewReader(data),
			writeBuf: bytes.NewBuffer(nil),
		}

		conn := ws.WrapRawConn(mock, false)
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err == nil {
			_ = len(payload)
		}

		buf := make([]byte, 512)
		_, _, _ = conn.ReadMessageTo(buf)
	})
}

func FuzzWSCloseMessage(f *testing.F) {
	f.Add(1000, "normal closure")
	f.Add(1001, "going away")
	f.Add(0, "")
	f.Add(9999, "custom code")

	f.Fuzz(func(t *testing.T, code int, reason string) {
		formatted := ws.FormatCloseMessage(code, reason)
		parsedCode, parsedReason := ws.ParseCloseMessage(formatted)
		if len(formatted) >= 2 {
			expectedCode := int(uint16(code))
			if parsedCode != expectedCode {
				t.Fatalf("close code mismatch: got %d, expected %d", parsedCode, expectedCode)
			}
			if parsedReason != reason {
				t.Fatalf("close reason mismatch: got %q, expected %q", parsedReason, reason)
			}
		}
	})
}

func FuzzWSMask(f *testing.F) {
	f.Add([]byte("hello websocket world"), byte(1), byte(2), byte(3), byte(4))
	f.Add([]byte(""), byte(0), byte(0), byte(0), byte(0))
	f.Add(make([]byte, 128), byte(0xff), byte(0xaa), byte(0x55), byte(0x00))

	f.Fuzz(func(t *testing.T, data []byte, m0, m1, m2, m3 byte) {
		mask := [4]byte{m0, m1, m2, m3}
		orig := make([]byte, len(data))
		copy(orig, data)

		ws.ApplyMask(data, mask)
		// Masking twice restores original bytes
		ws.ApplyMask(data, mask)

		if !bytes.Equal(data, orig) {
			t.Fatalf("XOR mask roundtrip failed")
		}
	})
}

func FuzzWSAcceptKey(f *testing.F) {
	f.Add("dGhlIHNhbXBsZSBub25jZQ==")
	f.Add("")
	f.Add("invalid_nonce")

	f.Fuzz(func(t *testing.T, key string) {
		res := ws.ComputeAcceptKey(key)
		if len(res) == 0 {
			t.Fatalf("expected non-empty accept key")
		}
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/lemon4ksan/aoni/realtime/socket"
)

func FuzzLengthPrefixedFramer(f *testing.F) {
	// Seed with 4-byte length + magic + payload
	f.Add([]byte{0x00, 0x00, 0x00, 0x04, 't', 'e', 's', 't'}, []byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x05, 'M', 'G', 'I', 'C', 'h', 'e', 'l', 'l', 'o'}, []byte{'M', 'G', 'I', 'C'})
	f.Add([]byte{}, []byte{})

	f.Fuzz(func(t *testing.T, data, magic []byte) {
		if len(magic) > 12 {
			return
		}

		framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{
			ByteOrder: binary.BigEndian,
			Magic:     magic,
			MaxLength: 64 * 1024,
		})

		r := bytes.NewReader(data)

		fb, err := framer.ReadFrame(r)
		if err == nil && fb != nil {
			var w bytes.Buffer

			_ = framer.WriteFrame(&w, fb.Bytes())
			socket.ReleaseFrameBuffer(fb)
		}

		pooled := socket.AcquireFrameBuffer(len(data))
		if pooled != nil {
			_ = pooled.Len()
			_ = pooled.Bytes()
			pooled.Reset()
			socket.ReleaseFrameBuffer(pooled)
		}
	})
}

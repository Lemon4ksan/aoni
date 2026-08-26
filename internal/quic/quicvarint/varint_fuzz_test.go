// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quicvarint_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func FuzzQUICVarint(f *testing.F) {
	f.Add([]byte{0x25})
	f.Add([]byte{0x7b, 0xbd})
	f.Add([]byte{0x9d, 0x7f, 0x3e, 0x7d})
	f.Add([]byte{0xc2, 0x19, 0x7c, 0x5e, 0xff, 0x14, 0xe8, 0x8c})
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		val, consumed, err := quicvarint.Parse(data)
		if err == nil {
			if consumed <= 0 || consumed > len(data) {
				t.Fatalf("invalid consumed length: %d", consumed)
			}

			// Verify Len and Append roundtrip
			expectedLen := quicvarint.Len(val)
			appended := quicvarint.Append(nil, val)
			if len(appended) != expectedLen {
				t.Fatalf("appended len %d != Len %d", len(appended), expectedLen)
			}

			val2, consumed2, err2 := quicvarint.Parse(appended)
			if err2 != nil || val2 != val || consumed2 != expectedLen {
				t.Fatalf("varint roundtrip mismatch: got %d, expected %d", val2, val)
			}

			// Read via ByteReader
			r := bytes.NewReader(data)
			val3, err3 := quicvarint.Read(r)
			if err3 == nil && val3 != val {
				t.Fatalf("Read vs Parse mismatch: got %d, expected %d", val3, val)
			}
		}
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"testing"

	"github.com/quic-go/qpack"
)

// FuzzQPACKDecode tests QPACK decoder robustness against arbitrary input bytes.
func FuzzQPACKDecode(f *testing.F) {
	seeds := [][]byte{
		{0x00, 0x00, 0xd8, 0xc1, 0xc0, 0x51, 0x0b, 'a', 'o', 'n', 'i', '-', 'h', '3', '-', 't', 'e', 's', 't'},
		{0x00, 0x00, 0x00, 0x00, 0xff},
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dec := qpack.NewDecoder()

		decodeFn := dec.Decode(data)
		for {
			_, err := decodeFn()
			if err != nil {
				break
			}
		}
	})
}

// FuzzH3FrameHeaderRead tests HTTP/3 varint frame header reading against arbitrary input bytes.
func FuzzH3FrameHeaderRead(f *testing.F) {
	seeds := [][]byte{
		{0x01, 0x04, 0x00, 0x00, 0x80, 0x01},
		{0x00, 0x0a, 't', 'e', 's', 't', 'b', 'o', 'd', 'y', '1', '2'},
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _, _ = ReadFrameHeader(r)
	})
}

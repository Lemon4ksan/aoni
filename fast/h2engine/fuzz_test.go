// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"bytes"
	"testing"
)

// FuzzHPACKDecode tests HPACK decoder robustness against arbitrary byte inputs.
func FuzzHPACKDecode(f *testing.F) {
	seeds := [][]byte{
		{
			0x82,
			0x86,
			0x84,
			0x41,
			0x0f,
			0x77,
			0x77,
			0x77,
			0x2e,
			0x65,
			0x78,
			0x61,
			0x6d,
			0x70,
			0x6c,
			0x65,
			0x2e,
			0x63,
			0x6f,
			0x6d,
		},
		{0x00, 0x00, 0x80, 0x01, 0xff, 0xff},
		{0x80, 0x81, 0x82, 0x83},
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		dec := AcquireHPACK()
		defer ReleaseHPACK(dec)

		hf := AcquireHeaderField()
		defer ReleaseHeaderField(hf)

		remaining := data
		for len(remaining) > 0 {
			var err error

			remaining, err = dec.Next(hf, remaining)
			if err != nil {
				break
			}

			hf.Reset()
		}
	})
}

// FuzzFrameRead tests H2 frame parser robustness against arbitrary wire bytes.
func FuzzFrameRead(f *testing.F) {
	seeds := [][]byte{
		{0x00, 0x00, 0x06, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x64},
		{0x00, 0x00, 0x04, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff},
		{0x00, 0x00, 0x05, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 'h', 'e', 'l', 'l', 'o'},
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		br := bufio.NewReader(bytes.NewReader(data))

		fr, err := ReadFrameFrom(br)
		if err == nil && fr != nil {
			ReleaseFrameHeader(fr)
		}
	})
}

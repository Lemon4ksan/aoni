// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

func BenchmarkFrameHeader_ReadFrame(b *testing.B) {
	raw := []byte{0x00, 0x00, 0x05, 0x01, 0x05, 0x00, 0x00, 0x00, 0x03, 'h', 'e', 'l', 'l', 'o'}
	br := bufio.NewReader(bytes.NewReader(raw))

	b.ReportAllocs()

	for b.Loop() {
		br.Reset(bytes.NewReader(raw))

		fr, err := h2engine.ReadFrameFrom(br)
		if err == nil {
			h2engine.ReleaseFrameHeader(fr)
		}
	}
}

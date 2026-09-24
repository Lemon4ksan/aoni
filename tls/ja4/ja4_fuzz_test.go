// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/tls/ja4"
)

func FuzzParseExtensionsFromRaw(f *testing.F) {
	f.Add(
		[]byte(
			"\x16\x03\x01\x00\x30\x01\x00\x00\x2c\x03\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x13\x01\x01\x00\x00\x00",
		),
	)
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ja4.ParseExtensionsFromRaw(data)
	})
}

func FuzzComputeJA4(f *testing.F) {
	f.Add(true, "h2", uint16(0x1301), uint16(0x0000))

	f.Fuzz(func(t *testing.T, sni bool, alpn string, cipher, ext uint16) {
		res := ja4.ComputeJA4(
			[]uint16{cipher},
			[]uint16{ext},
			[]uint16{0x0304},
			sni,
			[]string{alpn},
			[]uint16{0x0403},
		)
		if len(res) < 30 {
			t.Fatalf("JA4 fingerprint too short: %q", res)
		}
	})
}

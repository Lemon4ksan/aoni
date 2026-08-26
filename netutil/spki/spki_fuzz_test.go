// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spki_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/spki"
)

func FuzzSPKI(f *testing.F) {
	f.Add([]byte("mock SPKI byte sequence"), `pin-sha256="47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="`)
	f.Add([]byte{}, `pin-sha256=""`)
	f.Add([]byte{0x30, 0x82, 0x01, 0x22, 0x30, 0x0d, 0x06, 0x09}, "unquoted-pin-hash")

	f.Fuzz(func(t *testing.T, rawSPKI []byte, rawPin string) {
		fp := spki.ComputeSPKIFingerprintFromSPKI(rawSPKI)
		_ = fp

		norm := spki.NormalizePin(rawPin)
		_ = norm

		_, _ = spki.ComputeSPKIFingerprintFromDER(rawSPKI)
	})
}

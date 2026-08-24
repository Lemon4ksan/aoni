// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handshake

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

var tls13CipherSuites = []uint16{
	tls.TLS_AES_128_GCM_SHA256,
	tls.TLS_AES_256_GCM_SHA384,
	tls.TLS_CHACHA20_POLY1305_SHA256,
}

func getHashForCipherSuite(id uint16) crypto.Hash {
	switch id {
	case tls.TLS_AES_128_GCM_SHA256, tls.TLS_CHACHA20_POLY1305_SHA256:
		return crypto.SHA256
	case tls.TLS_AES_256_GCM_SHA384:
		return crypto.SHA384
	default:
		return crypto.SHA256
	}
}

func TestHKDF(t *testing.T) {
	for _, id := range tls13CipherSuites {
		t.Run(tls.CipherSuiteName(id), func(t *testing.T) {
			h := getHashForCipherSuite(id)
			secret := []byte("foobar")
			expanded := hkdfExpandLabel(h, secret, nil, "traffic upd", h.Size())
			require.NotEmpty(t, expanded)
			require.Equal(t, h.Size(), len(expanded))
		})
	}
}

func BenchmarkHKDFExpandLabel(b *testing.B) {
	for _, id := range tls13CipherSuites {
		h := getHashForCipherSuite(id)
		b.Run(tls.CipherSuiteName(id), func(b *testing.B) {
			b.ReportAllocs()

			secret := make([]byte, 32)
			_, _ = rand.Read(secret)

			for b.Loop() {
				hkdfExpandLabel(h, secret, nil, "traffic upd", h.Size())
			}
		})
	}
}

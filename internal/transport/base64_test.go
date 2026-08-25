// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/aoni/internal/transport"
)

func TestBase64EncodeURL(t *testing.T) {
	t.Parallel()

	testData := [][]byte{
		[]byte(""),
		[]byte("f"),
		[]byte("fo"),
		[]byte("foo"),
		[]byte("foob"),
		[]byte("fooba"),
		[]byte("foobar"),
		[]byte("https://example.com/api/v1/auth?code=1234567890"),
	}

	for _, data := range testData {
		expected := base64.RawURLEncoding.EncodeToString(data)
		dst := make([]byte, transport.Base64URLEncodedLen(len(data)))
		n := transport.Base64EncodeURL(data, dst)
		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(dst[:n]))
	}
}

func BenchmarkBase64EncodeURL_SHA256(b *testing.B) {
	sum := sha256.Sum256([]byte("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	dst := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = transport.Base64EncodeURL(sum[:], dst)
	}
}

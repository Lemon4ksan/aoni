// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/transport"
)

func createTestAEAD(t *testing.T) (cipher.AEAD, []byte) {
	key := make([]byte, 32)
	iv := make([]byte, 12)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)

	block, err := aes.NewCipher(key)
	require.NoError(t, err)

	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)

	return aead, iv
}

func TestRecordFramer_EndToEnd(t *testing.T) {
	t.Parallel()

	aead, iv := createTestAEAD(t)
	framer := transport.NewRecordFramer()

	scope := borrow.NewScope()
	defer scope.Release()

	payload := []byte("GET /stream HTTP/1.1\r\nHost: example.com\r\n\r\n")

	var buf bytes.Buffer

	err := framer.WriteRecordScoped(&buf, aead, iv, 0, transport.RecordTypeApplicationData, payload, scope)
	require.NoError(t, err)

	decrypted, innerType, err := framer.ReadRecordScoped(&buf, aead, iv, 0, scope)
	require.NoError(t, err)
	assert.Equal(t, transport.RecordTypeApplicationData, innerType)
	assert.Equal(t, string(payload), string(decrypted))
}

func BenchmarkTLS13_InPlaceDecryption(b *testing.B) {
	key := make([]byte, 32)
	iv := make([]byte, 12)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)

	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)

	framer := transport.NewRecordFramer()

	scope := borrow.NewScope()
	defer scope.Release()

	payload := make([]byte, 4096)
	_, _ = rand.Read(payload)

	var recordBuf bytes.Buffer

	_ = framer.WriteRecordScoped(&recordBuf, aead, iv, 0, transport.RecordTypeApplicationData, payload, scope)
	wireBytes := recordBuf.Bytes()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	reader := bytes.NewReader(wireBytes)

	for b.Loop() {
		reader.Reset(wireBytes)

		s := borrow.AcquireScope()
		_, _, _ = framer.ReadRecordScoped(reader, aead, iv, 0, s)
		s.Release()
	}
}

func BenchmarkTLS13_StandardCopyDecryption(b *testing.B) {
	key := make([]byte, 32)
	iv := make([]byte, 12)
	_, _ = rand.Read(key)
	_, _ = rand.Read(iv)

	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)

	framer := transport.NewRecordFramer()

	scope := borrow.NewScope()
	defer scope.Release()

	payload := make([]byte, 4096)
	_, _ = rand.Read(payload)

	var recordBuf bytes.Buffer

	_ = framer.WriteRecordScoped(&recordBuf, aead, iv, 0, transport.RecordTypeApplicationData, payload, scope)
	wireBytes := recordBuf.Bytes()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	reader := bytes.NewReader(wireBytes)

	for b.Loop() {
		reader.Reset(wireBytes)

		// 1. Read header
		var hdr [5]byte

		_, _ = io.ReadFull(reader, hdr[:])

		// 2. Allocate heap buffer for ciphertext
		cipherBuf := make([]byte, len(wireBytes)-5)
		_, _ = io.ReadFull(reader, cipherBuf)

		// 3. Decrypt into a second heap buffer
		var nonce [12]byte
		transport.ComputeNonceXOR(iv, 0, &nonce)
		plainBuf, _ := aead.Open(nil, nonce[:], cipherBuf, hdr[:])

		// 4. Copy into destination application slice (standard tls.Conn.Read behavior)
		appBuf := make([]byte, len(plainBuf)-1)
		copy(appBuf, plainBuf[:len(plainBuf)-1])
	}
}

func BenchmarkComputeNonceXOR(b *testing.B) {
	iv := make([]byte, 12)
	var nonce [12]byte
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		transport.ComputeNonceXOR(iv, uint64(i), &nonce)
	}
}


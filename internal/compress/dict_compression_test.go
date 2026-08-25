// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compress_test

import (
	"bytes"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
	kzstd "github.com/klauspost/compress/zstd"

	"github.com/lemon4ksan/aoni/internal/compress"
)

func TestDCZ_Decompression(t *testing.T) {
	dictPayload := []byte(`{"version":"1.0","api_schema":{"user":{"id":0,"name":"","email":""}}}`)
	dictHash := sha256.Sum256(dictPayload)

	originalData := []byte(`{"version":"1.0","api_schema":` +
		`{"user":{"id":12345,"name":"Alice","email":"alice@example.com"}}}`)

	// Compress with klauspost/compress/zstd using raw dictionary
	enc, err := kzstd.NewWriter(nil, kzstd.WithEncoderDictRaw(1, dictPayload))
	if err != nil {
		t.Fatal(err)
	}

	defer enc.Close()

	compressedZstd := enc.EncodeAll(originalData, nil)

	// Wrap with RFC 9842 §5 fixed 40-byte header
	dczStream := compress.WrapDCZHeader(compressedZstd, dictHash)

	t.Run("UnzstdDCZ", func(t *testing.T) {
		decompressed, err := compress.UnzstdDCZ(dczStream, nil, dictPayload)
		if err != nil {
			t.Fatalf("UnzstdDCZ failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("DecompressWithDict", func(t *testing.T) {
		decompressed, err := compress.DecompressWithDict("dcz", dczStream, nil, dictPayload)
		if err != nil {
			t.Fatalf("DecompressWithDict(dcz) failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("NewDCZReader streaming", func(t *testing.T) {
		reader, err := compress.NewDictionaryReader("dcz", bytes.NewReader(dczStream), dictPayload)
		if err != nil {
			t.Fatalf("NewDictionaryReader failed: %v", err)
		}

		defer reader.Close()

		decompressed, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("streaming read failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("SHA-256 mismatch rejection", func(t *testing.T) {
		wrongDict := []byte("different dictionary contents")

		_, err := compress.UnzstdDCZ(dczStream, nil, wrongDict)
		if err == nil {
			t.Fatal("expected error on dictionary hash mismatch, got nil")
		}
	})

	t.Run("Corrupt header rejection", func(t *testing.T) {
		corrupt := make([]byte, len(dczStream))
		copy(corrupt, dczStream)
		corrupt[0] = 0x00 // corrupt magic

		_, err := compress.UnzstdDCZ(corrupt, nil, dictPayload)
		if err == nil {
			t.Fatal("expected error on corrupt magic, got nil")
		}
	})
}

func TestDCB_Decompression(t *testing.T) {
	dictPayload := []byte(`common template html header and navigation bar elements`)
	dictHash := sha256.Sum256(dictPayload)

	originalData := []byte(`common template html header and navigation bar elements - page content here`)

	var buf bytes.Buffer

	bw := brotli.NewWriter(&buf)
	if _, err := bw.Write(originalData); err != nil {
		t.Fatal(err)
	}

	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}

	dcbStream := compress.WrapDCBHeader(buf.Bytes(), dictHash)

	t.Run("UnbrotliDCB", func(t *testing.T) {
		decompressed, err := compress.UnbrotliDCB(dcbStream, nil, dictPayload)
		if err != nil {
			t.Fatalf("UnbrotliDCB failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("DecompressWithDict", func(t *testing.T) {
		decompressed, err := compress.DecompressWithDict("dcb", dcbStream, nil, dictPayload)
		if err != nil {
			t.Fatalf("DecompressWithDict(dcb) failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("NewDCBReader streaming", func(t *testing.T) {
		reader, err := compress.NewDictionaryReader("dcb", bytes.NewReader(dcbStream), dictPayload)
		if err != nil {
			t.Fatalf("NewDictionaryReader failed: %v", err)
		}

		defer reader.Close()

		decompressed, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("streaming read failed: %v", err)
		}

		if !bytes.Equal(decompressed, originalData) {
			t.Fatalf("mismatch: got %q, want %q", decompressed, originalData)
		}
	})

	t.Run("SHA-256 mismatch rejection", func(t *testing.T) {
		wrongDict := []byte("wrong dictionary")

		_, err := compress.UnbrotliDCB(dcbStream, nil, wrongDict)
		if err == nil {
			t.Fatal("expected error on hash mismatch, got nil")
		}
	})

	t.Run("Corrupt header rejection", func(t *testing.T) {
		corrupt := make([]byte, len(dcbStream))
		copy(corrupt, dcbStream)
		corrupt[0] = 0x00

		_, err := compress.UnbrotliDCB(corrupt, nil, dictPayload)
		if err == nil {
			t.Fatal("expected error on corrupt magic, got nil")
		}
	})
}

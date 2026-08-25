// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compress_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/lemon4ksan/aoni/internal/compress"
)

func TestDCZ_Decompression(t *testing.T) {
	dictPayload := []byte(`{"version":"1.0","api_schema":{"user":{"id":0,"name":"","email":""}}}`)

	originalData := []byte(`{"version":"1.0","api_schema":` +
		`{"user":{"id":12345,"name":"Alice","email":"alice@example.com"}}}`)

	// RFC 9842 §5 fixed 40-byte header + Zstandard compressed payload with raw dictionary
	dczStream := []byte{
		0x5e, 0x2a, 0x4d, 0x18, 0x20, 0x0, 0x0, 0x0, 0xf9, 0x51, 0x8c, 0x2, 0x40, 0xd7, 0x26, 0xec,
		0x1e, 0xfb, 0x26, 0xd2, 0x5, 0x82, 0x5d, 0x91, 0xe7, 0x15, 0xad, 0xbc, 0x52, 0xee, 0x48, 0x2a,
		0x4f, 0xc5, 0x36, 0x3, 0x43, 0xc9, 0x13, 0x24, 0x28, 0xb5, 0x2f, 0xfd, 0x5, 0x0, 0x1, 0x5d,
		0x1, 0x0, 0xf4, 0x1, 0x31, 0x32, 0x33, 0x34, 0x35, 0x41, 0x6c, 0x69, 0x63, 0x65, 0x61, 0x6c,
		0x69, 0x63, 0x65, 0x40, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x2e, 0x63, 0x6f, 0x6d, 0x22,
		0x7d, 0x7d, 0x7d, 0x3, 0x10, 0x6, 0x91, 0xd1, 0x4c, 0xa1, 0x21, 0x66, 0x10, 0xe0, 0x73, 0x52,
		0x57,
	}

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

	originalData := []byte(`common template html header and navigation bar elements - page content here`)

	// RFC 9842 §4 36-byte DCB header + Brotli compressed stream
	dcbStream := []byte{
		0xff, 0x44, 0x43, 0x42, 0x79, 0x5b, 0xe, 0xaf, 0xad, 0x8b, 0x1f, 0x2b, 0xa2, 0x1a, 0xa7, 0xe,
		0x3a, 0x30, 0xb8, 0xa3, 0x99, 0x56, 0x6e, 0x3d, 0xf1, 0xd8, 0xc3, 0x34, 0xbf, 0xa4, 0xe0, 0x8d,
		0x20, 0xde, 0xd5, 0xfe, 0x1b, 0x4a, 0x0, 0x0, 0x4, 0x72, 0x6b, 0xa9, 0x5e, 0x28, 0xd6, 0xa0,
		0x30, 0xd4, 0xe2, 0x3f, 0x73, 0x8c, 0x3, 0x97, 0x69, 0x59, 0x7c, 0x81, 0x1b, 0x70, 0xc2, 0x3d,
		0xc6, 0xc6, 0xb6, 0x38, 0x36, 0x2, 0x10, 0xe9, 0x1, 0x62, 0x3c, 0xe1, 0xaf, 0x7c, 0x9a, 0xb0,
		0xba, 0xc4, 0x63, 0xdf, 0x51, 0x69, 0xf4, 0x1f, 0xc1, 0xcf, 0xd6, 0xe2, 0x98, 0x19, 0x59,
	}

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

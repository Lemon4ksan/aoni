// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package header_test

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni/internal/header"
)

func TestFastHeader(t *testing.T) {
	var fh header.FastHeader

	fh.SetString("User-Agent", "aoni/1.0")
	fh.SetString("Accept", "application/json")
	fh.SetString("Host", "example.com")

	if fh.Len() != 3 {
		t.Fatalf("expected len 3, got %d", fh.Len())
	}

	val, ok := fh.GetString("user-agent")
	if !ok || val != "aoni/1.0" {
		t.Fatalf("expected 'aoni/1.0', got %q (ok=%v)", val, ok)
	}

	fh.SetString("User-Agent", "aoni/2.0")

	val, _ = fh.GetString("User-Agent")
	if val != "aoni/2.0" {
		t.Fatalf("expected updated 'aoni/2.0', got %q", val)
	}

	fh.Del([]byte("Accept"))

	if fh.Len() != 2 {
		t.Fatalf("expected len 2 after Del, got %d", fh.Len())
	}

	fh.Reset()

	if fh.Len() != 0 {
		t.Fatalf("expected len 0 after Reset, got %d", fh.Len())
	}
}

func BenchmarkFastHeader_SetGet(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var fh header.FastHeader
		fh.SetString("User-Agent", "aoni/1.0")
		fh.SetString("Accept", "application/json")
		fh.SetString("Content-Type", "application/json")
		fh.SetString("Host", "example.com")
		fh.SetString("Authorization", "Bearer token123")

		_, _ = fh.GetString("Authorization")
	}
}

func BenchmarkStandardHeader_SetGet(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		h := make(http.Header)
		h.Set("User-Agent", "aoni/1.0")
		h.Set("Accept", "application/json")
		h.Set("Content-Type", "application/json")
		h.Set("Host", "example.com")
		h.Set("Authorization", "Bearer token123")

		_ = h.Get("Authorization")
	}
}

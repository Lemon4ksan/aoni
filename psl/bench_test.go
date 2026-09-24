// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package psl_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/psl"
)

func BenchmarkPublicSuffix_String(b *testing.B) {
	domains := []string{
		"amazon.co.uk",
		"maps.google.com",
		"foo.bar.golang.org",
		"foo.dyndns.org",
		"city.kobe.jp",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for _, d := range domains {
			_, _ = psl.PublicSuffix(d)
		}
	}
}

func BenchmarkPublicSuffix_Bytes(b *testing.B) {
	domains := [][]byte{
		[]byte("amazon.co.uk"),
		[]byte("maps.google.com"),
		[]byte("foo.bar.golang.org"),
		[]byte("foo.dyndns.org"),
		[]byte("city.kobe.jp"),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for _, d := range domains {
			_, _ = psl.PublicSuffixBytes(d)
		}
	}
}

func BenchmarkEffectiveTLDPlusOne_Bytes(b *testing.B) {
	domain := []byte("www.books.amazon.co.uk")

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = psl.EffectiveTLDPlusOneBytes(domain)
	}
}

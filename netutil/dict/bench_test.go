// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict_test

import (
	"net/url"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/dict"
)

func BenchmarkStore_Match(b *testing.B) {
	store := dict.NewStore()
	baseURL, _ := url.Parse("https://api.example.com/dict")
	targetURL, _ := url.Parse("https://api.example.com/v1/users/profile/details")

	_, _ = store.Store(baseURL, `match="/v1/users/*", id="users-v1"`, []byte("user dictionary payload"))

	b.ReportAllocs()

	for b.Loop() {
		d, ok := store.Match(targetURL, "")
		if !ok || d == nil {
			b.Fatal("match failed")
		}
	}
}

func BenchmarkFormatAvailableDictionary(b *testing.B) {
	hash := dict.ComputeSHA256([]byte("benchmark sample dictionary"))

	b.ReportAllocs()

	for b.Loop() {
		s := dict.FormatAvailableDictionary(hash)
		if len(s) == 0 {
			b.Fatal("formatting failed")
		}
	}
}

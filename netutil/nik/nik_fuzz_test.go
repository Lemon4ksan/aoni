// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nik_test

import (
	"context"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/nik"
)

func FuzzNetworkIsolationKey(f *testing.F) {
	f.Add("https://example.com", "https://sub.example.com")
	f.Add("http://localhost:8080", "http://localhost:8080")
	f.Add("example.com", "")
	f.Add("", "")
	f.Add("not-a-valid-url-format:9999999", "https://another.domain.com:443")

	f.Fuzz(func(t *testing.T, top, frame string) {
		k := nik.New(top, frame)
		_ = k.TopFrameSite()
		_ = k.FrameSite()
		_ = k.IsCrossSite()
		_ = k.IsEmpty()
		_ = k.IsTransient()
		_ = k.KeyString()
		_ = k.String()

		ctx := nik.WithNIK(context.Background(), k)
		extracted, ok := nik.FromContext(ctx)
		if ok && extracted.IsEmpty() {
			t.Fatalf("extracted NIK reported ok but is empty")
		}

		sameSiteKey := nik.NewSameSite(top)
		_ = sameSiteKey.KeyString()

		transientKey := nik.NewTransient()
		if !transientKey.IsTransient() {
			t.Fatalf("expected transient NIK")
		}
	})
}

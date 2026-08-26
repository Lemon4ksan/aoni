// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package etag_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/resiliency/etag"
)

func FuzzETag(f *testing.F) {
	f.Add(`"xyzzy"`, `W/"xyzzy"`)
	f.Add(`"12345"`, `"12345"`)
	f.Add(`W/"12345"`, `W/"12345"`)
	f.Add("", "")
	f.Add(`*`, `"anything"`)
	f.Add(`"unclosed`, `W/`)

	f.Fuzz(func(t *testing.T, a, b string) {
		_ = etag.StrongMatch(a, b)
		_ = etag.WeakMatch(a, b)
		_ = etag.IsWeak(a)
		_ = etag.IsWeak(b)
		_ = etag.Normalize(a)
		_ = etag.Normalize(b)
	})
}

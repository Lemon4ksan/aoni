// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hints_test

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/hints"
)

func FuzzParseLinkHeader(f *testing.F) {
	f.Add("<https://example.com/asset.js>; rel=\"preload\"; as=\"script\"; crossorigin=\"anonymous\"")
	f.Add("</style.css>; rel=stylesheet, </font.woff2>; rel=preload; as=font")
	f.Add("<https://api.example.com>; rel=preconnect")
	f.Add("")
	f.Add("<>; rel=\"\"")
	f.Add("not a link header")

	f.Fuzz(func(t *testing.T, raw string) {
		links := hints.ParseLinkHeader(raw)
		for _, l := range links {
			_ = l.URI
			_ = l.Rel
			_ = l.As
			_ = l.Crossorigin
		}

		h := make(http.Header)
		h.Add("Link", raw)
		fromH := hints.ParseLinksFromHeaders(h)
		_ = len(fromH)
	})
}

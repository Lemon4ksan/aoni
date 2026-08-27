// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict_test

import (
	"net/url"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/dict"
)

func FuzzParseUseAsDictionary(f *testing.F) {
	f.Add(
		"match=\"/api/*\", match-dest=(\"document\" \"script\"), id=\"v1\", type=raw, ttl=3600",
		"https://example.com/base",
	)
	f.Add("match=\"/*\", raw", "https://example.com/")
	f.Add("", "")
	f.Add("invalid=format; random", "http://localhost:8080/")
	f.Add("match=\"/assets/*\", ttl=99999999999999999999999999", "https://sub.example.com/assets/app.js")

	f.Fuzz(func(t *testing.T, header, rawURL string) {
		u, _ := url.Parse(rawURL)

		meta, err := dict.ParseUseAsDictionary(header, u)
		if err == nil && meta != nil {
			_ = meta.Match
			_ = meta.Type
			_ = meta.ID
			_ = meta.TTL
			_ = meta.MatchDest
		}
	})
}

func FuzzParseAvailableDictionary(f *testing.F) {
	f.Add(":47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:")
	f.Add("::")
	f.Add("")
	f.Add(":invalid_base64_content!@#$:")
	f.Add(":AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")

	f.Fuzz(func(t *testing.T, header string) {
		hash, err := dict.ParseAvailableDictionary(header)
		if err == nil {
			formatted := dict.FormatAvailableDictionary(hash)
			if len(formatted) == 0 {
				t.Fatalf("expected non-empty formatted dictionary for valid hash")
			}
		}
	})
}

func FuzzMatchURLPattern(f *testing.F) {
	f.Add("/api/*", "https://example.com/base", "https://example.com/api/v1/users")
	f.Add("*.js", "https://example.com/static/", "https://example.com/static/bundle.js")
	f.Add("", "", "")
	f.Add("https://example.com/api/*", "https://example.com/", "https://example.com/api/test")

	f.Fuzz(func(t *testing.T, pattern, rawBaseURL, rawTargetURL string) {
		baseU, _ := url.Parse(rawBaseURL)

		targetU, err := url.Parse(rawTargetURL)
		if err != nil || targetU == nil {
			return
		}

		_ = dict.MatchURLPattern(pattern, baseU, targetU)
	})
}

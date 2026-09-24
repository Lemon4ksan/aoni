// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/cookie"
)

func FuzzPathMatch(f *testing.F) {
	f.Add("/api/v1/users", "/api")
	f.Add("/api/v1/users", "/api/")
	f.Add("/api", "/api")
	f.Add("/", "/")
	f.Add("/auth", "/api")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, reqPath, cookiePath string) {
		_ = cookie.PathMatch(reqPath, cookiePath)
	})
}

func FuzzParseSetCookieHeader(f *testing.F) {
	f.Add(
		"session_id=xyz123; Domain=example.com; Path=/api; Secure; HttpOnly; SameSite=Lax; Max-Age=3600",
		"example.com",
		"/",
	)
	f.Add("name=value", "domain.com", "/")
	f.Add("", "", "")
	f.Add("__Secure-ID=123; Secure; SameSite=None", "sub.example.com", "/auth")

	f.Fuzz(func(t *testing.T, headerVal, defaultDomain, defaultPath string) {
		c := cookie.ParseSetCookieHeader(headerVal, defaultDomain, defaultPath)
		_ = cookie.ValidatePrefix(c)
	})
}

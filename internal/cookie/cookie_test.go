// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalCookie "github.com/lemon4ksan/aoni/internal/cookie"
)

func TestInternalCookie_ParseSetCookieHeader(t *testing.T) {
	header := "session_id=xyz123; Domain=example.com; Path=/api; Secure; HttpOnly; SameSite=Lax; Max-Age=3600"
	c := internalCookie.ParseSetCookieHeader(header, "default.com", "/")

	assert.Equal(t, "session_id", c.Name)
	assert.Equal(t, "xyz123", c.Value)
	assert.Equal(t, "example.com", c.Domain)
	assert.Equal(t, "/api", c.Path)
	assert.True(t, c.Secure)
	assert.True(t, c.HTTPOnly)
	assert.Equal(t, "Lax", c.SameSite)
	assert.Equal(t, 3600, c.MaxAge)
}

func TestInternalCookie_PathMatch(t *testing.T) {
	assert.True(t, internalCookie.PathMatch("/api/v1", "/api"))
	assert.False(t, internalCookie.PathMatch("/apiv1", "/api"))
	assert.True(t, internalCookie.PathMatch("/api", "/api"))
}

func TestInternalCookie_BuildCookieHeader(t *testing.T) {
	cookies := []*http.Cookie{
		{Name: "c1", Value: "v1", Path: "/"},
		{Name: "c2", Value: "v2", Path: "/api"},
	}

	hdr := internalCookie.BuildCookieHeader(cookies)
	assert.Equal(t, "c2=v2; c1=v1", hdr)
}

func TestInternalCookie_ExportNetscape(t *testing.T) {
	cookies := []*http.Cookie{
		{Name: "session", Value: "123", Domain: ".example.com", Path: "/api", Secure: true},
	}

	netscape := internalCookie.ExportNetscape(cookies, "example.com")
	assert.Contains(t, netscape, "# Netscape HTTP Cookie File")
	assert.Contains(t, netscape, ".example.com\tTRUE\t/api\tTRUE\t0\tsession\t123")
	assert.Equal(t, "", internalCookie.ExportNetscape(nil, ""))
}

func TestInternalCookie_ParseSingleCookie(t *testing.T) {
	ck := internalCookie.ParseSingleCookie([]byte("foo"), []byte("foo=bar; Path=/"))
	require.NotNil(t, ck)
	assert.Equal(t, "foo", ck.Name)
	assert.Equal(t, "bar", ck.Value)

	emptyCk := internalCookie.ParseSingleCookie(nil, []byte(""))
	assert.Nil(t, emptyCk)
}

func TestInternalCookie_EmptyParse(t *testing.T) {
	c := internalCookie.ParseSetCookieHeader("", "domain.com", "/")
	assert.Equal(t, "", c.Name)
}

func TestInternalCookie_RFC6265bis_Limits(t *testing.T) {
	// CTL character rejection (RFC 6265bis §5.5 step 1 & §5.7 step 3)
	badHeader := "name=val\x00ue; Domain=example.com"
	c := internalCookie.ParseSetCookieHeader(badHeader, "example.com", "/")
	assert.Equal(t, "", c.Name)

	// Max-Age 400 days limit clamping (RFC 6265bis §5.5 & §5.6.2 step 6)
	hdrMaxAge := "name=value; Max-Age=50000000; SameSite=strict"
	c = internalCookie.ParseSetCookieHeader(hdrMaxAge, "example.com", "/")
	assert.Equal(t, "name", c.Name)
	assert.Equal(t, internalCookie.MaxCookieAgeSeconds, c.MaxAge)
	assert.Equal(t, "Strict", c.SameSite)

	// SameSite variations
	cNone := internalCookie.ParseSetCookieHeader("n=v; SameSite=none", "example.com", "/")
	assert.Equal(t, "None", cNone.SameSite)

	cInvalid := internalCookie.ParseSetCookieHeader("n=v; SameSite=invalid_value", "example.com", "/")
	assert.Equal(t, "Default", cInvalid.SameSite)
}

func TestInternalCookie_RFC6265bis_Prefixes(t *testing.T) {
	// __Secure- prefix (RFC 6265bis §4.1.3.1, §5.4 & §5.7 step 20)
	assert.True(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Secure-SID",
		Value:  "12345",
		Secure: true,
	}))
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Secure-SID",
		Value:  "12345",
		Secure: false,
	}))
	// Case-insensitive check (§5.4)
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__secure-token",
		Value:  "12345",
		Secure: false,
	}))

	// __Host- prefix (RFC 6265bis §4.1.3.2, §5.4 & §5.7 step 21)
	assert.True(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Host-SID",
		Value:  "12345",
		Secure: true,
		Path:   "/",
		Domain: "",
	}))
	// Fails if Secure=false
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Host-SID",
		Value:  "12345",
		Secure: false,
		Path:   "/",
		Domain: "",
	}))
	// Fails if Path != "/"
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Host-SID",
		Value:  "12345",
		Secure: true,
		Path:   "/api",
		Domain: "",
	}))
	// Fails if Domain is set (not host-only)
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:   "__Host-SID",
		Value:  "12345",
		Secure: true,
		Path:   "/",
		Domain: "example.com",
	}))

	// Nameless cookies with prefix values (RFC 6265bis §5.7 step 22)
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:  "",
		Value: "__Secure-bad",
	}))
	assert.False(t, internalCookie.ValidatePrefix(internalCookie.Cookie{
		Name:  "",
		Value: "__Host-bad",
	}))
}

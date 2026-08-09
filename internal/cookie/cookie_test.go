// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

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

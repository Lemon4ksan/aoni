// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package urlutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/urlutil"
)

func TestParse(t *testing.T) {
	u1, err := urlutil.Parse("https://api.example.com/v1/resource?query=1")
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", u1.Host)

	u2, err := urlutil.Parse("https://api.example.com/v1/resource?query=1")
	require.NoError(t, err)
	assert.Equal(t, u1, u2)
}

func TestReplaceVar(t *testing.T) {
	res := urlutil.ReplaceVar("/users/{id}/profile", "id", "42")
	assert.Equal(t, "/users/42/profile", res)

	noMatch := urlutil.ReplaceVar("/users/profile", "id", "42")
	assert.Equal(t, "/users/profile", noMatch)
}

func TestFastAppendQuery(t *testing.T) {
	res1 := urlutil.FastAppendQuery("https://example.com/api", "page", "1")
	assert.Equal(t, "https://example.com/api?page=1", res1)

	res2 := urlutil.FastAppendQuery("https://example.com/api?limit=10", "page", "2")
	assert.Equal(t, "https://example.com/api?limit=10&page=2", res2)
}

func TestCloneURL(t *testing.T) {
	assert.Nil(t, urlutil.CloneURL(nil))

	u1, _ := urlutil.Parse("https://user:pass@example.com:8443/path")
	cloned := urlutil.CloneURL(u1)
	require.NotNil(t, cloned)
	assert.Equal(t, u1.String(), cloned.String())
	assert.NotSame(t, u1, cloned)
	assert.NotSame(t, u1.User, cloned.User)
}

func TestMatchDomainPattern(t *testing.T) {
	assert.True(t, urlutil.MatchDomainPattern("api.example.com", "*.example.com"))
	assert.True(t, urlutil.MatchDomainPattern("example.com", "*.example.com"))
	assert.True(t, urlutil.MatchDomainPattern("example.com", "example.com"))
	assert.False(t, urlutil.MatchDomainPattern("other.com", "*.example.com"))
}

func TestIsCrossOrigin(t *testing.T) {
	u1, _ := urlutil.Parse("https://example.com/a")
	u2, _ := urlutil.Parse("https://example.com/b")
	uDiffScheme, _ := urlutil.Parse("http://example.com/a")
	uDiffDomain, _ := urlutil.Parse("https://other.com/a")
	uDiffPort, _ := urlutil.Parse("https://example.com:8443/a")

	assert.False(t, urlutil.IsCrossOrigin(u1, u2))
	assert.True(t, urlutil.IsCrossOrigin(u1, uDiffScheme))
	assert.True(t, urlutil.IsCrossOrigin(u1, uDiffDomain))
	assert.True(t, urlutil.IsCrossOrigin(u1, uDiffPort))
	assert.False(t, urlutil.IsCrossOrigin(nil, u1))
}

func TestCanonicalPort(t *testing.T) {
	uHTTP, _ := urlutil.Parse("http://example.com")
	uHTTPS, _ := urlutil.Parse("https://example.com")
	uCustom, _ := urlutil.Parse("https://example.com:9000")

	assert.Equal(t, "80", urlutil.CanonicalPort(uHTTP))
	assert.Equal(t, "443", urlutil.CanonicalPort(uHTTPS))
	assert.Equal(t, "9000", urlutil.CanonicalPort(uCustom))
	assert.Equal(t, "", urlutil.CanonicalPort(nil))
}

func TestIsSameDomainOrSubdomain(t *testing.T) {
	assert.True(t, urlutil.IsSameDomainOrSubdomain("api.example.com", "example.com"))
	assert.True(t, urlutil.IsSameDomainOrSubdomain("example.com", "api.example.com"))
	assert.True(t, urlutil.IsSameDomainOrSubdomain("example.com", "example.com"))
	assert.False(t, urlutil.IsSameDomainOrSubdomain("other.com", "example.com"))
}

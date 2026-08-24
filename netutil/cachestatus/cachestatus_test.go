// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cachestatus_test

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/cachestatus"
)

func TestCacheStatus_RFC9211_Examples(t *testing.T) {
	t.Parallel()

	// Example 1: Minimal cache hit (RFC 9211 §3)
	chain1, err1 := cachestatus.Parse("ExampleCache; hit")
	require.NoError(t, err1)
	require.Len(t, chain1, 1)
	assert.Equal(t, "ExampleCache", chain1[0].CacheName)
	assert.True(t, chain1[0].IsHit())
	assert.False(t, chain1[0].IsMiss())

	// Example 2: Hit with TTL freshness (RFC 9211 §3)
	chain2, err2 := cachestatus.Parse("OriginCache; hit; ttl=1100")
	require.NoError(t, err2)
	require.Len(t, chain2, 1)
	assert.Equal(t, "OriginCache", chain2[0].CacheName)
	assert.True(t, chain2[0].IsHit())
	assert.Equal(t, 1100, chain2[0].TTL)
	assert.False(t, chain2[0].IsStale())

	// Example 3: Stale hit with negative freshness & quoted string identifier (RFC 9211 §3)
	chain3, err3 := cachestatus.Parse(`"Example Cache"; hit; ttl=-42`)
	require.NoError(t, err3)
	require.Len(t, chain3, 1)
	assert.Equal(t, "Example Cache", chain3[0].CacheName)
	assert.True(t, chain3[0].IsHit())
	assert.Equal(t, -42, chain3[0].TTL)
	assert.True(t, chain3[0].IsStale())

	// Example 4: Complete miss (RFC 9211 §3)
	chain4, err4 := cachestatus.Parse("ExampleCache; fwd=uri-miss")
	require.NoError(t, err4)
	require.Len(t, chain4, 1)
	assert.Equal(t, "ExampleCache", chain4[0].CacheName)
	assert.False(t, chain4[0].IsHit())
	assert.True(t, chain4[0].IsMiss())
	assert.Equal(t, cachestatus.FwdURIMiss, chain4[0].Fwd)

	// Example 5: Miss that validated on backend with 304 (RFC 9211 §3)
	chain5, err5 := cachestatus.Parse("ExampleCache; fwd=stale; fwd-status=304")
	require.NoError(t, err5)
	require.Len(t, chain5, 1)
	assert.Equal(t, cachestatus.FwdStale, chain5[0].Fwd)
	assert.Equal(t, 304, chain5[0].FwdStatus)
	assert.True(t, chain5[0].IsStale())

	// Example 6: Collapsed request (RFC 9211 §3)
	chain6, err6 := cachestatus.Parse("ExampleCache; fwd=uri-miss; collapsed")
	require.NoError(t, err6)
	require.Len(t, chain6, 1)
	assert.True(t, chain6[0].IsCollapsed())

	// Example 7: Request attempted collapse but could not (RFC 9211 §3)
	chain7, err7 := cachestatus.Parse("ExampleCache; fwd=uri-miss; collapsed=?0")
	require.NoError(t, err7)
	require.Len(t, chain7, 1)
	assert.False(t, chain7[0].IsCollapsed())
	assert.True(t, chain7[0].HasCollapsed)

	// Example 8: Multi-layer caching chain (RFC 9211 §3)
	// Cache1 is closest to origin, Cache2 is closest to user
	chain8, err8 := cachestatus.Parse("Cache1; hit, Cache2; fwd=uri-miss; stored")
	require.NoError(t, err8)
	require.Len(t, chain8, 2)

	origin, okOrigin := chain8.Origin()
	assert.True(t, okOrigin)
	assert.Equal(t, "Cache1", origin.CacheName)
	assert.True(t, origin.IsHit())

	nearest, okNearest := chain8.Nearest()
	assert.True(t, okNearest)
	assert.Equal(t, "Cache2", nearest.CacheName)
	assert.True(t, nearest.IsMiss())
	assert.True(t, nearest.Stored)

	// Example 9: Three-layer caching system (RFC 9211 §3)
	header9 := "ReverseProxy; hit, ForwardProxy; fwd=uri-miss; collapsed; stored, BrowserCache; fwd=uri-miss"
	chain9, err9 := cachestatus.Parse(header9)
	require.NoError(t, err9)
	require.Len(t, chain9, 3)
	assert.Equal(t, "ReverseProxy", chain9[0].CacheName)
	assert.Equal(t, "ForwardProxy", chain9[1].CacheName)
	assert.True(t, chain9[1].IsCollapsed())
	assert.True(t, chain9[1].Stored)
	assert.Equal(t, "BrowserCache", chain9[2].CacheName)

	// Verify serialization roundtrip of chain9
	reparsed9, errReparse := cachestatus.Parse(chain9.String())
	require.NoError(t, errReparse)
	assert.Equal(t, chain9, reparsed9)
}

func TestCacheStatus_ParseHeader(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set(cachestatus.Header, "CDN-1; hit; ttl=3600, Edge-2; fwd=uri-miss; key=\"http://example.com/item\"")

	chain, err := cachestatus.ParseHeader(h)
	require.NoError(t, err)
	require.Len(t, chain, 2)

	assert.Equal(t, "CDN-1", chain[0].CacheName)
	assert.True(t, chain[0].IsHit())
	assert.Equal(t, 3600, chain[0].TTL)

	assert.Equal(t, "Edge-2", chain[1].CacheName)
	assert.Equal(t, cachestatus.FwdURIMiss, chain[1].Fwd)
	assert.Equal(t, "http://example.com/item", chain[1].Key)

	// Empty header returns ErrEmptyHeader
	emptyH := http.Header{}
	_, errEmpty := cachestatus.ParseHeader(emptyH)
	assert.ErrorIs(t, errEmpty, cachestatus.ErrEmptyHeader)
}

func TestCacheStatus_FormatRoundtrip(t *testing.T) {
	t.Parallel()

	entry := cachestatus.Entry{
		CacheName: "MyCustomCDN",
		HasHit:    true,
		Hit:       true,
		HasTTL:    true,
		TTL:       500,
		Key:       "/api/v1/users",
		Detail:    "datacenter-fra",
	}

	formatted := entry.String()
	assert.Contains(t, formatted, "MyCustomCDN")
	assert.Contains(t, formatted, "; hit")
	assert.Contains(t, formatted, "; ttl=500")
	assert.Contains(t, formatted, `; key="/api/v1/users"`)
	assert.Contains(t, formatted, "; detail=datacenter-fra")

	parsed, err := cachestatus.Parse(formatted)
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, entry.CacheName, parsed[0].CacheName)
	assert.Equal(t, entry.Hit, parsed[0].Hit)
	assert.Equal(t, entry.TTL, parsed[0].TTL)
	assert.Equal(t, entry.Key, parsed[0].Key)
	assert.Equal(t, entry.Detail, parsed[0].Detail)
}

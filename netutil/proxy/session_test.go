// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestProxyAwareSessionCache_Operations(t *testing.T) {
	t.Parallel()

	cache := NewProxyAwareSessionCache()
	require.NotNil(t, cache)

	cache.SetProxyKey("http://proxy1.net")
	assert.Equal(t, "http://proxy1.net", cache.CurrentProxyKey())

	cache.Put("google.com", nil)

	_, ok := cache.Get("google.com")
	assert.True(t, ok)

	cache.SetProxyKey("http://proxy2.net")
	assert.Equal(t, "http://proxy2.net", cache.CurrentProxyKey())

	_, ok = cache.Get("google.com")
	assert.False(t, ok, "cache should be cleared after proxy key change")

	cache.Put("yahoo.com", nil)
	cache.SetProxyKey("http://proxy2.net")
	_, ok = cache.Get("yahoo.com")
	assert.True(t, ok)

	cache.Clear()
	_, ok = cache.Get("yahoo.com")
	assert.False(t, ok)
}

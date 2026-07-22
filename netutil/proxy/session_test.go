// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyAwareSessionCache_Operations(t *testing.T) {
	t.Parallel()

	cache := NewProxyAwareSessionCache()
	require.NotNil(t, cache)

	// Set initial proxy key
	cache.SetProxyKey("http://proxy1.net")
	assert.Equal(t, "http://proxy1.net", cache.CurrentProxyKey())

	// Put dummy state (nil state is handled by standard LRU cache safely)
	cache.Put("google.com", nil)

	// Retrieve (should be present, even if nil)
	_, ok := cache.Get("google.com")
	assert.True(t, ok)

	// Change proxy key (must invalidate/clear the cache)
	cache.SetProxyKey("http://proxy2.net")
	assert.Equal(t, "http://proxy2.net", cache.CurrentProxyKey())

	_, ok = cache.Get("google.com")
	assert.False(t, ok, "cache should be cleared after proxy key change")

	// Verify same proxy key does not invalidate
	cache.Put("yahoo.com", nil)
	cache.SetProxyKey("http://proxy2.net") // same key
	_, ok = cache.Get("yahoo.com")
	assert.True(t, ok)

	// Manual Clear
	cache.Clear()
	_, ok = cache.Get("yahoo.com")
	assert.False(t, ok)
}

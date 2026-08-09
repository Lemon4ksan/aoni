// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package urlcache_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/urlcache"
)

func TestURLCacheParse(t *testing.T) {
	u1, err := urlcache.Parse("https://api.example.com/v1/users")
	assert.NoError(t, err)
	assert.Equal(t, "api.example.com", u1.Host)

	// Second call hits cache
	u2, err := urlcache.Parse("https://api.example.com/v1/users")
	assert.NoError(t, err)
	assert.Same(t, u1, u2)
}

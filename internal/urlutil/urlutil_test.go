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
	assert.Same(t, u1, u2)
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

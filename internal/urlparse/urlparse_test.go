// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package urlparse_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/urlparse"
)

func TestReplaceVar(t *testing.T) {
	res := urlparse.ReplaceVar("/users/{id}/details", "id", "42")
	assert.Equal(t, "/users/42/details", res)

	noMatch := urlparse.ReplaceVar("/users/all", "id", "42")
	assert.Equal(t, "/users/all", noMatch)
}

func TestFastAppendQuery(t *testing.T) {
	q1 := urlparse.FastAppendQuery("https://api.example.com/users", "page", "1")
	assert.Equal(t, "https://api.example.com/users?page=1", q1)

	q2 := urlparse.FastAppendQuery("https://api.example.com/users?sort=asc", "page", "2")
	assert.Equal(t, "https://api.example.com/users?sort=asc&page=2", q2)
}

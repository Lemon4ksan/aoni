// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trie_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/netutil/trie"
)

func TestTrieFacade(t *testing.T) {
	t.Parallel()

	tr := trie.NewReverseDomainTrie[string]()
	tr.Insert("example.com", "exact")
	tr.Insert("*.example.com", "wildcard")

	val, ok := tr.Match("example.com")
	assert.True(t, ok)
	assert.Equal(t, "exact", val)

	val, ok = tr.Match("sub.example.com")
	assert.True(t, ok)
	assert.Equal(t, "wildcard", val)
}

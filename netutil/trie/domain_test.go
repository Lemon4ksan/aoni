// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trie_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/netutil/trie"
)

func TestReverseDomainTrie(t *testing.T) {
	tr := trie.NewReverseDomainTrie[string]()

	tr.Insert("example.com", "exact_example")
	tr.Insert("*.example.com", "wildcard_example")
	tr.Insert("*.sub.example.com", "wildcard_sub_example")
	tr.Insert("api.example.com", "exact_api_example")
	tr.Insert("other.org", "other_org")

	// Exact matches
	val, ok := tr.Match("example.com")
	assert.True(t, ok)
	assert.Equal(t, "exact_example", val)

	val, ok = tr.Match("api.example.com")
	assert.True(t, ok)
	assert.Equal(t, "exact_api_example", val)

	// Wildcard matches
	val, ok = tr.Match("foo.example.com")
	assert.True(t, ok)
	assert.Equal(t, "wildcard_example", val)

	val, ok = tr.Match("deep.sub.example.com")
	assert.True(t, ok)
	assert.Equal(t, "wildcard_sub_example", val)

	// Dot suffix handling
	val, ok = tr.Match("foo.example.com.")
	assert.True(t, ok)
	assert.Equal(t, "wildcard_example", val)

	// Non-matching
	_, ok = tr.Match("nomatch.com")
	assert.False(t, ok)

	_, ok = tr.Match("")
	assert.False(t, ok)
}

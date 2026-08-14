// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trie_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/trie"
)

func TestReverseDomainTrie(t *testing.T) {
	tr := trie.NewReverseDomainTrie[string]()

	tr.Insert("example.com", "exact-example")
	tr.Insert("*.example.com", "wildcard-example")
	tr.Insert("api.v1.example.com", "exact-api-v1")
	tr.Insert("*.org", "wildcard-org")

	tests := []struct {
		domain   string
		expected string
		found    bool
	}{
		{"example.com", "exact-example", true},
		{"sub.example.com", "wildcard-example", true},
		{"api.v1.example.com", "exact-api-v1", true},
		{"foo.bar.org", "wildcard-org", true},
		{"unknown.net", "", false},
	}

	for _, tt := range tests {
		val, ok := tr.Match(tt.domain)
		if ok != tt.found {
			t.Errorf("domain %q: expected found=%v, got=%v", tt.domain, tt.found, ok)
		}

		if ok && val != tt.expected {
			t.Errorf("domain %q: expected val=%q, got=%q", tt.domain, tt.expected, val)
		}
	}
}

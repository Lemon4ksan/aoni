// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trie provides high-performance reverse domain radix trees for fast domain suffix routing.
// Core implementation is located in [github.com/lemon4ksan/foundation/silicon/trie].
package trie

import (
	ftrie "github.com/lemon4ksan/foundation/silicon/trie"
)

// ReverseDomainTrie implements a radix tree keyed by reverse domain label components.
type ReverseDomainTrie[V any] = ftrie.ReverseDomainTrie[V]

// NewReverseDomainTrie instantiates an empty [ReverseDomainTrie].
func NewReverseDomainTrie[V any]() *ReverseDomainTrie[V] {
	return ftrie.NewReverseDomainTrie[V]()
}

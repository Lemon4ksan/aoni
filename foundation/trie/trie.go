// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trie provides a high-performance, zero-allocation generic Radix Tree (compact prefix tree)
// with edge compression for O(K) string and path lookups.
package trie

import (
	"slices"
	"strings"
	"sync"
)

// RadixTree is a thread-safe, generic edge-compressed prefix tree (Patricia trie).
// It stores key-value pairs with O(K) lookup time where K is key length.
type RadixTree[V any] struct {
	mu   sync.RWMutex
	root *node[V]
	size int
}

type edge[V any] struct {
	label byte
	node  *node[V]
}

type node[V any] struct {
	prefix   string
	edges    []edge[V]
	value    V
	hasValue bool
}

// New constructs an empty [RadixTree].
func New[V any]() *RadixTree[V] {
	return &RadixTree[V]{
		root: &node[V]{},
	}
}

// Len returns the total number of stored keys in the tree.
func (t *RadixTree[V]) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.size
}

// Insert adds or updates key with val. Returns the previous value and true if the key already existed.
func (t *RadixTree[V]) Insert(key string, val V) (old V, updated bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	curr := t.root
	search := key

	for {
		// Handle empty search key at current node
		if len(search) == 0 {
			if curr.hasValue {
				old = curr.value
				updated = true
			} else {
				t.size++
			}

			curr.value = val
			curr.hasValue = true

			return old, updated
		}

		// Find matching edge by first byte
		idx := curr.findEdge(search[0])
		if idx == -1 {
			// No edge exists: create a new leaf child node
			child := &node[V]{
				prefix:   search,
				value:    val,
				hasValue: true,
			}
			curr.addEdge(search[0], child)

			t.size++

			return old, false
		}

		next := curr.edges[idx].node
		common := commonPrefixLength(search, next.prefix)

		if common < len(next.prefix) {
			// Split the existing edge
			split := &node[V]{
				prefix:   next.prefix[common:],
				edges:    next.edges,
				value:    next.value,
				hasValue: next.hasValue,
			}

			next.prefix = search[:common]
			next.edges = []edge[V]{{label: split.prefix[0], node: split}}
			next.hasValue = false

			var zero V

			next.value = zero
		}

		if common < len(search) {
			// Continue down the tree with remainder
			search = search[common:]
			curr = next
			continue
		}

		// Exact match on next node
		if next.hasValue {
			old = next.value
			updated = true
		} else {
			t.size++
		}

		next.value = val
		next.hasValue = true

		return old, updated
	}
}

// Get retrieves the value associated with key. Returns (value, true) if found.
// Operates with zero heap allocations.
func (t *RadixTree[V]) Get(key string) (V, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	search := key

	for {
		if len(search) == 0 {
			if curr.hasValue {
				return curr.value, true
			}

			var zero V

			return zero, false
		}

		idx := curr.findEdge(search[0])
		if idx == -1 {
			var zero V
			return zero, false
		}

		next := curr.edges[idx].node
		if !strings.HasPrefix(search, next.prefix) {
			var zero V
			return zero, false
		}

		search = search[len(next.prefix):]
		curr = next
	}
}

// LongestPrefix finds the longest registered key that is a prefix of key.
// Returns (matchingPrefix, value, true) if a match is found.
func (t *RadixTree[V]) LongestPrefix(key string) (string, V, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	search := key
	matchedLen := 0

	var (
		lastVal V
		found   bool
	)

	for {
		if curr.hasValue {
			lastVal = curr.value
			found = true
		}

		if len(search) == 0 {
			break
		}

		idx := curr.findEdge(search[0])
		if idx == -1 {
			break
		}

		next := curr.edges[idx].node
		if !strings.HasPrefix(search, next.prefix) {
			break
		}

		matchedLen += len(next.prefix)
		search = search[len(next.prefix):]
		curr = next
	}

	if found {
		return key[:matchedLen], lastVal, true
	}

	var zero V

	return "", zero, false
}

// HasPrefix reports whether any stored key starts with prefix.
func (t *RadixTree[V]) HasPrefix(prefix string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	search := prefix

	for {
		if len(search) == 0 {
			return true
		}

		idx := curr.findEdge(search[0])
		if idx == -1 {
			return false
		}

		next := curr.edges[idx].node

		common := commonPrefixLength(search, next.prefix)
		if common == len(search) {
			return true
		}

		if common < len(next.prefix) {
			return false
		}

		search = search[len(next.prefix):]
		curr = next
	}
}

// WalkPrefix calls fn for every key-value pair under prefix. If fn returns false, traversal stops.
func (t *RadixTree[V]) WalkPrefix(prefix string, fn func(key string, val V) bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	search := prefix

	for len(search) > 0 {
		idx := curr.findEdge(search[0])
		if idx == -1 {
			return
		}

		next := curr.edges[idx].node

		common := commonPrefixLength(search, next.prefix)
		if common < len(search) && common < len(next.prefix) {
			return
		}

		if common >= len(search) {
			walkNode(prefix[:len(prefix)-len(search)]+next.prefix, next, fn)
			return
		}

		search = search[len(next.prefix):]
		curr = next
	}

	walkNode("", curr, fn)
}

func walkNode[V any](accum string, n *node[V], fn func(key string, val V) bool) bool {
	if n.hasValue {
		if !fn(accum, n.value) {
			return false
		}
	}

	for _, e := range n.edges {
		if !walkNode(accum+e.node.prefix, e.node, fn) {
			return false
		}
	}

	return true
}

// Delete removes key from the tree. Returns the removed value and true if key was present.
func (t *RadixTree[V]) Delete(key string) (V, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	val, deleted := t.deleteNode(t.root, key)
	if deleted {
		t.size--
	}

	return val, deleted
}

func (t *RadixTree[V]) deleteNode(curr *node[V], search string) (V, bool) {
	if len(search) == 0 {
		if !curr.hasValue {
			var zero V
			return zero, false
		}

		old := curr.value
		curr.hasValue = false

		var zero V

		curr.value = zero

		return old, true
	}

	idx := curr.findEdge(search[0])
	if idx == -1 {
		var zero V
		return zero, false
	}

	next := curr.edges[idx].node
	if !strings.HasPrefix(search, next.prefix) {
		var zero V
		return zero, false
	}

	val, deleted := t.deleteNode(next, search[len(next.prefix):])
	if !deleted {
		return val, false
	}

	// Edge compression: if child has no value and no edges, remove it
	if !next.hasValue && len(next.edges) == 0 {
		curr.edges = slices.Delete(curr.edges, idx, idx+1)
	} else if !next.hasValue && len(next.edges) == 1 {
		// Merge child edge with single grandchild
		grandChild := next.edges[0].node
		next.prefix += grandChild.prefix
		next.edges = grandChild.edges
		next.value = grandChild.value
		next.hasValue = grandChild.hasValue
	}

	return val, true
}

func (n *node[V]) findEdge(b byte) int {
	for i := 0; i < len(n.edges); i++ {
		if n.edges[i].label == b {
			return i
		}
	}

	return -1
}

func (n *node[V]) addEdge(b byte, child *node[V]) {
	n.edges = append(n.edges, edge[V]{label: b, node: child})
}

//go:inline
func commonPrefixLength(a, b string) int {
	maxLen := min(len(a), len(b))
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}

	return maxLen
}

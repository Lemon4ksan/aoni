// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trie provides high-performance, reverse domain radix tree structures for O(K) domain pattern matching.
package trie

import (
	"strings"
	"sync"
)

type node[V any] struct {
	children map[string]*node[V]
	wildcard *node[V]
	value    V
	hasValue bool
}

// ReverseDomainTrie implements a thread-safe, right-to-left domain matching radix tree.
// It matches exact domains and wildcards (e.g. `*.example.com`, `*.sub.example.com`) in O(K) time
// where K is the number of domain labels, completely independent of the number of patterns.
type ReverseDomainTrie[V any] struct {
	mu   sync.RWMutex
	root node[V]
}

// NewReverseDomainTrie creates an empty [ReverseDomainTrie].
func NewReverseDomainTrie[V any]() *ReverseDomainTrie[V] {
	return &ReverseDomainTrie[V]{}
}

// Insert registers a domain pattern (e.g. "example.com" or "*.example.com") with associated value.
func (t *ReverseDomainTrie[V]) Insert(pattern string, val V) {
	pattern = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pattern), "."))
	if pattern == "" {
		return
	}

	labels := splitDomainLabels(pattern)
	// Right-to-left traversal: reverse labels slice
	reverseLabels(labels)

	t.mu.Lock()
	defer t.mu.Unlock()

	curr := &t.root
	for _, label := range labels {
		if label == "*" {
			if curr.wildcard == nil {
				curr.wildcard = &node[V]{}
			}

			curr = curr.wildcard

			continue
		}

		if curr.children == nil {
			curr.children = make(map[string]*node[V])
		}

		next, exists := curr.children[label]
		if !exists {
			next = &node[V]{}
			curr.children[label] = next
		}

		curr = next
	}

	curr.value = val
	curr.hasValue = true
}

// Match searches for the most specific matching pattern for domain (e.g. "api.example.com").
// Exact label matches take precedence over wildcard matches. Returns (value, true) if matched.
func (t *ReverseDomainTrie[V]) Match(domain string) (V, bool) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		var zero V

		return zero, false
	}

	labels := splitDomainLabels(domain)
	reverseLabels(labels)

	t.mu.RLock()
	defer t.mu.RUnlock()

	val, found := matchNode(&t.root, labels, 0)

	return val, found
}

func matchNode[V any](curr *node[V], labels []string, idx int) (V, bool) {
	if idx == len(labels) {
		if curr.hasValue {
			return curr.value, true
		}

		var zero V

		return zero, false
	}

	label := labels[idx]

	// 1. Try exact label match first
	if curr.children != nil {
		if next, ok := curr.children[label]; ok {
			if val, found := matchNode(next, labels, idx+1); found {
				return val, true
			}
		}
	}

	// 2. Try wildcard match
	if curr.wildcard != nil {
		// Wildcard can match 1 label or remaining labels
		if val, found := matchNode(curr.wildcard, labels, idx+1); found {
			return val, true
		}

		if curr.wildcard.hasValue {
			return curr.wildcard.value, true
		}
	}

	var zero V

	return zero, false
}

func splitDomainLabels(domain string) []string {
	return strings.Split(domain, ".")
}

func reverseLabels(labels []string) {
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
}

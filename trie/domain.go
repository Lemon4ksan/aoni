// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trie provides a thread-safe, right-to-left domain matching radix tree.
package trie

import (
	"slices"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// domainNode represents an internal domain radix tree node keyed by domain label.
type domainNode[V any] struct {
	children map[string]*domainNode[V]
	wildcard *domainNode[V]
	value    V
	hasValue bool
}

// ReverseDomainTrie implements a thread-safe, right-to-left domain matching radix tree.
// It matches exact domains and wildcards (e.g. `*.example.com`, `*.sub.example.com`) in O(K) time
// where K is the number of domain labels, completely independent of the number of patterns.
type ReverseDomainTrie[V any] struct {
	mu   sync.RWMutex
	root domainNode[V]
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

	t.mu.Lock()
	defer t.mu.Unlock()

	curr := &t.root
	// Right-to-left traversal: reverse labels slice
	for _, label := range slices.Backward(labels) {
		if label == "*" {
			if curr.wildcard == nil {
				curr.wildcard = &domainNode[V]{}
			}

			curr = curr.wildcard

			continue
		}

		if curr.children == nil {
			curr.children = make(map[string]*domainNode[V])
		}

		next, exists := curr.children[label]
		if !exists {
			next = &domainNode[V]{}
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
		return generic.Zero[V](), false
	}

	labels := splitDomainLabels(domain)
	slices.Reverse(labels)

	t.mu.RLock()
	defer t.mu.RUnlock()

	return matchDomainNode(&t.root, labels, 0)
}

// matchDomainNode recursively traverses the trie nodes right-to-left matching exact labels before wildcards.
func matchDomainNode[V any](curr *domainNode[V], labels []string, idx int) (V, bool) {
	if idx == len(labels) {
		if curr.hasValue {
			return curr.value, true
		}

		return generic.Zero[V](), false
	}

	label := labels[idx]

	// 1. Try exact label match first
	if curr.children != nil {
		if next, ok := curr.children[label]; ok {
			if val, found := matchDomainNode(next, labels, idx+1); found {
				return val, true
			}
		}
	}

	// 2. Try wildcard match
	if curr.wildcard != nil {
		// Wildcard can match 1 label or remaining labels
		if val, found := matchDomainNode(curr.wildcard, labels, idx+1); found {
			return val, true
		}

		if curr.wildcard.hasValue {
			return curr.wildcard.value, true
		}
	}

	return generic.Zero[V](), false
}

// splitDomainLabels splits a domain string into individual dot-separated label strings.
func splitDomainLabels(domain string) []string {
	var labels []string
	for tok := range bytesconv.ScanTokens(domain, '.') {
		labels = append(labels, tok)
	}

	return labels
}

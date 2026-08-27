// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package etag implements RFC 9111 conditional HTTP caching and automatic 304 Not Modified body reconstruction.
//
// # RFC Compliance
//
// Conforms to RFC 9110 §8.8.3 (Entity Tags) and RFC 9111 §4.3 (Conditional Requests).
package etag

import (
	fetag "github.com/lemon4ksan/foundation/net/http/etag"
)

const DefaultMaxEntries = fetag.DefaultMaxEntries

// Automaton manages ETag recording, If-None-Match header injection, and 304 body reconstruction.
type Automaton = fetag.Automaton

// NewAutomaton creates a new RFC 9111 [Automaton] instance with default capacity (1024).
func NewAutomaton() *Automaton {
	return fetag.NewAutomaton()
}

// NewAutomatonWithCapacity creates a new RFC 9111 [Automaton] with the specified capacity limit.
func NewAutomatonWithCapacity(maxEntries int) *Automaton {
	return fetag.NewAutomatonWithCapacity(maxEntries)
}

// DefaultAutomaton is the package-level shared ETag automaton instance.
var DefaultAutomaton = fetag.DefaultAutomaton

// StrongMatch checks whether two ETags match under strong comparison semantics (RFC 7232 §2.3.2).
func StrongMatch(a, b string) bool {
	return fetag.StrongMatch(a, b)
}

// WeakMatch checks whether two ETags match under weak comparison semantics (RFC 7232 §2.3.2).
func WeakMatch(a, b string) bool {
	return fetag.WeakMatch(a, b)
}

// IsWeak reports whether etagVal is a weak entity tag.
func IsWeak(etagVal string) bool {
	return fetag.IsWeak(etagVal)
}

// Normalize strips whitespace and any leading "W/" or "w/" weak prefix.
func Normalize(etagVal string) string {
	return fetag.Normalize(etagVal)
}

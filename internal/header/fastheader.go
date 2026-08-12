// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package header provides zero-allocation flat array header structures for high-throughput HTTP pipelines.
package header

import (
	"bytes"
)

// HeaderKV stores a single key-value header byte pair in contiguous memory.
type HeaderKV struct {
	Key   []byte
	Value []byte
}

// FastHeader is a zero-allocation flat array structure designed for low-header-count requests (up to 16 slots).
// Linear scans over contiguous L1 CPU data cache lines outperform map hashing and bucket lookups for < 16 items.
type FastHeader struct {
	slots [16]HeaderKV
	count int
}

// Set stores or updates key with val. Performs case-insensitive ASCII comparison.
//
//go:inline
func (h *FastHeader) Set(key, val []byte) {
	_ = h.slots[15] // BCE: Bounds Check Elimination hint for compiler
	for i := 0; i < h.count; i++ {
		if bytes.EqualFold(h.slots[i].Key, key) {
			h.slots[i].Value = val

			return
		}
	}

	if h.count < 16 {
		h.slots[h.count] = HeaderKV{Key: key, Value: val}
		h.count++
	}
}

// SetString stores or updates key with val string values.
//
//go:inline
func (h *FastHeader) SetString(key, val string) {
	h.Set([]byte(key), []byte(val))
}

// Get searches for key and returns (valueBytes, true) if present.
//
//go:inline
func (h *FastHeader) Get(key []byte) ([]byte, bool) {
	_ = h.slots[15] // BCE: Bounds Check Elimination hint for compiler
	for i := 0; i < h.count; i++ {
		if bytes.EqualFold(h.slots[i].Key, key) {
			return h.slots[i].Value, true
		}
	}

	return nil, false
}

// GetString searches for key string and returns (valueString, true) if present.
func (h *FastHeader) GetString(key string) (string, bool) {
	val, ok := h.Get([]byte(key))
	if !ok {
		return "", false
	}

	return string(val), true
}

// Del removes key from the header slots, preserving contiguous memory ordering.
func (h *FastHeader) Del(key []byte) {
	for i := 0; i < h.count; i++ {
		if bytes.EqualFold(h.slots[i].Key, key) {
			copy(h.slots[i:], h.slots[i+1:h.count])
			h.count--
			h.slots[h.count] = HeaderKV{}

			return
		}
	}
}

// Len returns the current count of stored headers.
func (h *FastHeader) Len() int {
	return h.count
}

// Reset clears all header slots for object reuse.
func (h *FastHeader) Reset() {
	for i := 0; i < h.count; i++ {
		h.slots[i] = HeaderKV{}
	}

	h.count = 0
}

// Range iterates over all non-empty header slots until fn returns false.
func (h *FastHeader) Range(fn func(key, value []byte) bool) {
	for i := 0; i < h.count; i++ {
		if !fn(h.slots[i].Key, h.slots[i].Value) {
			break
		}
	}
}

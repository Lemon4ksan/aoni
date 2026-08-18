// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"bytes"

	"github.com/lemon4ksan/aoni/foundation/simd"
)

// PatternSlicer is a zero-allocation byte slice pattern matcher and splitter.
// It searches for pattern in data and splits the byte slice at pattern + offset.
type PatternSlicer struct {
	Pattern []byte
	Offset  int
}

// NewPatternSlicer creates a new [PatternSlicer].
func NewPatternSlicer(pattern []byte, offset int) *PatternSlicer {
	return &PatternSlicer{
		Pattern: pattern,
		Offset:  offset,
	}
}

// findIndex locates the byte offset of s.Pattern in data using vector-accelerated SIMD routines.
func (s *PatternSlicer) findIndex(data []byte) int {
	if len(s.Pattern) == 1 {
		return simd.IndexByteVector(data, s.Pattern[0])
	}

	if len(s.Pattern) == 2 {
		return simd.IndexTwoBytesVector(data, s.Pattern[0], s.Pattern[1])
	}

	return bytes.Index(data, s.Pattern)
}

// Slice finds the first occurrence of Pattern in data, splitting data into two parts at Index + Offset.
// Returns [][]byte{data[:splitPoint], data[splitPoint:]} and true if matched, or [][]byte{data} and false if not matched.
func (s *PatternSlicer) Slice(data []byte) ([][]byte, bool) {
	if len(data) == 0 || len(s.Pattern) == 0 {
		return [][]byte{data}, false
	}

	idx := s.findIndex(data)
	if idx == -1 {
		return [][]byte{data}, false
	}

	splitPoint := idx + s.Offset
	if splitPoint <= 0 || splitPoint >= len(data) {
		return [][]byte{data}, false
	}

	return [][]byte{data[:splitPoint], data[splitPoint:]}, true
}

// SliceAll recursively splits data on every match of Pattern + Offset.
func (s *PatternSlicer) SliceAll(data []byte) [][]byte {
	if len(data) == 0 || len(s.Pattern) == 0 {
		return [][]byte{data}
	}

	var result [][]byte

	curr := data

	for len(curr) > 0 {
		idx := s.findIndex(curr)
		if idx == -1 {
			result = append(result, curr)
			break
		}

		splitPoint := idx + s.Offset
		if splitPoint <= 0 || splitPoint >= len(curr) {
			result = append(result, curr)
			break
		}

		result = append(result, curr[:splitPoint])
		curr = curr[splitPoint:]
	}

	return result
}

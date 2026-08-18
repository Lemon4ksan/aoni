// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"iter"
)

// TrimSpaceASCII removes leading and trailing ASCII whitespace (spaces, tabs, newlines, carriage returns) without heap allocations.
//
//go:inline
func TrimSpaceASCII(s string) string {
	start := 0
	end := len(s)

	for start < end && isASCIIWhitespace(s[start]) {
		start++
	}

	for end > start && isASCIIWhitespace(s[end-1]) {
		end--
	}

	return s[start:end]
}

// TrimSpaceASCIIBytes removes leading and trailing ASCII whitespace from a byte slice without heap allocations.
//
//go:inline
func TrimSpaceASCIIBytes(b []byte) []byte {
	start := 0
	end := len(b)

	for start < end && isASCIIWhitespace(b[start]) {
		start++
	}

	for end > start && isASCIIWhitespace(b[end-1]) {
		end--
	}

	return b[start:end]
}

// CutByte slices s around the first instance of sep, returning the text before and after sep.
// The found result reports whether sep appears in s. If sep does not appear in s, CutByte returns s, "", false.
//
//go:inline
func CutByte(s string, sep byte) (before, after string, found bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}

	return s, "", false
}

// CutByteBytes slices b around the first instance of sep, returning the byte sub-slices before and after sep.
//
//go:inline
func CutByteBytes(b []byte, sep byte) (before, after []byte, found bool) {
	for i := 0; i < len(b); i++ {
		if b[i] == sep {
			return b[:i], b[i+1:], true
		}
	}

	return b, nil, false
}

// ScanTokens yields trimmed tokens from s separated by delim.
// Delimiters occurring inside double quotes are preserved within the token.
// Empty tokens after trimming are skipped.
//
// It operates with 0 heap allocations under range-over-func loops.
func ScanTokens(s string, delim byte) iter.Seq[string] {
	return func(yield func(string) bool) {
		inQuote := false
		start := 0
		n := len(s)

		for i := 0; i < n; i++ {
			c := s[i]
			if c == '"' {
				inQuote = !inQuote
			} else if c == delim && !inQuote {
				token := TrimSpaceASCII(s[start:i])
				if len(token) > 0 {
					if !yield(token) {
						return
					}
				}

				start = i + 1
			}
		}

		if start < n {
			token := TrimSpaceASCII(s[start:])
			if len(token) > 0 {
				yield(token)
			}
		}
	}
}

// ScanTokensBytes yields trimmed byte sub-slices from b separated by delim.
// Delimiters occurring inside double quotes are preserved within the token.
//
// It operates with 0 heap allocations under range-over-func loops.
func ScanTokensBytes(b []byte, delim byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		inQuote := false
		start := 0
		n := len(b)

		for i := 0; i < n; i++ {
			c := b[i]
			if c == '"' {
				inQuote = !inQuote
			} else if c == delim && !inQuote {
				token := TrimSpaceASCIIBytes(b[start:i])
				if len(token) > 0 {
					if !yield(token) {
						return
					}
				}

				start = i + 1
			}
		}

		if start < n {
			token := TrimSpaceASCIIBytes(b[start:])
			if len(token) > 0 {
				yield(token)
			}
		}
	}
}

// ScanPairs yields key-value pairs from s, first splitting by entryDelim, then splitting each entry by kvDelim.
// Keys and values are trimmed of leading/trailing ASCII whitespace.
// Values wrapped in double quotes have their outer quotes trimmed.
// If an entry contains no kvDelim, it yields (key, "").
//
// Example:
//
//	for k, v := range bytesconv.ScanPairs("a=1; b=2; c=\"val;with;delim\"", ';', '=') {
//	    // k="a", v="1" -> k="b", v="2" -> k="c", v="val;with;delim"
//	}
func ScanPairs(s string, entryDelim, kvDelim byte) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for token := range ScanTokens(s, entryDelim) {
			k, v, found := CutByte(token, kvDelim)

			key := TrimSpaceASCII(k)
			if len(key) == 0 {
				continue
			}

			var val string
			if found {
				val = TrimQuotesStr(TrimSpaceASCII(v))
			}

			if !yield(key, val) {
				return
			}
		}
	}
}

// ScanPairsBytes yields key-value byte sub-slices from b, first splitting by entryDelim, then splitting each entry by kvDelim.
//
// It operates with 0 heap allocations.
func ScanPairsBytes(b []byte, entryDelim, kvDelim byte) iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		for token := range ScanTokensBytes(b, entryDelim) {
			k, v, found := CutByteBytes(token, kvDelim)

			key := TrimSpaceASCIIBytes(k)
			if len(key) == 0 {
				continue
			}

			var val []byte
			if found {
				val = TrimQuotes(TrimSpaceASCIIBytes(v))
			}

			if !yield(key, val) {
				return
			}
		}
	}
}

// TrimQuotesStr strips leading and trailing double-quotes if both exist.
//
//go:inline
func TrimQuotesStr(s string) string {
	n := len(s)
	if n >= 2 && s[0] == '"' && s[n-1] == '"' {
		return s[1 : n-1]
	}

	return s
}

//go:inline
func isASCIIWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

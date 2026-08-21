// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package extract provides zero-allocation byte slice scraping and boundary extraction utilities.
// Core implementation is located in [github.com/lemon4ksan/foundation/text/extract].
package extract

import (
	"github.com/lemon4ksan/foundation/generic"
	fextract "github.com/lemon4ksan/foundation/text/extract"
)

var (
	ErrElementNotFound = fextract.ErrElementNotFound
	ErrBetweenNotFound = fextract.ErrBetweenNotFound
	ErrAttrNotFound    = fextract.ErrAttrNotFound
	ErrRegexMismatch   = fextract.ErrRegexMismatch
)

// Between slices a byte buffer between prefix and suffix boundaries with zero allocations.
func Between(src []byte, prefix, suffix string) ([]byte, error) {
	return fextract.Between(src, prefix, suffix)
}

// BetweenResult slices byte buffer between boundaries and returns a Swift-inspired [generic.Result].
func BetweenResult(src []byte, prefix, suffix string) generic.Result[[]byte] {
	return fextract.BetweenResult(src, prefix, suffix)
}

// BetweenString extracts string content between prefix and suffix boundaries as a [generic.Result].
func BetweenString(src []byte, prefix, suffix string) generic.Result[string] {
	return fextract.BetweenString(src, prefix, suffix)
}

// BetweenOptional extracts content between boundaries and returns an [generic.Optional].
func BetweenOptional(src []byte, prefix, suffix string) generic.Optional[string] {
	return fextract.BetweenOptional(src, prefix, suffix)
}

// Attr parses an HTML element attribute value with zero-alloc tokenization.
func Attr(src []byte, css, attrName string) ([]byte, error) {
	return fextract.Attr(src, css, attrName)
}

// AttrResult extracts an HTML attribute value as a Swift-inspired [generic.Result].
func AttrResult(src []byte, css, attrName string) generic.Result[[]byte] {
	return fextract.AttrResult(src, css, attrName)
}

// AttrString extracts an HTML attribute string value as a [generic.Result].
func AttrString(src []byte, css, attrName string) generic.Result[string] {
	return fextract.AttrString(src, css, attrName)
}

// AttrOptional extracts an HTML attribute string value as a [generic.Optional].
func AttrOptional(src []byte, css, attrName string) generic.Optional[string] {
	return fextract.AttrOptional(src, css, attrName)
}

// Regex searches for pattern in src and returns capture group 1 (or match 0).
func Regex(src []byte, pattern string) ([]byte, error) {
	return fextract.Regex(src, pattern)
}

// RegexResult searches pattern in src and returns capture group 1 as a [generic.Result].
func RegexResult(src []byte, pattern string) generic.Result[[]byte] {
	return fextract.RegexResult(src, pattern)
}

// RegexString searches pattern in src and returns capture group 1 as a [generic.Result] string.
func RegexString(src []byte, pattern string) generic.Result[string] {
	return fextract.RegexString(src, pattern)
}

// RegexOptional searches pattern in src and returns capture group 1 as a [generic.Optional].
func RegexOptional(src []byte, pattern string) generic.Optional[string] {
	return fextract.RegexOptional(src, pattern)
}

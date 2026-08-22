// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package extract

import (
	"github.com/lemon4ksan/foundation/generic"
	fextract "github.com/lemon4ksan/foundation/text/extract"
)

var (
	// ErrElementNotFound indicates that an HTML element matching the CSS selector could not be found.
	ErrElementNotFound = fextract.ErrElementNotFound

	// ErrBetweenNotFound indicates that the specified prefix or suffix boundary was not present in the byte slice.
	ErrBetweenNotFound = fextract.ErrBetweenNotFound

	// ErrAttrNotFound indicates that the requested attribute does not exist on the matched HTML element.
	ErrAttrNotFound = fextract.ErrAttrNotFound

	// ErrRegexMismatch indicates that the regular expression pattern did not match the input payload.
	ErrRegexMismatch = fextract.ErrRegexMismatch
)

// Between slices a byte buffer between prefix and suffix boundaries with zero allocations.
func Between(src []byte, prefix, suffix string) ([]byte, error) {
	return fextract.Between(src, prefix, suffix)
}

// BetweenResult extracts bytes between prefix and suffix returning a generic.Result.
func BetweenResult(src []byte, prefix, suffix string) generic.Result[[]byte] {
	return fextract.BetweenResult(src, prefix, suffix)
}

// BetweenString extracts string between prefix and suffix returning a generic.Result.
func BetweenString(src []byte, prefix, suffix string) generic.Result[string] {
	return fextract.BetweenString(src, prefix, suffix)
}

// BetweenOptional extracts string between prefix and suffix returning an optional.Optional.
func BetweenOptional(src []byte, prefix, suffix string) generic.Optional[string] {
	return fextract.BetweenOptional(src, prefix, suffix)
}

// Attr parses an HTML element attribute value with zero-alloc tokenization.
func Attr(src []byte, css, attrName string) ([]byte, error) {
	return fextract.Attr(src, css, attrName)
}

// AttrResult parses an HTML element attribute returning a generic.Result.
func AttrResult(src []byte, css, attrName string) generic.Result[[]byte] {
	return fextract.AttrResult(src, css, attrName)
}

// AttrString parses an HTML element attribute returning a generic.Result[string].
func AttrString(src []byte, css, attrName string) generic.Result[string] {
	return fextract.AttrString(src, css, attrName)
}

// AttrOptional parses an HTML element attribute returning an optional.Optional[string].
func AttrOptional(src []byte, css, attrName string) generic.Optional[string] {
	return fextract.AttrOptional(src, css, attrName)
}

// Regex searches for pattern in src and returns capture group 1 (or match 0).
func Regex(src []byte, pattern string) ([]byte, error) {
	return fextract.Regex(src, pattern)
}

// RegexResult extracts regex capture group returning a generic.Result.
func RegexResult(src []byte, pattern string) generic.Result[[]byte] {
	return fextract.RegexResult(src, pattern)
}

// RegexString extracts regex capture group returning a generic.Result[string].
func RegexString(src []byte, pattern string) generic.Result[string] {
	return fextract.RegexString(src, pattern)
}

// RegexOptional extracts regex capture group returning an optional.Optional[string].
func RegexOptional(src []byte, pattern string) generic.Optional[string] {
	return fextract.RegexOptional(src, pattern)
}

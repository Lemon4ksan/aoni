// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package extract provides zero-allocation byte slice scraping and boundary extraction utilities.
package extract

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

var (
	// ErrElementNotFound is returned when target HTML element (by id/tag) is missing.
	ErrElementNotFound = errors.New("aoni/extract: target HTML element not found")
	// ErrBetweenNotFound is returned when prefix or suffix boundary is missing during between extraction.
	ErrBetweenNotFound = errors.New("aoni/extract: boundary not found during between extraction")
	// ErrAttrNotFound is returned when HTML attribute is missing.
	ErrAttrNotFound = errors.New("aoni/extract: HTML attribute not found")
	// ErrRegexMismatch is returned when regex pattern fails to match data.
	ErrRegexMismatch = errors.New("aoni/extract: regular expression did not match")
)

// Between slices a byte buffer between prefix and suffix boundaries with zero allocations.
func Between(src []byte, prefix, suffix string) ([]byte, error) {
	startIdx := 0
	if prefix != "" {
		pIdx := bytes.Index(src, []byte(prefix))
		if pIdx == -1 {
			return nil, ErrBetweenNotFound
		}

		startIdx = pIdx + len(prefix)
	}

	remaining := src[startIdx:]
	if suffix != "" {
		sIdx := bytes.Index(remaining, []byte(suffix))
		if sIdx == -1 {
			return nil, ErrBetweenNotFound
		}

		return remaining[:sIdx], nil
	}

	return remaining, nil
}

// BetweenResult slices byte buffer between boundaries and returns a Swift-inspired [generic.Result].
func BetweenResult(src []byte, prefix, suffix string) generic.Result[[]byte] {
	b, err := Between(src, prefix, suffix)
	if err != nil {
		return generic.Failure[[]byte](err)
	}

	return generic.Success(b)
}

// BetweenString extracts string content between prefix and suffix boundaries as a [generic.Result].
func BetweenString(src []byte, prefix, suffix string) generic.Result[string] {
	b, err := Between(src, prefix, suffix)
	if err != nil {
		return generic.Failure[string](err)
	}

	return generic.Success(bytesconv.B2S(b))
}

// BetweenOptional extracts content between boundaries and returns an [generic.Optional].
func BetweenOptional(src []byte, prefix, suffix string) generic.Optional[string] {
	b, err := Between(src, prefix, suffix)
	if err != nil {
		return generic.None[string]()
	}

	return generic.Some(bytesconv.B2S(b))
}

// Attr parses an HTML element attribute value with zero-alloc tokenization.
func Attr(src []byte, css, attrName string) ([]byte, error) {
	if len(css) == 0 || css[0] != '#' {
		return extractAttributeValue(src, attrName)
	}

	idTarget := css[1:]
	idKey := "id=\"" + idTarget + "\""

	pos := bytes.Index(src, []byte(idKey))
	if pos == -1 {
		idKey = "id='" + idTarget + "'"
		pos = bytes.Index(src, []byte(idKey))
	}

	if pos == -1 {
		return nil, ErrElementNotFound
	}

	tagStart := bytes.LastIndexByte(src[:pos], '<')
	if tagStart == -1 {
		return nil, ErrAttrNotFound
	}

	tagEnd := bytes.IndexByte(src[pos:], '>')
	if tagEnd == -1 {
		return nil, ErrAttrNotFound
	}

	tagSlice := src[tagStart : pos+tagEnd+1]

	return extractAttributeValue(tagSlice, attrName)
}

// AttrResult extracts an HTML attribute value as a Swift-inspired [generic.Result].
func AttrResult(src []byte, css, attrName string) generic.Result[[]byte] {
	b, err := Attr(src, css, attrName)
	if err != nil {
		return generic.Failure[[]byte](err)
	}

	return generic.Success(b)
}

// AttrString extracts an HTML attribute string value as a [generic.Result].
func AttrString(src []byte, css, attrName string) generic.Result[string] {
	b, err := Attr(src, css, attrName)
	if err != nil {
		return generic.Failure[string](err)
	}

	return generic.Success(bytesconv.B2S(b))
}

// AttrOptional extracts an HTML attribute string value as a [generic.Optional].
func AttrOptional(src []byte, css, attrName string) generic.Optional[string] {
	b, err := Attr(src, css, attrName)
	if err != nil {
		return generic.None[string]()
	}

	return generic.Some(bytesconv.B2S(b))
}

func extractAttributeValue(data []byte, attrName string) ([]byte, error) {
	attrKey := []byte(attrName + "=\"")
	idx := bytes.Index(data, attrKey)

	quote := byte('"')
	if idx == -1 {
		attrKey = []byte(attrName + "='")
		idx = bytes.Index(data, attrKey)
		quote = byte('\'')
	}

	if idx == -1 {
		return nil, ErrAttrNotFound
	}

	start := idx + len(attrKey)

	end := bytes.IndexByte(data[start:], quote)
	if end == -1 {
		return nil, ErrAttrNotFound
	}

	return data[start : start+end], nil
}

// Regex searches for pattern in src and returns capture group 1 (or match 0).
func Regex(src []byte, pattern string) ([]byte, error) {
	rx, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("aoni/extract: compile regex %q: %w", pattern, err)
	}

	matches := rx.FindSubmatch(src)
	if len(matches) < 2 {
		if len(matches) == 1 {
			return matches[0], nil
		}

		return nil, ErrRegexMismatch
	}

	return matches[1], nil
}

// RegexResult searches pattern in src and returns capture group 1 as a [generic.Result].
func RegexResult(src []byte, pattern string) generic.Result[[]byte] {
	b, err := Regex(src, pattern)
	if err != nil {
		return generic.Failure[[]byte](err)
	}

	return generic.Success(b)
}

// RegexString searches pattern in src and returns capture group 1 as a [generic.Result] string.
func RegexString(src []byte, pattern string) generic.Result[string] {
	b, err := Regex(src, pattern)
	if err != nil {
		return generic.Failure[string](err)
	}

	return generic.Success(bytesconv.B2S(b))
}

// RegexOptional searches pattern in src and returns capture group 1 as a [generic.Optional].
func RegexOptional(src []byte, pattern string) generic.Optional[string] {
	b, err := Regex(src, pattern)
	if err != nil {
		return generic.None[string]()
	}

	return generic.Some(bytesconv.B2S(b))
}

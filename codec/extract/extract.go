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

// Attr parses an HTML element attribute value with zero-alloc tokenization.
func Attr(src []byte, css, attrName string) ([]byte, error) {
	idTarget := ""
	if len(css) > 0 && css[0] == '#' {
		idTarget = css[1:]
	}

	if idTarget != "" {
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
		if tagStart != -1 {
			tagEnd := bytes.IndexByte(src[pos:], '>')
			if tagEnd != -1 {
				tagSlice := src[tagStart : pos+tagEnd+1]
				return extractAttributeValue(tagSlice, attrName)
			}
		}

		return nil, ErrAttrNotFound
	}

	return extractAttributeValue(src, attrName)
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

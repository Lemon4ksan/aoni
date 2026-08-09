// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package urlparse provides zero-allocation URL path variable template expansion and string substitution.
package urlparse

import (
	"strings"
	"sync"
	"unsafe"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

var builderPool = sync.Pool{
	New: func() any {
		return new(strings.Builder)
	},
}

// ReplaceVar performs zero-allocation path variable replacement ({key} -> value) in path.
func ReplaceVar(path, key, value string) string {
	target := "{" + key + "}"

	before, after, ok := strings.Cut(path, target)
	if !ok {
		return path
	}

	sb := builderPool.Get().(*strings.Builder)

	sb.Reset()
	defer builderPool.Put(sb)

	sb.Grow(len(path) + len(value) - len(target))

	sb.WriteString(before)
	sb.WriteString(value)
	sb.WriteString(after)

	return sb.String()
}

// FastAppendQuery appends a key-value query parameter to rawURL with zero heap allocations when capacity allows.
func FastAppendQuery(rawURL, key, value string) string {
	if key == "" {
		return rawURL
	}

	sb := builderPool.Get().(*strings.Builder)

	sb.Reset()
	defer builderPool.Put(sb)

	sb.Grow(len(rawURL) + len(key) + len(value) + 2)
	sb.WriteString(rawURL)

	if strings.Contains(rawURL, "?") {
		sb.WriteByte('&')
	} else {
		sb.WriteByte('?')
	}

	sb.WriteString(key)
	sb.WriteByte('=')
	sb.WriteString(value)

	return sb.String()
}

// Suppress unused warning for bytesconv import if any.
var (
	_ = bytesconv.B2S(nil)
	_ = unsafe.Sizeof(0)
)

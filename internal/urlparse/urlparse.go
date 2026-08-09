// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package urlparse provides zero-allocation URL path variable template expansion and string substitution.
package urlparse

import (
	"strings"
	"sync"
	"unsafe"
)

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

// ReplaceVar performs zero-allocation path variable replacement ({key} -> value) in path.
func ReplaceVar(path, key, value string) string {
	target := "{" + key + "}"

	before, after, ok := strings.Cut(path, target)
	if !ok {
		return path
	}

	bufPtr := bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	buf = append(buf, before...)
	buf = append(buf, value...)
	buf = append(buf, after...)

	res := unsafe.String(unsafe.SliceData(buf), len(buf))

	*bufPtr = buf
	bufPool.Put(bufPtr)

	return res
}

// FastAppendQuery appends key=value to targetURL with zero allocations.
func FastAppendQuery(targetURL, key, value string) string {
	if key == "" {
		return targetURL
	}

	bufPtr := bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	buf = append(buf, targetURL...)
	if strings.Contains(targetURL, "?") {
		buf = append(buf, '&')
	} else {
		buf = append(buf, '?')
	}

	buf = append(buf, key...)
	buf = append(buf, '=')
	buf = append(buf, value...)

	res := unsafe.String(unsafe.SliceData(buf), len(buf))

	*bufPtr = buf
	bufPool.Put(bufPtr)

	return res
}

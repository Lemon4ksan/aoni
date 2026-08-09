// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package urlutil provides zero-allocation URL parsing, caching, path variable expansion,
// and fast query parameter appending.
package urlutil

import (
	"net/url"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni/internal/simd"
)

var (
	cache   sync.Map
	bufPool = sync.Pool{
		New: func() any {
			b := make([]byte, 0, 512)
			return &b
		},
	}
)

// Parse parses rawURL string or returns a cached [*url.URL] pointer with zero heap allocations.
func Parse(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return &url.URL{}, nil
	}

	if val, ok := cache.Load(rawURL); ok {
		return val.(*url.URL), nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	cache.Store(rawURL, u)

	return u, nil
}

// ReplaceVar performs path variable replacement ({key} -> value) in path.
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

	res := string(buf)

	*bufPtr = buf
	bufPool.Put(bufPtr)

	return res
}

// FastAppendQuery appends key=value to targetURL using SIMD byte detection and pooled buffers.
func FastAppendQuery(targetURL, key, value string) string {
	if key == "" {
		return targetURL
	}

	bufPtr := bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	buf = append(buf, targetURL...)
	if simd.IndexByteVector([]byte(targetURL), '?') >= 0 {
		buf = append(buf, '&')
	} else {
		buf = append(buf, '?')
	}

	buf = append(buf, key...)
	buf = append(buf, '=')
	buf = append(buf, value...)

	res := string(buf)

	*bufPtr = buf
	bufPool.Put(bufPtr)

	return res
}

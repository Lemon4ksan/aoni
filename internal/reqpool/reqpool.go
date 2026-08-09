// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reqpool provides high-performance object pooling for http.Header maps,
// eliminating heap allocations on hot request execution paths.
package reqpool

import (
	"net/http"
	"sync"
)

var headerPool = sync.Pool{
	New: func() any {
		return make(http.Header, 8)
	},
}

// AcquireHeader checks out a clean http.Header map from pool.
func AcquireHeader() http.Header {
	return headerPool.Get().(http.Header)
}

// ReleaseHeader resets and returns an http.Header map to pool.
func ReleaseHeader(h http.Header) {
	if h == nil {
		return
	}

	clear(h)
	headerPool.Put(h)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package urlcache provides atomic LRU caching of pre-parsed *url.URL objects,
// eliminating url.Parse heap string allocations on hot request execution paths.
package urlcache

import (
	"net/url"
	"sync"
)

var cache sync.Map

// Parse parses rawURL string or returns a cached *url.URL pointer without heap allocations.
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

	// Store copy in cache
	cache.Store(rawURL, u)

	return u, nil
}

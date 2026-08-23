// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"crypto/tls"
	"net/http"

	"github.com/lemon4ksan/aoni/netutil/cachestatus"
)

// HeaderCacheStatus is the standard HTTP response header field name for cache status metadata (RFC 9211 §2).
const HeaderCacheStatus = cachestatus.Header

// CacheStatusEntry represents a single cache node entry in the Cache-Status header field (RFC 9211 §2).
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/cachestatus].
type CacheStatusEntry = cachestatus.Entry

// CacheStatusChain represents an ordered chain of cache entries from the Cache-Status header (RFC 9211 §2).
type CacheStatusChain = cachestatus.Chain

// ParseCacheStatus parses a Cache-Status HTTP response header string conforming to RFC 9211 §2.
func ParseCacheStatus(header string) (CacheStatusChain, error) {
	return cachestatus.Parse(header)
}

// ParseCacheStatusHeader parses the Cache-Status header from standard [http.Header] map (RFC 9211 §2).
func ParseCacheStatusHeader(h http.Header) (CacheStatusChain, error) {
	return cachestatus.ParseHeader(h)
}

// ResolveStdSessionCache adapts an aoni SessionCache into a standard [tls.ClientSessionCache].
func ResolveStdSessionCache(cache any) tls.ClientSessionCache {
	if cache == nil {
		return nil
	}

	if provider, ok := cache.(interface{ StdTLSSessionCache() tls.ClientSessionCache }); ok {
		return provider.StdTLSSessionCache()
	}

	if stdCache, ok := cache.(tls.ClientSessionCache); ok {
		return stdCache
	}

	return nil
}

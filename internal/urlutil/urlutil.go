// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package urlutil provides zero-allocation URL parsing, caching, path variable expansion,
// and fast query parameter appending.
package urlutil

import (
	"hash/crc32"
	"net/url"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/simd"
)

// fastHash computes a hardware CRC32 hash of string s to select a cache shard index.
//
//go:inline
func fastHash(s string) uint32 {
	return crc32.ChecksumIEEE(bytesconv.S2B(s))
}

// cacheShard protects an isolated hash map partition with an RWMutex to minimize lock contention.
type cacheShard struct {
	mu sync.RWMutex
	m  map[string]*url.URL
}

type shardedURLCache struct {
	shards [16]cacheShard
}

var (
	globalURLCache shardedURLCache
	bufPool        = sync.Pool{
		New: func() any {
			b := make([]byte, 0, 512)
			return &b
		},
	}
)

func init() {
	for i := 0; i < 16; i++ {
		globalURLCache.shards[i].m = make(map[string]*url.URL, 16)
	}
}

// Parse parses rawURL string or returns a cached [*url.URL] pointer.
func Parse(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return &url.URL{}, nil
	}

	idx := fastHash(rawURL) & 15
	sh := &globalURLCache.shards[idx]

	sh.mu.RLock()
	u, ok := sh.m[rawURL]
	sh.mu.RUnlock()

	if ok {
		clone := *u
		return &clone, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	cached := *parsed

	sh.mu.Lock()
	if len(sh.m) > 512 {
		clear(sh.m)
	}

	sh.m[rawURL] = &cached
	sh.mu.Unlock()

	return parsed, nil
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

// AppendRawQuery appends raw query string to targetURL using SIMD byte detection and pooled buffers.
func AppendRawQuery(targetURL, rawQuery string) string {
	if rawQuery == "" {
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

	buf = append(buf, rawQuery...)
	res := string(buf)

	*bufPtr = buf
	bufPool.Put(bufPtr)

	return res
}

// CloneURL returns a deep copy of u.
func CloneURL(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}

	cloned := *u
	if u.User != nil {
		userCopy := *u.User
		cloned.User = &userCopy
	}

	return &cloned
}

// MatchDomainPattern checks if host matches pattern (supporting exact and *.wildcard matches).
func MatchDomainPattern(host, pattern string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	pattern = strings.ToLower(strings.TrimSuffix(pattern, "."))

	if !strings.HasPrefix(pattern, "*.") {
		return host == pattern
	}

	suffix := pattern[1:] // ".example.com"

	return strings.HasSuffix(host, suffix) || host == pattern[2:]
}

// IsCrossOrigin determines whether u1 and u2 belong to different RFC 6454 web origins.
func IsCrossOrigin(u1, u2 *url.URL) bool {
	if u1 == nil || u2 == nil {
		return false
	}

	if !strings.EqualFold(u1.Scheme, u2.Scheme) {
		return true
	}

	h1 := strings.ToLower(strings.TrimSuffix(u1.Hostname(), "."))
	h2 := strings.ToLower(strings.TrimSuffix(u2.Hostname(), "."))

	if h1 != h2 {
		return true
	}

	return CanonicalPort(u1) != CanonicalPort(u2)
}

// CanonicalPort resolves effective port number considering scheme defaults.
func CanonicalPort(u *url.URL) string {
	if u == nil {
		return ""
	}

	port := u.Port()
	if port != "" {
		return port
	}

	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}

	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}

	return ""
}

// IsSameDomainOrSubdomain reports whether clean host h1 and h2 match domain or subdomain suffix.
func IsSameDomainOrSubdomain(clean1, clean2 string) bool {
	clean1 = strings.ToLower(clean1)
	clean2 = strings.ToLower(clean2)

	if clean1 == clean2 {
		return true
	}

	return strings.HasSuffix(clean1, "."+clean2) || strings.HasSuffix(clean2, "."+clean1)
}

// BuildPath constructs a final URL path by interpolating pathParams and appending queryParams efficiently.
func BuildPath(basePath string, pathParams map[string]string, queryParams url.Values) string {
	res := basePath

	for k, v := range pathParams {
		res = ReplaceVar(res, k, url.PathEscape(v))
	}

	if len(queryParams) > 0 {
		encoded := queryParams.Encode()
		if encoded != "" {
			if strings.Contains(res, "?") {
				res += "&" + encoded
			} else {
				res += "?" + encoded
			}
		}
	}

	return res
}

const hexDigits = "0123456789ABCDEF"

//go:inline
//go:nosplit
func shouldEscape(c byte) bool {
	if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
		return false
	}

	switch c {
	case '-', '_', '.', '~':
		return false
	}

	return true
}

// AppendQueryEscape appends percent-encoded bytes of src to dst without heap allocations.
func AppendQueryEscape(dst, src []byte) []byte {
	for _, b := range src {
		if !shouldEscape(b) {
			dst = append(dst, b)
		} else {
			dst = append(dst, '%', hexDigits[b>>4], hexDigits[b&15])
		}
	}

	return dst
}

// AppendQueryEscapeString appends percent-encoded bytes of string src to dst without heap allocations.
func AppendQueryEscapeString(dst []byte, src string) []byte {
	for i := 0; i < len(src); i++ {
		b := src[i]
		if !shouldEscape(b) {
			dst = append(dst, b)
		} else {
			dst = append(dst, '%', hexDigits[b>>4], hexDigits[b&15])
		}
	}

	return dst
}

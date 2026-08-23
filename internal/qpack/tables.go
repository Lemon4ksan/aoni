// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

// StaticTable represents the 99 pre-defined static table entries defined in RFC 9204 Appendix A.
var staticTable = [99]HeaderField{
	{Name: ":authority", Value: ""},
	{Name: ":path", Value: "/"},
	{Name: "age", Value: "0"},
	{Name: "content-disposition", Value: ""},
	{Name: "content-length", Value: "0"},
	{Name: "cookie", Value: ""},
	{Name: "date", Value: ""},
	{Name: "etag", Value: ""},
	{Name: "if-modified-since", Value: ""},
	{Name: "if-none-match", Value: ""},
	{Name: "last-modified", Value: ""},
	{Name: "link", Value: ""},
	{Name: "location", Value: ""},
	{Name: "referer", Value: ""},
	{Name: "set-cookie", Value: ""},
	{Name: ":method", Value: "CONNECT"},
	{Name: ":method", Value: "DELETE"},
	{Name: ":method", Value: "GET"},
	{Name: ":method", Value: "HEAD"},
	{Name: ":method", Value: "OPTIONS"},
	{Name: ":method", Value: "POST"},
	{Name: ":method", Value: "PUT"},
	{Name: ":scheme", Value: "http"},
	{Name: ":scheme", Value: "https"},
	{Name: ":status", Value: "103"},
	{Name: ":status", Value: "200"},
	{Name: ":status", Value: "304"},
	{Name: ":status", Value: "404"},
	{Name: ":status", Value: "503"},
	{Name: "accept", Value: "*/*"},
	{Name: "accept", Value: "application/dns-message"},
	{Name: "accept-encoding", Value: "gzip, deflate, br"},
	{Name: "accept-ranges", Value: "bytes"},
	{Name: "access-control-allow-headers", Value: "cache-control"},
	{Name: "access-control-allow-headers", Value: "content-type"},
	{Name: "access-control-allow-origin", Value: "*"},
	{Name: "cache-control", Value: "max-age=0"},
	{Name: "cache-control", Value: "max-age=2592000"},
	{Name: "cache-control", Value: "max-age=604800"},
	{Name: "cache-control", Value: "no-cache"},
	{Name: "cache-control", Value: "no-store"},
	{Name: "cache-control", Value: "public, max-age=31536000"},
	{Name: "content-encoding", Value: "br"},
	{Name: "content-encoding", Value: "gzip"},
	{Name: "content-type", Value: "application/dns-message"},
	{Name: "content-type", Value: "application/javascript"},
	{Name: "content-type", Value: "application/json"},
	{Name: "content-type", Value: "application/x-www-form-urlencoded"},
	{Name: "image/gif", Value: "image/gif"},
	{Name: "content-type", Value: "image/jpeg"},
	{Name: "content-type", Value: "image/png"},
	{Name: "content-type", Value: "text/css"},
	{Name: "content-type", Value: "text/html; charset=utf-8"},
	{Name: "content-type", Value: "text/plain"},
	{Name: "content-type", Value: "text/plain;charset=utf-8"},
	{Name: "range", Value: "bytes=0-"},
	{Name: "strict-transport-security", Value: "max-age=31536000"},
	{Name: "strict-transport-security", Value: "max-age=31536000; includesubdomains"},
	{Name: "strict-transport-security", Value: "max-age=31536000; includesubdomains; preload"},
	{Name: "vary", Value: "accept-encoding"},
	{Name: "vary", Value: "origin"},
	{Name: "x-content-type-options", Value: "nosniff"},
	{Name: "x-xss-protection", Value: "1; mode=block"},
	{Name: ":status", Value: "100"},
	{Name: ":status", Value: "204"},
	{Name: ":status", Value: "206"},
	{Name: ":status", Value: "302"},
	{Name: ":status", Value: "400"},
	{Name: ":status", Value: "403"},
	{Name: ":status", Value: "421"},
	{Name: ":status", Value: "425"},
	{Name: ":status", Value: "500"},
	{Name: "accept-language", Value: ""},
	{Name: "access-control-allow-credentials", Value: "FALSE"},
	{Name: "access-control-allow-credentials", Value: "TRUE"},
	{Name: "access-control-allow-headers", Value: "*"},
	{Name: "access-control-allow-methods", Value: "get"},
	{Name: "access-control-allow-methods", Value: "get, post, options"},
	{Name: "access-control-allow-methods", Value: "options"},
	{Name: "access-control-expose-headers", Value: "content-length"},
	{Name: "access-control-request-headers", Value: "content-type"},
	{Name: "access-control-request-method", Value: "get"},
	{Name: "access-control-request-method", Value: "post"},
	{Name: "alt-svc", Value: "clear"},
	{Name: "authorization", Value: ""},
	{Name: "content-security-policy", Value: "script-src 'none'; object-src 'none'; base-uri 'none';"},
	{Name: "early-data", Value: "1"},
	{Name: "expect-ct", Value: ""},
	{Name: "forwarded", Value: ""},
	{Name: "if-range", Value: ""},
	{Name: "origin", Value: ""},
	{Name: "purpose", Value: "prefetch"},
	{Name: "server", Value: ""},
	{Name: "timing-allow-origin", Value: "*"},
	{Name: "upgrade-insecure-requests", Value: "1"},
	{Name: "user-agent", Value: ""},
	{Name: "x-forwarded-for", Value: ""},
	{Name: "x-frame-options", Value: "deny"},
	{Name: "x-frame-options", Value: "sameorigin"},
}

// Fix index 48: RFC 9204 defines index 48 as content-type: image/gif
func init() {
	staticTable[48] = HeaderField{Name: "content-type", Value: "image/gif"}
}

var (
	staticNameMap  = make(map[string]int, len(staticTable))
	staticExactMap = make(map[string]map[string]int)
)

func init() {
	for i, entry := range staticTable {
		if _, exists := staticNameMap[entry.Name]; !exists {
			staticNameMap[entry.Name] = i
		}

		if entry.Value != "" {
			if staticExactMap[entry.Name] == nil {
				staticExactMap[entry.Name] = make(map[string]int)
			}

			staticExactMap[entry.Name][entry.Value] = i
		}
	}
}

func findStatic(name, value string) (exactIdx, nameIdx int, hasExact, hasName bool) {
	exactIdx = -1
	nameIdx = -1

	if nIdx, ok := staticNameMap[name]; ok {
		nameIdx = nIdx
		hasName = true

		if valMap, ok := staticExactMap[name]; ok {
			if eIdx, ok := valMap[value]; ok {
				exactIdx = eIdx
				hasExact = true
			}
		}
	}

	return exactIdx, nameIdx, hasExact, hasName
}

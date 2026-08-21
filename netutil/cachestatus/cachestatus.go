// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cachestatus implements the Cache-Status HTTP Response Header Field strictly conforming to RFC 9211.
//
// The Cache-Status header field indicates how caches in the request path handled the request and its response,
// serialized as a Structured Fields List (RFC 8941 / RFC 9211 §2).
package cachestatus

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Header is the standard HTTP response header field name for cache status metadata (RFC 9211 §2).
const Header = "Cache-Status"

// RFC 9211 §2.2: Standard forward reasons (fwd parameter tokens).
const (
	// FwdBypass indicates the cache was configured not to handle this request (RFC 9211 §2.2).
	FwdBypass = "bypass"

	// FwdMethod indicates the request method semantics require the request to be forwarded (e.g. POST, PUT) (RFC 9211 §2.2).
	FwdMethod = "method"

	// FwdURIMiss indicates the cache did not contain any responses matching the request URI (RFC 9211 §2.2).
	FwdURIMiss = "uri-miss"

	// FwdVaryMiss indicates a URI match existed, but Vary headers could not select a response (RFC 9211 §2.2).
	FwdVaryMiss = "vary-miss"

	// FwdMiss indicates the cache did not contain any response (when uri-miss vs vary-miss cannot be distinguished) (RFC 9211 §2.2).
	FwdMiss = "miss"

	// FwdRequest indicates a fresh response was available, but request Cache-Control directives forbade its use (RFC 9211 §2.2).
	FwdRequest = "request"

	// FwdStale indicates the cache contained a response, but it was stale (RFC 9211 §2.2).
	FwdStale = "stale"

	// FwdPartial indicates the cache contained a partial response, but not all requested ranges (RFC 9211 §2.2).
	FwdPartial = "partial"
)

var (
	// ErrEmptyHeader is returned when parsing an empty Cache-Status header value.
	ErrEmptyHeader = errors.New("aoni/cachestatus: empty Cache-Status header")

	// ErrInvalidHeader is returned when the header value violates RFC 9211 / RFC 8941 structured field syntax.
	ErrInvalidHeader = errors.New("aoni/cachestatus: invalid Cache-Status header syntax")
)

// Entry represents a single cache node entry in the Cache-Status header field (RFC 9211 §2).
type Entry struct {
	// CacheName is the identifier of the cache that inserted this status (String or Token, RFC 9211 §2).
	CacheName string

	// Hit indicates whether the request was satisfied by the cache without forwarding (RFC 9211 §2.1).
	Hit bool

	// HasHit indicates whether the "hit" parameter was explicitly present in the header.
	HasHit bool

	// Fwd indicates why the request went forward towards the origin (Token, RFC 9211 §2.2).
	Fwd string

	// FwdStatus is the HTTP status code returned by the next-hop server (RFC 9211 §2.3).
	FwdStatus int

	// TTL is the response's remaining freshness lifetime in seconds (negative if stale, RFC 9211 §2.4).
	TTL int

	// HasTTL indicates whether the "ttl" parameter was explicitly present in the header.
	HasTTL bool

	// Stored indicates whether the cache stored the response (RFC 9211 §2.5).
	Stored bool

	// HasStored indicates whether the "stored" parameter was explicitly present in the header.
	HasStored bool

	// Collapsed indicates whether this request was collapsed/coalesced with concurrent requests (RFC 9211 §2.6).
	Collapsed bool

	// HasCollapsed indicates whether the "collapsed" parameter was explicitly present in the header.
	HasCollapsed bool

	// Key is the representation of the cache key used for the response (RFC 9211 §2.7).
	Key string

	// Detail contains implementation-specific additional information or metrics (RFC 9211 §2.8).
	Detail string
}

// IsHit reports whether this cache entry recorded a successful cache hit without forwarding (RFC 9211 §2.1).
func (e Entry) IsHit() bool {
	return e.HasHit && e.Hit
}

// IsMiss reports whether this cache entry went forward due to a cache miss (RFC 9211 §2.2).
func (e Entry) IsMiss() bool {
	return e.Fwd == FwdMiss || e.Fwd == FwdURIMiss || e.Fwd == FwdVaryMiss
}

// IsStale reports whether this cache entry recorded a stale response (RFC 9211 §2.2 & §2.4).
func (e Entry) IsStale() bool {
	return e.Fwd == FwdStale || (e.HasTTL && e.TTL < 0)
}

// IsCollapsed reports whether request coalescing was used by this cache (RFC 9211 §2.6).
func (e Entry) IsCollapsed() bool {
	return e.HasCollapsed && e.Collapsed
}

// String serializes the cache status entry into standard RFC 9211 structured parameter format.
func (e Entry) String() string {
	var sb strings.Builder
	e.appendTo(&sb)
	return sb.String()
}

func (e Entry) appendTo(sb *strings.Builder) {
	if strings.ContainsAny(e.CacheName, " \"(),/:;<=>?@[\\]{}") || e.CacheName == "" {
		sb.WriteByte('"')
		sb.WriteString(strings.ReplaceAll(e.CacheName, "\"", "\\\""))
		sb.WriteByte('"')
	} else {
		sb.WriteString(e.CacheName)
	}

	if e.HasHit {
		if e.Hit {
			sb.WriteString("; hit")
		} else {
			sb.WriteString("; hit=?0")
		}
	}

	if e.Fwd != "" {
		sb.WriteString("; fwd=")
		sb.WriteString(e.Fwd)
	}

	if e.FwdStatus > 0 {
		sb.WriteString("; fwd-status=")
		sb.WriteString(strconv.Itoa(e.FwdStatus))
	}

	if e.HasTTL {
		sb.WriteString("; ttl=")
		sb.WriteString(strconv.Itoa(e.TTL))
	}

	if e.HasStored {
		if e.Stored {
			sb.WriteString("; stored")
		} else {
			sb.WriteString("; stored=?0")
		}
	}

	if e.HasCollapsed {
		if e.Collapsed {
			sb.WriteString("; collapsed")
		} else {
			sb.WriteString("; collapsed=?0")
		}
	}

	if e.Key != "" {
		sb.WriteString("; key=\"")
		sb.WriteString(strings.ReplaceAll(e.Key, "\"", "\\\""))
		sb.WriteByte('"')
	}

	if e.Detail != "" {
		if strings.ContainsAny(e.Detail, " \"(),/:;<=>?@[\\]{}") {
			sb.WriteString("; detail=\"")
			sb.WriteString(strings.ReplaceAll(e.Detail, "\"", "\\\""))
			sb.WriteByte('"')
		} else {
			sb.WriteString("; detail=")
			sb.WriteString(e.Detail)
		}
	}
}

// Chain represents an ordered chain of [Entry] objects from the Cache-Status header (RFC 9211 §2).
// The first member represents the cache closest to the origin server, and the last member
// represents the cache closest to the user.
type Chain []Entry

// Origin returns the cache entry closest to the origin server (first list member, RFC 9211 §2).
func (c Chain) Origin() (Entry, bool) {
	if len(c) == 0 {
		return Entry{}, false
	}

	return c[0], true
}

// Nearest returns the cache entry closest to the user/client (last list member, RFC 9211 §2).
func (c Chain) Nearest() (Entry, bool) {
	if len(c) == 0 {
		return Entry{}, false
	}

	return c[len(c)-1], true
}

// String formats the entire chain into a standard comma-separated Cache-Status header value (RFC 9211 §2).
func (c Chain) String() string {
	if len(c) == 0 {
		return ""
	}

	var sb strings.Builder
	for i := range c {
		if i > 0 {
			sb.WriteString(", ")
		}

		c[i].appendTo(&sb)
	}

	return sb.String()
}

// Parse extracts and parses all cache status entries from a Cache-Status HTTP header string (RFC 9211 §2).
func Parse(header string) (Chain, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, ErrEmptyHeader
	}

	var chain Chain
	// Split by comma outside quoted strings
	members := splitMembers(header)
	if len(members) == 0 {
		return nil, ErrInvalidHeader
	}

	for _, member := range members {
		entry, err := parseEntry(member)
		if err != nil {
			return nil, err
		}

		chain = append(chain, entry)
	}

	return chain, nil
}

// ParseHeader parses the Cache-Status header from standard [http.Header] map (RFC 9211 §2).
func ParseHeader(h http.Header) (Chain, error) {
	vals := h[Header]
	if len(vals) == 0 {
		// Fallback case-insensitive search
		for k, v := range h {
			if strings.EqualFold(k, Header) && len(v) > 0 {
				vals = v
				break
			}
		}
	}

	if len(vals) == 0 {
		return nil, ErrEmptyHeader
	}

	var chain Chain
	for _, val := range vals {
		c, err := Parse(val)
		if err != nil {
			return nil, err
		}

		chain = append(chain, c...)
	}

	return chain, nil
}

func splitMembers(header string) []string {
	var members []string

	start := 0
	inQuote := false

	for i := 0; i < len(header); i++ {
		c := header[i]
		if c == '\\' && inQuote && i+1 < len(header) {
			i++
			continue
		}

		if c == '"' {
			inQuote = !inQuote
		} else if c == ',' && !inQuote {
			m := strings.TrimSpace(header[start:i])
			if m != "" {
				members = append(members, m)
			}

			start = i + 1
		}
	}

	if start < len(header) {
		m := strings.TrimSpace(header[start:])
		if m != "" {
			members = append(members, m)
		}
	}

	return members
}

func parseEntry(s string) (Entry, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Entry{}, ErrInvalidHeader
	}

	var entry Entry

	// Extract cache identifier (String or Token) up to first semicolon
	before, after, ok := strings.Cut(s, ";")

	var idPart, paramsPart string
	if !ok {
		idPart = s
		paramsPart = ""
	} else {
		idPart = strings.TrimSpace(before)
		paramsPart = strings.TrimSpace(after)
	}

	if strings.HasPrefix(idPart, "\"") && strings.HasSuffix(idPart, "\"") && len(idPart) >= 2 {
		entry.CacheName = unescapeString(idPart[1 : len(idPart)-1])
	} else {
		entry.CacheName = idPart
	}

	if entry.CacheName == "" {
		return Entry{}, ErrInvalidHeader
	}

	if paramsPart == "" {
		return entry, nil
	}

	// Parse parameters separated by semicolons
	params := splitParams(paramsPart)
	for _, p := range params {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		eqIdx := strings.IndexByte(p, '=')

		var key, val string
		if eqIdx < 0 {
			key = strings.ToLower(p)
			val = "?1" // RFC 8941 boolean true default for bare parameters
		} else {
			key = strings.ToLower(strings.TrimSpace(p[:eqIdx]))
			val = strings.TrimSpace(p[eqIdx+1:])
		}

		switch key {
		case "hit":
			entry.HasHit = true
			entry.Hit = parseBool(val)

		case "fwd":
			entry.Fwd = strings.Trim(val, "\"")

		case "fwd-status":
			if status, err := strconv.Atoi(val); err == nil {
				entry.FwdStatus = status
			}

		case "ttl":
			if ttl, err := strconv.Atoi(val); err == nil {
				entry.TTL = ttl
				entry.HasTTL = true
			}

		case "stored":
			entry.HasStored = true
			entry.Stored = parseBool(val)

		case "collapsed":
			entry.HasCollapsed = true
			entry.Collapsed = parseBool(val)

		case "key":
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
				entry.Key = unescapeString(val[1 : len(val)-1])
			} else {
				entry.Key = val
			}

		case "detail":
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
				entry.Detail = unescapeString(val[1 : len(val)-1])
			} else {
				entry.Detail = val
			}
		}
	}

	return entry, nil
}

func splitParams(s string) []string {
	var params []string

	start := 0
	inQuote := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && inQuote && i+1 < len(s) {
			i++
			continue
		}

		if c == '"' {
			inQuote = !inQuote
		} else if c == ';' && !inQuote {
			p := strings.TrimSpace(s[start:i])
			if p != "" {
				params = append(params, p)
			}

			start = i + 1
		}
	}

	if start < len(s) {
		p := strings.TrimSpace(s[start:])
		if p != "" {
			params = append(params, p)
		}
	}

	return params
}

func parseBool(v string) bool {
	v = strings.TrimSpace(v)
	return v == "?1" || strings.EqualFold(v, "true") || v == "1"
}

func unescapeString(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			sb.WriteByte(s[i])
		} else {
			sb.WriteByte(s[i])
		}
	}

	return sb.String()
}

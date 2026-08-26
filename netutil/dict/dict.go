// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidUseAsDictionary indicates a malformed Use-As-Dictionary header.
	ErrInvalidUseAsDictionary = errors.New("dict: invalid Use-As-Dictionary header")

	// ErrMissingMatchPattern indicates the 'match' parameter is absent (RFC 9842 §2.1.1).
	ErrMissingMatchPattern = errors.New("dict: missing required 'match' parameter in Use-As-Dictionary")

	// ErrUnsupportedDictionaryType indicates an unsupported dictionary 'type' (RFC 9842 §2.1.4).
	ErrUnsupportedDictionaryType = errors.New("dict: unsupported dictionary type")

	// ErrInvalidAvailableDictionary indicates a malformed Available-Dictionary header.
	ErrInvalidAvailableDictionary = errors.New("dict: invalid Available-Dictionary header")

	// ErrDictionaryExpired indicates that the dictionary has expired according to its TTL/freshness.
	ErrDictionaryExpired = errors.New("dict: dictionary expired")
)

// Dictionary represents a cached HTTP compression dictionary conforming to RFC 9842.
type Dictionary struct {
	// Hash is the SHA-256 digest of the raw dictionary payload (RFC 9842 §2.2).
	Hash [32]byte

	// ID is the optional server-provided identifier string (RFC 9842 §2.1.3).
	ID string

	// BaseURL is the origin and response URL from which the dictionary was fetched (RFC 9842 §2.1.1).
	BaseURL *url.URL

	// Match is the URL Pattern string used for matching outbound requests (RFC 9842 §2.1.1).
	Match string

	// MatchDest is the list of Fetch request destinations (RFC 9842 §2.1.2).
	MatchDest []string

	// Type specifies the dictionary file format, defaulting to "raw" (RFC 9842 §2.1.4).
	Type string

	// TTL is the freshness lifetime of the dictionary.
	TTL time.Duration

	// FetchedAt is the timestamp when the dictionary was stored.
	FetchedAt time.Time

	// ExpiresAt is the absolute expiration time of the dictionary (RFC 9842 §2.2.1).
	ExpiresAt time.Time

	// Data holds the raw uncompressed dictionary bytes.
	Data []byte
}

// IsFresh reports whether the dictionary is fresh at the given timestamp (RFC 9842 §2.2.1).
func (d *Dictionary) IsFresh(now time.Time) bool {
	if d == nil {
		return false
	}

	if d.ExpiresAt.IsZero() {
		return true
	}

	return now.Before(d.ExpiresAt)
}

// Matches reports whether this dictionary matches the given request URL and destination (RFC 9842 §2.2.2).
func (d *Dictionary) Matches(targetURL *url.URL, dest string) bool {
	if d == nil || targetURL == nil || d.BaseURL == nil {
		return false
	}

	// 1. Destination check (RFC 9842 §2.1.2 & §2.2.2)
	if !MatchDest(d.MatchDest, dest) {
		return false
	}

	// 2. Same-Origin check (RFC 9842 §2.1.1, §2.2.2, §9.3.1)
	if !IsSameOrigin(d.BaseURL, targetURL) {
		return false
	}

	// 3. URL Pattern Match check (RFC 9842 §2.1.1 & §2.2.2)
	return MatchURLPattern(d.Match, d.BaseURL, targetURL)
}

// IsSameOrigin reports whether u1 and u2 belong to the exact same Origin (RFC 9110 §4.3.1 & RFC 9842 §2.2.2).
func IsSameOrigin(u1, u2 *url.URL) bool {
	if u1 == nil || u2 == nil {
		return false
	}

	if !strings.EqualFold(u1.Scheme, u2.Scheme) {
		return false
	}

	return strings.EqualFold(u1.Host, u2.Host)
}

// MatchDest reports whether request destination dest satisfies the dictionary's match-dest list (RFC 9842 §2.1.2).
func MatchDest(matchDest []string, dest string) bool {
	if len(matchDest) == 0 {
		return true
	}

	if dest == "" {
		return false
	}

	for _, d := range matchDest {
		if strings.EqualFold(d, dest) {
			return true
		}
	}

	return false
}

// MatchURLPattern tests if targetURL matches the specified pattern relative to baseURL (RFC 9842 §2.1.1).
func MatchURLPattern(pattern string, baseURL, targetURL *url.URL) bool {
	if pattern == "" || targetURL == nil {
		return false
	}

	targetPath := targetURL.EscapedPath()
	if targetPath == "" {
		targetPath = "/"
	}

	// If pattern is a full URL or absolute path
	pat := pattern
	if strings.HasPrefix(pat, "http://") || strings.HasPrefix(pat, "https://") {
		parsed, err := url.Parse(pat)
		if err != nil || !IsSameOrigin(baseURL, parsed) {
			return false
		}

		pat = parsed.EscapedPath()
	} else if !strings.HasPrefix(pat, "/") {
		// Relative pattern to baseURL path directory
		baseDir := "/"
		if baseURL != nil && baseURL.EscapedPath() != "" {
			idx := strings.LastIndex(baseURL.EscapedPath(), "/")
			if idx >= 0 {
				baseDir = baseURL.EscapedPath()[:idx+1]
			}
		}

		pat = baseDir + pat
	}

	return matchWildcard(pat, targetPath)
}

// matchWildcard implements standard glob/wildcard matching for URL patterns without regex (RFC 9842 §2.1.1).
func matchWildcard(pattern, s string) bool {
	for len(pattern) > 0 {
		idx := strings.IndexByte(pattern, '*')
		if idx < 0 {
			return pattern == s
		}

		prefix := pattern[:idx]
		if !strings.HasPrefix(s, prefix) {
			return false
		}

		s = s[len(prefix):]
		pattern = pattern[idx+1:]

		if len(pattern) == 0 {
			return true
		}

		nextWildcard := strings.IndexByte(pattern, '*')

		var segment string

		if nextWildcard >= 0 {
			segment = pattern[:nextWildcard]
		} else {
			segment = pattern
		}

		matchIdx := strings.Index(s, segment)
		if matchIdx < 0 {
			return false
		}

		s = s[matchIdx:]
	}

	return len(s) == 0
}

// DictionaryMeta holds the parsed parameters from a Use-As-Dictionary response header.
type DictionaryMeta struct {
	Match     string
	MatchDest []string
	ID        string
	Type      string
	TTL       time.Duration
}

// ParseUseAsDictionary parses a Use-As-Dictionary structured header value per RFC 9842 §2.1.
func ParseUseAsDictionary(header string, respURL *url.URL) (*DictionaryMeta, error) {
	if header == "" {
		return nil, ErrInvalidUseAsDictionary
	}

	meta := &DictionaryMeta{
		Type: TypeRaw,
	}

	// Tokenize comma-separated structured field parameters
	parts := strings.Split(header, ",")
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}

		key, val, hasVal := strings.Cut(item, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "match":
			if !hasVal {
				return nil, ErrInvalidUseAsDictionary
			}

			meta.Match = unquoteString(val)

		case "match-dest":
			if hasVal {
				meta.MatchDest = parseInnerList(val)
			}

		case "id":
			if hasVal {
				id := unquoteString(val)
				if len(id) > MaxIDLength {
					id = id[:MaxIDLength]
				}

				meta.ID = id
			}

		case "type":
			if hasVal {
				meta.Type = strings.ToLower(unquoteString(val))
			} else {
				meta.Type = TypeRaw
			}

		case "ttl":
			if hasVal {
				if sec, err := strconv.ParseInt(unquoteString(val), 10, 64); err == nil && sec > 0 {
					meta.TTL = time.Duration(sec) * time.Second
				}
			}

		case "raw":
			meta.Type = TypeRaw
		}
	}

	if meta.Match == "" {
		// Default to the directory of the response URL if match is omitted (or error if strictly required)
		if respURL != nil && respURL.EscapedPath() != "" {
			idx := strings.LastIndex(respURL.EscapedPath(), "/")
			if idx >= 0 {
				meta.Match = respURL.EscapedPath()[:idx+1] + "*"
			} else {
				meta.Match = "/*"
			}
		} else {
			return nil, ErrMissingMatchPattern
		}
	}

	if meta.Type != "" && meta.Type != TypeRaw {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDictionaryType, meta.Type)
	}

	return meta, nil
}

func unquoteString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}

	return s
}

func parseInnerList(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}

	var res []string

	for _, token := range strings.Fields(s) {
		token = strings.Trim(token, " ,;\"")
		if token != "" {
			res = append(res, token)
		}
	}

	return res
}

// FormatAvailableDictionary formats a 32-byte SHA-256 hash as an RFC 8941 Structured Field Byte Sequence
// (:base64:) per RFC 9842 §2.2.
func FormatAvailableDictionary(hash [32]byte) string {
	b64 := base64.StdEncoding.EncodeToString(hash[:])

	var sb strings.Builder
	sb.Grow(len(b64) + 2)
	sb.WriteByte(':')
	sb.WriteString(b64)
	sb.WriteByte(':')

	return sb.String()
}

// ParseAvailableDictionary parses an RFC 8941 Structured Field Byte Sequence (:base64:) from an
// Available-Dictionary request header into a 32-byte SHA-256 digest (RFC 9842 §2.2).
func ParseAvailableDictionary(header string) ([32]byte, error) {
	var hash [32]byte

	s := strings.TrimSpace(header)
	if len(s) < 2 || s[0] != ':' || s[len(s)-1] != ':' {
		return hash, ErrInvalidAvailableDictionary
	}

	rawB64 := s[1 : len(s)-1]
	if len(rawB64) > 64 {
		return hash, ErrInvalidAvailableDictionary
	}

	decoded, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil || len(decoded) != 32 {
		return hash, ErrInvalidAvailableDictionary
	}

	copy(hash[:], decoded)

	return hash, nil
}

// ComputeSHA256 returns the 32-byte SHA-256 hash digest of data.
func ComputeSHA256(data []byte) [32]byte {
	return sha256.Sum256(data)
}

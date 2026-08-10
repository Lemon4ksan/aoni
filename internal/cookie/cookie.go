// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie implements a cookie parser and serializer for HTTP cookies.
package cookie

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// CookieDTO is the data transfer representation of Cookie for internal operations.
type CookieDTO struct {
	Expires      time.Time
	Name         string
	Value        string
	Domain       string
	Path         string
	SameSite     string
	PartitionKey string
	HTTPOnly     bool
	Secure       bool
	Partitioned  bool
	MaxAge       int
}

// ParseSetCookieHeader parses a raw Set-Cookie header line.
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) CookieDTO {
	if headerVal == "" {
		return CookieDTO{}
	}

	parts := strings.Split(headerVal, ";")
	if len(parts) == 0 {
		return CookieDTO{}
	}

	nameVal := strings.TrimSpace(parts[0])

	eqIdx := strings.IndexByte(nameVal, '=')
	if eqIdx <= 0 {
		return CookieDTO{}
	}

	c := CookieDTO{
		Name:   strings.TrimSpace(nameVal[:eqIdx]),
		Value:  strings.TrimSpace(nameVal[eqIdx+1:]),
		Domain: defaultDomain,
		Path:   defaultPath,
	}

	for _, attr := range parts[1:] {
		ParseCookieAttribute(strings.TrimSpace(attr), &c)
	}

	return c
}

// ParseCookieAttribute sets cookie properties from attribute strings.
func ParseCookieAttribute(attr string, c *CookieDTO) {
	if attr == "" {
		return
	}

	lower := strings.ToLower(attr)
	switch {
	case lower == "httponly":
		c.HTTPOnly = true
	case lower == "secure":
		c.Secure = true
	case lower == "partitioned":
		c.Partitioned = true
	case strings.HasPrefix(lower, "samesite="):
		c.SameSite = parseAttributeValue(attr)
	case strings.HasPrefix(lower, "domain="):
		c.Domain = strings.TrimPrefix(parseAttributeValue(attr), ".")
	case strings.HasPrefix(lower, "path="):
		c.Path = parseAttributeValue(attr)
	case strings.HasPrefix(lower, "max-age="):
		if maxAge, err := strconv.Atoi(parseAttributeValue(attr)); err == nil {
			c.MaxAge = maxAge
		}
	case strings.HasPrefix(lower, "expires="):
		if exp, err := http.ParseTime(parseAttributeValue(attr)); err == nil {
			c.Expires = exp
		}
	}
}

func parseAttributeValue(attr string) string {
	_, val, ok := strings.Cut(attr, "=")
	if !ok {
		return ""
	}

	return strings.TrimSpace(val)
}

// PathMatch reports whether reqPath matches cookiePath per RFC 6265 §5.1.4.
func PathMatch(reqPath, cookiePath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}

	if reqPath == "" {
		reqPath = "/"
	}

	if reqPath == cookiePath {
		return true
	}

	if strings.HasPrefix(reqPath, cookiePath) {
		if strings.HasSuffix(cookiePath, "/") {
			return true
		}

		if len(reqPath) > len(cookiePath) && reqPath[len(cookiePath)] == '/' {
			return true
		}
	}

	return false
}

// SortForBrowser sorts cookies in-place per RFC 6265 §5.4 (longest path length first).
func SortForBrowser(cookies []*http.Cookie) {
	if len(cookies) <= 1 {
		return
	}

	slices.SortStableFunc(cookies, func(a, b *http.Cookie) int {
		return len(b.Path) - len(a.Path)
	})
}

// BuildCookieHeader constructs an RFC 6265 compliant Cookie request header string.
func BuildCookieHeader(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	var (
		stackBuf [16]*http.Cookie
		sorted   []*http.Cookie
	)

	if len(cookies) <= len(stackBuf) {
		sorted = stackBuf[:len(cookies)]
		copy(sorted, cookies)
	} else {
		sorted = slices.Clone(cookies)
	}

	SortForBrowser(sorted)

	var sb strings.Builder
	sb.Grow(len(sorted) * 36)

	for i, c := range sorted {
		if i > 0 {
			sb.WriteString("; ")
		}

		sb.WriteString(c.Name)
		sb.WriteByte('=')
		sb.WriteString(c.Value)
	}

	return sb.String()
}

// ExportNetscape exports cookies as Netscape format string.
func ExportNetscape(cookies []*http.Cookie, defaultHost string) string {
	if len(cookies) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(len(cookies) * 80)
	sb.WriteString("# Netscape HTTP Cookie File\n\n")

	var numBuf [20]byte

	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			domain = defaultHost
		}

		includeSubdomains := "FALSE"
		if len(domain) > 0 && domain[0] == '.' {
			includeSubdomains = "TRUE"
		}

		path := c.Path
		if path == "" {
			path = "/"
		}

		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}

		expires := "0"
		if !c.Expires.IsZero() {
			b := strconv.AppendInt(numBuf[:0], c.Expires.Unix(), 10)
			expires = bytesconv.B2S(b)
		}

		sb.WriteString(domain)
		sb.WriteByte('\t')
		sb.WriteString(includeSubdomains)
		sb.WriteByte('\t')
		sb.WriteString(path)
		sb.WriteByte('\t')
		sb.WriteString(secure)
		sb.WriteByte('\t')
		sb.WriteString(expires)
		sb.WriteByte('\t')
		sb.WriteString(c.Name)
		sb.WriteByte('\t')
		sb.WriteString(c.Value)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// ParseSingleCookie parses key and value bytes into an http.Cookie pointer.
func ParseSingleCookie(_, value []byte) *http.Cookie {
	header := http.Header{}
	header.Add("Set-Cookie", bytesconv.B2S(value))

	fakeResp := &http.Response{Header: header}

	parsed := fakeResp.Cookies()
	if len(parsed) > 0 {
		return parsed[0]
	}

	return nil
}

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

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// MaxCookieAgeSeconds defines the maximum recommended cookie lifetime in seconds (400 days / 34,560,000s)
// as mandated by RFC 6265bis §5.5.
const (
	MaxCookieAgeSeconds = 34560000
	MaxCookieAgeLimit   = 400 * 24 * time.Hour
)

// Cookie represents an HTTP cookie structure, capturing attributes from Set-Cookie headers
// and formatted for storage and persistence (RFC 6265 §4.1.1, §4.1.2 & §5.3, RFC 6265bis §5.7).
type Cookie struct {
	Expires      time.Time `json:"expires,omitempty"`
	Name         string    `json:"name"`
	Value        string    `json:"value"`
	Domain       string    `json:"domain"`
	Path         string    `json:"path"`
	SameSite     string    `json:"sameSite,omitempty"`
	PartitionKey string    `json:"partitionKey,omitempty"`
	HTTPOnly     bool      `json:"httpOnly,omitempty"`
	Secure       bool      `json:"secure,omitempty"`
	Partitioned  bool      `json:"partitioned,omitempty"`
	MaxAge       int       `json:"maxAge,omitempty"`
}

// CookieDTO is an alias for [Cookie] provided for backwards compatibility.
type CookieDTO = Cookie

// hasProhibitedControlChars reports whether s contains CTL characters %x00-08 / %x0A-1F / %x7F (excluding HTAB %x09)
// per RFC 6265bis §5.5 Step 1 & §5.7 Step 3.
func hasProhibitedControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b <= 0x08) || (b >= 0x0A && b <= 0x1F) || b == 0x7F {
			return true
		}
	}

	return false
}

// ParseSetCookieHeader parses a raw Set-Cookie header line with zero heap allocations (RFC 6265 §5.2, RFC 6265bis §5.5 & §5.7).
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	if headerVal == "" || hasProhibitedControlChars(headerVal) {
		return Cookie{}
	}

	c := Cookie{
		Domain: defaultDomain,
		Path:   defaultPath,
	}

	isFirst := true
	for key, val := range bytesconv.ScanPairs(headerVal, ';', '=') {
		if isFirst {
			c.Name = key
			c.Value = val
			isFirst = false

			continue
		}

		ParseCookieAttribute(key, val, &c)
	}

	// RFC 6265bis §5.5 Step 5 & §5.7 Step 4: Sum of lengths of name and value must not exceed 4096 octets
	if c.Name == "" || len(c.Name)+len(c.Value) > 4096 {
		return Cookie{}
	}

	return c
}

// ParseCookieAttribute sets the corresponding field on [CookieDTO] with zero heap allocations using case-insensitive ASCII comparison (RFC 6265 §5.2.1–§5.2.6, RFC 6265bis §5.5 & §5.6).
func ParseCookieAttribute(key, val string, c *CookieDTO) {
	hasVal := len(val) > 0

	// RFC 6265bis §5.5 Step 6: Attribute value longer than 1024 octets must be ignored
	if len(val) > 1024 {
		return
	}

	switch {
	case bytesconv.EqualFoldASCII(key, "httponly"):
		// RFC 6265 §5.2.6 & RFC 6265bis §5.6.6: The HttpOnly Attribute
		c.HTTPOnly = true
	case bytesconv.EqualFoldASCII(key, "secure"):
		// RFC 6265 §5.2.5 & RFC 6265bis §5.6.5: The Secure Attribute
		c.Secure = true
	case bytesconv.EqualFoldASCII(key, "partitioned"):
		// RFC 6265bis: CHIPS Partitioned attribute
		c.Partitioned = true
	case bytesconv.EqualFoldASCII(key, "samesite"):
		// RFC 6265bis §4.1.2.7 & §5.6.7: SameSite attribute
		if hasVal {
			switch {
			case bytesconv.EqualFoldASCII(val, "strict"):
				c.SameSite = "Strict"
			case bytesconv.EqualFoldASCII(val, "lax"):
				c.SameSite = "Lax"
			case bytesconv.EqualFoldASCII(val, "none"):
				c.SameSite = "None"
			default:
				c.SameSite = "Default"
			}
		}

	case bytesconv.EqualFoldASCII(key, "domain"):
		// RFC 6265 §5.2.3 & RFC 6265bis §5.6.3: The Domain Attribute (strip leading dot)
		if hasVal {
			c.Domain = strings.TrimPrefix(val, ".")
		}
	case bytesconv.EqualFoldASCII(key, "path"):
		// RFC 6265 §5.2.4 & RFC 6265bis §5.6.4: The Path Attribute
		if hasVal {
			c.Path = val
		}
	case bytesconv.EqualFoldASCII(key, "max-age"):
		// RFC 6265 §5.2.2 & RFC 6265bis §5.5 & §5.6.2: The Max-Age Attribute
		if hasVal {
			if maxAge, err := strconv.Atoi(val); err == nil {
				// RFC 6265bis §5.5 & §5.6.2 step 6: Clamp lifetime to at most 400 days (34,560,000s)
				if maxAge > MaxCookieAgeSeconds {
					maxAge = MaxCookieAgeSeconds
				}

				c.MaxAge = maxAge
			}
		}

	case bytesconv.EqualFoldASCII(key, "expires"):
		// RFC 6265 §5.2.1 & RFC 6265bis §5.6.1: The Expires Attribute
		if hasVal {
			if exp, err := http.ParseTime(val); err == nil {
				c.Expires = exp
			}
		}
	}
}

// ValidatePrefix verifies whether cookie conforms to RFC 6265bis §4.1.3 & §5.4 cookie prefix rules:
//   - "__Secure-": MUST be set with Secure=true.
//   - "__Host-": MUST be set with Secure=true, Path="/", and empty Domain (host-only).
//   - Nameless cookies whose value begins with "__Secure-" or "__Host-" MUST be rejected (RFC 6265bis §5.7 step 22).
func ValidatePrefix(c Cookie) bool {
	if c.Name == "" {
		lowerVal := strings.ToLower(c.Value)
		if strings.HasPrefix(lowerVal, "__secure-") || strings.HasPrefix(lowerVal, "__host-") {
			return false // RFC 6265bis §5.7 step 22
		}

		return true
	}

	lowerName := strings.ToLower(c.Name)
	if strings.HasPrefix(lowerName, "__secure-") {
		// RFC 6265bis §4.1.3.1, §5.4 & §5.7 step 20: __Secure- prefix requires Secure=true
		return c.Secure
	}

	if strings.HasPrefix(lowerName, "__host-") {
		// RFC 6265bis §4.1.3.2, §5.4 & §5.7 step 21: __Host- prefix requires Secure=true, Path="/", and host-only (Domain="")
		return c.Secure && c.Path == "/" && c.Domain == ""
	}

	return true
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

// BuildCookieHeader constructs an RFC 6265 compliant Cookie request header string (RFC 6265 §4.2.1 & §5.4).
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

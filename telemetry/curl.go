// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package telemetry

import (
	"fmt"
	"maps"
	"net/http"
	"strings"
)

// CurlOptions controls formatting, redaction, and cookie extraction for cURL generation.
type CurlOptions struct {
	RedactHeaders []string
	RedactSecret  string
}

const defaultRedacted = "*****REDACTED*****"

var defaultSensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
	"token":               true,
	"secret":              true,
	"set-cookie":          true,
}

// CurlFromRequest converts an [http.Request] and optional body payload into a clean, shell-escaped cURL command string.
func CurlFromRequest(req *http.Request, body []byte) string {
	return CurlFromRequestWithOptions(req, body, nil)
}

// CurlFromRequestWithOptions converts an [http.Request] with custom [CurlOptions] into a cURL command string.
func CurlFromRequestWithOptions(req *http.Request, body []byte, opts *CurlOptions) string {
	if req == nil {
		return "curl"
	}

	var sb strings.Builder
	sb.Grow(512)

	sb.WriteString("curl")

	if req.Method != "" && req.Method != http.MethodGet {
		sb.WriteString(" -X ")
		sb.WriteString(req.Method)
	}

	sensitive := defaultSensitiveHeaders
	redactPlaceholder := defaultRedacted

	if opts != nil {
		if len(opts.RedactHeaders) > 0 {
			sensitive = make(map[string]bool, len(defaultSensitiveHeaders)+len(opts.RedactHeaders))
			maps.Copy(sensitive, defaultSensitiveHeaders)

			for _, h := range opts.RedactHeaders {
				sensitive[strings.ToLower(h)] = true
			}
		}

		if opts.RedactSecret != "" {
			redactPlaceholder = opts.RedactSecret
		}
	}

	// 1. Process Request Headers
	hasCookieHeader := false
	for key, values := range req.Header {
		lowerKey := strings.ToLower(key)
		if lowerKey == "cookie" {
			hasCookieHeader = true
		}

		isSecret := sensitive[lowerKey]
		for _, val := range values {
			outVal := val
			if isSecret {
				outVal = redactPlaceholder
			}

			headerStr := fmt.Sprintf("%s: %s", key, outVal)

			sb.WriteString(" -H ")
			sb.WriteString(escapeShell(headerStr))
		}
	}

	// 2. Process CookieJar / Cookies if Cookie header was not explicitly set
	if !hasCookieHeader {
		cookies := req.Cookies()
		if len(cookies) > 0 {
			var cookieSb strings.Builder
			for i, c := range cookies {
				if i > 0 {
					cookieSb.WriteString("; ")
				}

				cookieSb.WriteString(c.Name)
				cookieSb.WriteString("=")

				if sensitive["cookie"] {
					cookieSb.WriteString(redactPlaceholder)
				} else {
					cookieSb.WriteString(c.Value)
				}
			}

			sb.WriteString(" -H ")
			sb.WriteString(escapeShell("Cookie: " + cookieSb.String()))
		}
	}

	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") && len(body) > 0 {
		summary := SummarizeMultipartBody(body, contentType)

		for part := range strings.SplitSeq(summary, "&") {
			sb.WriteString(" -F ")
			sb.WriteString(escapeShell(part))
		}
	} else if len(body) > 0 {
		sb.WriteString(" -d ")
		sb.WriteString(escapeShell(string(body)))
	}

	// 4. Target URL
	if req.URL != nil {
		sb.WriteString(" ")
		sb.WriteString(escapeShell(req.URL.String()))
	}

	return sb.String()
}

// isShellSafeByte returns true if b is safe in a POSIX shell command without quoting.
func isShellSafeByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.' || b == '/' ||
		b == ':' || b == '=' || b == '?' || b == '&' ||
		b == '%' || b == '+' || b == ',' || b == '@' || b == '~'
}

// escapeShell returns s as a shell-safe string.
// Clean strings are returned as-is, while strings with spaces/special characters are wrapped in single quotes.
func escapeShell(s string) string {
	if s == "" {
		return "''"
	}

	safe := true
	for i := 0; i < len(s); i++ {
		if !isShellSafeByte(s[i]) {
			safe = false
			break
		}
	}

	if safe {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s) + 8)
	sb.WriteByte('\'')

	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			sb.WriteString("'\\''")
		} else {
			sb.WriteByte(s[i])
		}
	}

	sb.WriteByte('\'')

	return sb.String()
}

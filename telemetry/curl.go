// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package telemetry provides observability, diagnostic logging, HAR exports, and latency tracking utilities.
package telemetry

import (
	"net/http"
	"strings"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// CurlOptions controls header redaction and secrets masking for cURL command generation.
type CurlOptions struct {
	RedactHeaders []string
	RedactSecret  string
}

const defaultRedacted = "*****REDACTED*****"

var defaultSensitiveHeaders = []string{
	"authorization",
	"proxy-authorization",
	"x-api-key",
	"api-key",
	"token",
	"secret",
	"set-cookie",
}

// CurlFromRequest converts an [*http.Request] and optional body payload into a shell-escaped cURL command.
func CurlFromRequest(req *http.Request, body []byte) string {
	return CurlFromRequestWithOptions(req, body, nil)
}

// CurlFromRequestWithOptions converts an [*http.Request] into cURL applying custom [CurlOptions].
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

	redactSecret := resolveRedactSecret(opts)
	sensitiveList := resolveSensitiveHeaders(opts)

	hasCookieHeader := false
	for key, values := range req.Header {
		if bytesconv.EqualFoldASCII(key, fheader.Cookie) {
			hasCookieHeader = true
		}

		isSecret := isHeaderSensitive(key, sensitiveList)
		for _, val := range values {
			outVal := val
			if isSecret {
				outVal = redactSecret
			}

			sb.WriteString(" -H ")
			sb.WriteString(escapeShell(key + ": " + outVal))
		}
	}

	if !hasCookieHeader {
		appendJarCookies(&sb, req.Cookies(), isHeaderSensitive(fheader.Cookie, sensitiveList), redactSecret)
	}

	appendBodyAndURL(&sb, req, body)

	return sb.String()
}

// resolveRedactSecret retrieves the masking placeholder string from opts.
func resolveRedactSecret(opts *CurlOptions) string {
	if opts != nil && opts.RedactSecret != "" {
		return opts.RedactSecret
	}

	return defaultRedacted
}

// resolveSensitiveHeaders combines default sensitive header names with any user overrides.
func resolveSensitiveHeaders(opts *CurlOptions) []string {
	if opts == nil || len(opts.RedactHeaders) == 0 {
		return defaultSensitiveHeaders
	}

	result := make([]string, len(defaultSensitiveHeaders), len(defaultSensitiveHeaders)+len(opts.RedactHeaders))
	copy(result, defaultSensitiveHeaders)

	return append(result, opts.RedactHeaders...)
}

// isHeaderSensitive checks whether key matches any header name in sensitiveList.
func isHeaderSensitive(key string, sensitiveList []string) bool {
	for _, target := range sensitiveList {
		if bytesconv.EqualFoldASCII(key, target) {
			return true
		}
	}

	return false
}

// appendJarCookies appends parsed cookie values to the cURL header string builder.
func appendJarCookies(sb *strings.Builder, cookies []*http.Cookie, isSecret bool, redactSecret string) {
	if len(cookies) == 0 {
		return
	}

	var cookieSb strings.Builder
	for i, c := range cookies {
		if i > 0 {
			cookieSb.WriteString("; ")
		}

		cookieSb.WriteString(c.Name)
		cookieSb.WriteByte('=')

		if isSecret {
			cookieSb.WriteString(redactSecret)
		} else {
			cookieSb.WriteString(c.Value)
		}
	}

	sb.WriteString(" -H ")
	sb.WriteString(escapeShell(fheader.Cookie + ": " + cookieSb.String()))
}

// appendBodyAndURL formats request body payload and destination URL onto sb.
func appendBodyAndURL(sb *strings.Builder, req *http.Request, body []byte) {
	contentType := req.Header.Get(fheader.ContentType)
	if len(contentType) >= 19 && bytesconv.EqualFoldASCII(contentType[:19], fheader.MIMEMultipartFormData) && len(body) > 0 {
		summary := SummarizeMultipartBody(body, contentType)
		for part := range strings.SplitSeq(summary, "&") {
			sb.WriteString(" -F ")
			sb.WriteString(escapeShell(part))
		}
	} else if len(body) > 0 {
		sb.WriteString(" -d ")
		sb.WriteString(escapeShell(bytesconv.B2S(body)))
	}

	if req.URL != nil {
		sb.WriteByte(' ')
		sb.WriteString(escapeShell(req.URL.String()))
	}
}

// isShellSafeByte reports whether byte b requires single-quote escaping in POSIX shells.
func isShellSafeByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.' || b == '/' ||
		b == ':' || b == '=' || b == '?' || b == '&' ||
		b == '%' || b == '+' || b == ',' || b == '@' || b == '~'
}

// escapeShell escapes a string for safe inclusion in POSIX shell command lines.
func escapeShell(s string) string {
	if s == "" {
		return "''"
	}

	safe := true
	for i := range s {
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

	for i := range s {
		if s[i] == '\'' {
			sb.WriteString("'\\''")
		} else {
			sb.WriteByte(s[i])
		}
	}

	sb.WriteByte('\'')

	return sb.String()
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package requestutil provides utility functions for HTTP requests.
package requestutil

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

var sensitiveHeaderStrings = [...]string{
	"authorization",
	"cookie",
	"set-cookie",
	"proxy-authorization",
}

// RedactHeaders redacts sensitive values from raw HTTP dump bytes.
func RedactHeaders(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	var buf bytes.Buffer
	buf.Grow(len(raw))

	lines := bytes.Split(raw, []byte("\r\n"))
	for i, line := range lines {
		if i > 0 {
			buf.Write([]byte("\r\n"))
		}

		key, _, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			buf.Write(line)
			continue
		}

		trimmedKey := bytes.TrimSpace(key)
		if IsSensitiveHeader(trimmedKey) {
			buf.Write(bytes.ToLower(trimmedKey))
			buf.WriteString(": <redacted>")
		} else {
			buf.Write(line)
		}
	}

	return buf.Bytes()
}

// IsSensitiveHeader checks if key matches standard sensitive HTTP header names.
func IsSensitiveHeader(key []byte) bool {
	keyStr := bytesconv.B2S(key)
	for _, target := range sensitiveHeaderStrings {
		if bytesconv.EqualFoldASCII(keyStr, target) {
			return true
		}
	}

	return false
}

// FindFirstNonWhitespaceByte returns the first non-whitespace byte in b.
func FindFirstNonWhitespaceByte(b []byte) byte {
	n := len(b)
	if n == 0 {
		return 0
	}

	_ = b[n-1]

	for i := 0; i < n; i++ {
		ch := b[i]
		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			return ch
		}
	}

	return 0
}

// IsCloudflareChallengeBytes reports whether lower HTML bytes contain Cloudflare challenge signatures.
func IsCloudflareChallengeBytes(lower []byte) bool {
	return bytes.Contains(lower, []byte("cf-challenge")) ||
		bytes.Contains(lower, []byte("ray id")) ||
		bytes.Contains(lower, []byte("cloudflare"))
}

// FormatGRPCTimeout converts d into a PROTOCOL-HTTP2.md compliant "grpc-timeout" header string.
func FormatGRPCTimeout(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}

	switch {
	case d < time.Microsecond:
		return strconv.FormatInt(d.Nanoseconds(), 10) + "n"
	case d < time.Millisecond:
		return strconv.FormatInt(d.Microseconds(), 10) + "u"
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "m"
	case d < time.Minute:
		return strconv.FormatInt(int64(d.Seconds()), 10) + "S"
	case d < time.Hour:
		return strconv.FormatInt(int64(d.Minutes()), 10) + "M"
	default:
		return strconv.FormatInt(int64(d.Hours()), 10) + "H"
	}
}

// IsIdempotentMethod reports whether the HTTP method is idempotent according to RFC 9110 §9.2.2.
func IsIdempotentMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

// IsSafeMethod reports whether the HTTP method is safe (read-only) according to RFC 9110 §9.2.1.
func IsSafeMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

// TruncateBody limits output payload representations to maxBytes without unnecessary allocations.
func TruncateBody(body []byte, maxBytes int) string {
	limit := maxBytes
	if limit <= 0 {
		limit = 4096
	}

	if len(body) <= limit {
		return bytesconv.B2S(body)
	}

	var numBuf [20]byte

	truncatedBytes := strconv.AppendInt(numBuf[:0], int64(len(body)-limit), 10)

	var sb strings.Builder
	sb.Grow(limit + 32 + len(truncatedBytes))
	sb.WriteString(bytesconv.B2S(body[:limit]))
	sb.WriteString("... [truncated ")
	sb.Write(truncatedBytes)
	sb.WriteString(" bytes]")

	return sb.String()
}

// IsStreamingResponse detects whether an HTTP response represents a real-time stream (SSE, NDJSON, Chunked).
func IsStreamingResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}

	contentType := strings.ToLower(resp.Header.Get(fheader.ContentType))
	streamingTypes := [...]string{
		fheader.MIMETextEventStream,
		"application/stream",
		"application/x-ndjson",
		"application/x-stream",
		"text/stream",
	}

	for _, st := range streamingTypes {
		if strings.Contains(contentType, st) {
			return true
		}
	}

	chunked := strings.Contains(strings.ToLower(resp.Header.Get(fheader.TransferEncoding)), "chunked")

	return strings.Contains(contentType, "text/plain") && chunked && resp.ContentLength == -1
}

// SummarizeMultipartBody extracts form field names and file metadata from a multipart/form-data payload (RFC 7578 §4.1–§4.2).
func SummarizeMultipartBody(body []byte, contentType string) string {
	if len(body) == 0 || contentType == "" {
		return ""
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return "(multipart/form-data payload)"
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])

	var parts []string

	for {
		part, err := reader.NextPart()
		if err == io.EOF || err != nil {
			break
		}

		name := part.FormName()
		if name == "" {
			continue
		}

		if filename := part.FileName(); filename != "" {
			parts = append(parts, name+"=@"+filename)
			continue
		}

		var sb strings.Builder
		if _, err := io.Copy(&sb, part); err == nil {
			parts = append(parts, name+"="+sb.String())
		}
	}

	if len(parts) == 0 {
		return "(multipart/form-data payload)"
	}

	return strings.Join(parts, "&")
}

// HeaderContainsToken reports whether any header value for name matches target token (comma-separated, case-insensitive).
func HeaderContainsToken(header http.Header, name, target string) bool {
	for _, s := range header[name] {
		for token := range bytesconv.ScanTokens(s, ',') {
			if bytesconv.EqualFoldASCII(token, target) {
				return true
			}
		}
	}

	return false
}

// CanonicalHeaderKeyBytes converts header key b to MIME canonical format in-place on a 64-byte stack array without heap allocations.
//
//go:inline
func CanonicalHeaderKeyBytes(src []byte) []byte {
	n := len(src)
	if n == 0 {
		return nil
	}

	var (
		buf [64]byte
		out []byte
	)

	if n <= 64 {
		out = buf[:n]
	} else {
		out = make([]byte, n)
	}

	upper := true
	_ = src[n-1]
	_ = out[n-1]

	for i := 0; i < n; i++ {
		c := src[i]
		if upper && 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		} else if !upper && 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}

		out[i] = c
		upper = (c == '-')
	}

	return out
}

// CanonicalHeaderKey converts header key string s to MIME canonical format in O(1) time for common headers.
//
//go:inline
func CanonicalHeaderKey(s string) string {
	if len(s) == 0 {
		return ""
	}

	switch s {
	case "Content-Type", "content-type", "CONTENT-TYPE":
		return fheader.ContentType
	case "Content-Length", "content-length", "CONTENT-LENGTH":
		return fheader.ContentLength
	case "Server", "server", "SERVER":
		return fheader.Server
	case "Date", "date", "DATE":
		return fheader.Date
	case "Set-Cookie", "set-cookie", "SET-COOKIE":
		return fheader.SetCookie
	case "Location", "location", "LOCATION":
		return fheader.Location
	case "Connection", "connection", "CONNECTION":
		return fheader.Connection
	case "Cache-Control", "cache-control", "CACHE-CONTROL":
		return fheader.CacheControl
	case "Accept", "accept", "ACCEPT":
		return fheader.Accept
	case "Accept-Encoding", "accept-encoding", "ACCEPT-ENCODING":
		return fheader.AcceptEncoding
	case "Authorization", "authorization", "AUTHORIZATION":
		return fheader.Authorization
	case "User-Agent", "user-agent", "USER-AGENT":
		return fheader.UserAgent
	case "Transfer-Encoding", "transfer-encoding", "TRANSFER-ENCODING":
		return fheader.TransferEncoding
	case "Keep-Alive", "keep-alive", "KEEP-ALIVE":
		return "Keep-Alive"
	case "ETag", "etag", "ETAG":
		return fheader.ETag
	case "Host", "host", "HOST":
		return fheader.Host
	}

	b := CanonicalHeaderKeyBytes(bytesconv.S2B(s))
	if len(b) == 0 {
		return ""
	}

	return string(b)
}

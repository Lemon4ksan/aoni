// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package requestutil provides utility functions for HTTP requests.
package requestutil

import (
	"bytes"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

var sensitiveHeaderBytes = [][]byte{
	[]byte("authorization"),
	[]byte("cookie"),
	[]byte("set-cookie"),
	[]byte("proxy-authorization"),
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
	for _, target := range sensitiveHeaderBytes {
		if bytesconv.EqualFoldASCII(keyStr, bytesconv.B2S(target)) {
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

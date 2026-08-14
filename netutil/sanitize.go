// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netutil provides network layer security helpers, filename sanitization, and transport utilities.
package netutil

import (
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
)

// SanitizeFileName cleans a string by stripping path traversal sequences, null bytes,
// control characters, and Windows reserved device names per RFC 6266 Section 4.3.
func SanitizeFileName(filename string) string {
	var sb strings.Builder
	sb.Grow(len(filename))

	for i := 0; i < len(filename); i++ {
		b := filename[i]
		if b >= 32 && b != 127 {
			sb.WriteByte(b)
		}
	}

	filename = filepath.Base(filepath.Clean(strings.TrimSpace(sb.String())))

	for strings.HasPrefix(filename, "..") || strings.HasPrefix(filename, ".") {
		filename = strings.TrimPrefix(filename, "..")
		filename = strings.TrimPrefix(filename, ".")
	}

	filename = strings.TrimSpace(filename)

	if isWindowsReservedDeviceName(filename) || filename == "" || filename == "." || filename == "/" ||
		filename == "\\" {
		return "downloaded_file"
	}

	return filename
}

// ExtractSanitizedFilename extracts, RFC 8187-decodes, and cleans the filename parameter
// from a Content-Disposition HTTP header according to RFC 6266 Section 4.3.
func ExtractSanitizedFilename(contentDispositionHeader string) string {
	if contentDispositionHeader == "" {
		return "downloaded_file"
	}

	_, params, err := mime.ParseMediaType(contentDispositionHeader)
	if err != nil {
		return "downloaded_file"
	}

	filename, ok := params["filename*"]
	if ok && filename != "" {
		filename = decodeRFC8187(filename)
	} else {
		filename = params["filename"]
	}

	if filename == "" {
		return "downloaded_file"
	}

	return SanitizeFileName(filename)
}

// decodeRFC8187 decodes RFC 8187 encoded string ("charset'lang'encoded-value").
func decodeRFC8187(extValue string) string {
	firstQuote := strings.IndexByte(extValue, '\'')
	if firstQuote == -1 {
		return extValue
	}

	secondQuote := strings.IndexByte(extValue[firstQuote+1:], '\'')
	if secondQuote == -1 {
		return extValue
	}

	secondQuote += firstQuote + 1

	charset := strings.ToLower(strings.TrimSpace(extValue[:firstQuote]))
	// language := extValue[firstQuote+1 : secondQuote] // Language tag is optional and ignored for file names
	rawEncoded := extValue[secondQuote+1:]

	unescaped, err := url.PathUnescape(rawEncoded)
	if err != nil {
		unescaped = rawEncoded
	}

	switch charset {
	case "utf-8", "utf8", "":
		if utf8.ValidString(unescaped) {
			return unescaped
		}

		return strings.ToValidUTF8(unescaped, "")

	case "iso-8859-1", "latin1":
		return iso88591ToUTF8(unescaped)

	default:
		if enc, err := htmlindex.Get(charset); err == nil {
			if decoded, err := enc.NewDecoder().String(unescaped); err == nil {
				return decoded
			}
		}

		if utf8.ValidString(unescaped) {
			return unescaped
		}

		return strings.ToValidUTF8(unescaped, "")
	}
}

// iso88591ToUTF8 translates ISO-8859-1 raw bytes into UTF-8 representation.
func iso88591ToUTF8(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) * 2)

	for i := 0; i < len(s); i++ {
		buf.WriteRune(rune(s[i]))
	}

	return buf.String()
}

// isWindowsReservedDeviceName checks whether filename stem conflicts with Win32 legacy DOS devices.
func isWindowsReservedDeviceName(filename string) bool {
	ext := filepath.Ext(filename)
	stem := strings.ToUpper(strings.TrimSuffix(filename, ext))

	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}

	return false
}

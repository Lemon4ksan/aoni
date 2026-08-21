// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sanitize implements Content-Disposition header parsing, RFC 8187 parameter encoding/decoding,
// and safe filename sanitization strictly conforming to RFC 6266 and RFC 8187.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/contentdisposition].
package sanitize

import (
	fcd "github.com/lemon4ksan/foundation/net/http/contentdisposition"
)

// Standard Content-Disposition disposition types per RFC 6266 §4.2 and RFC 7578.
const (
	DispositionInline     = fcd.DispositionInline
	DispositionAttachment = fcd.DispositionAttachment
	DispositionFormData   = fcd.DispositionFormData
)

// ContentDisposition represents a parsed HTTP Content-Disposition header according to RFC 6266.
type ContentDisposition = fcd.ContentDisposition

// FileName cleans a string by stripping path traversal sequences, null bytes,
// control characters, and Windows reserved device names per RFC 6266 §4.3.
func FileName(filename string) string {
	return fcd.FileName(filename)
}

// ParseContentDisposition parses a Content-Disposition header string per RFC 6266 §4.1–§4.4.
func ParseContentDisposition(contentDispositionHeader string) ContentDisposition {
	return fcd.ParseContentDisposition(contentDispositionHeader)
}

// ExtractFilename extracts, RFC 8187-decodes, and cleans the filename parameter.
func ExtractFilename(contentDispositionHeader string) string {
	return fcd.ExtractFilename(contentDispositionHeader)
}

// FormatContentDisposition constructs an RFC 6266 compliant Content-Disposition header value.
func FormatContentDisposition(dispType, filename string) string {
	return fcd.FormatContentDisposition(dispType, filename)
}

// IsRFC8187AttrChar reports whether b is a valid RFC 8187 attr-char (RFC 8187 §3.2.1).
func IsRFC8187AttrChar(b byte) bool {
	return fcd.IsRFC8187AttrChar(b)
}

// EncodeRFC8187 encodes value into RFC 8187 extended parameter value notation ("UTF-8'lang'encoded-value").
func EncodeRFC8187(value, language string) string {
	return fcd.EncodeRFC8187(value, language)
}

// DecodeRFC8187 decodes an RFC 8187 extended parameter value string.
func DecodeRFC8187(extValue string) (charset, language, value string, err error) {
	return fcd.DecodeRFC8187(extValue)
}

// DecodeRFC8187Value decodes an RFC 8187 extended parameter value string.
func DecodeRFC8187Value(extValue string) string {
	return fcd.DecodeRFC8187Value(extValue)
}

// ISO88591ToUTF8 translates ISO-8859-1 raw bytes into UTF-8 representation.
func ISO88591ToUTF8(s string) string {
	return fcd.ISO88591ToUTF8(s)
}

// IsWindowsReservedName checks whether filename stem conflicts with Win32 legacy DOS devices.
func IsWindowsReservedName(filename string) bool {
	return fcd.IsWindowsReservedName(filename)
}

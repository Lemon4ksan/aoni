// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"github.com/lemon4ksan/aoni/netutil/sanitize"
)

// Standard Content-Disposition disposition types per RFC 6266 §4.2 and RFC 7578.
const (
	// DispositionInline indicates default in-place processing and rendering of the response (RFC 6266 §4.2).
	DispositionInline = sanitize.DispositionInline

	// DispositionAttachment prompts the user to save the response locally ("Save As...") (RFC 6266 §4.2).
	DispositionAttachment = sanitize.DispositionAttachment

	// DispositionFormData denotes a form field payload in multipart/form-data bodies (RFC 7578).
	DispositionFormData = sanitize.DispositionFormData
)

// ContentDisposition represents a parsed HTTP Content-Disposition header according to RFC 6266.
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/sanitize].
type ContentDisposition = sanitize.ContentDisposition

// SanitizeFileName cleans a string by stripping path traversal sequences, null bytes,
// control characters, and Windows reserved device names per RFC 6266 §4.3.
func SanitizeFileName(filename string) string {
	return sanitize.FileName(filename)
}

// ParseContentDisposition parses a Content-Disposition header string per RFC 6266 §4.1–§4.4.
func ParseContentDisposition(contentDispositionHeader string) ContentDisposition {
	return sanitize.ParseContentDisposition(contentDispositionHeader)
}

// ExtractSanitizedFilename extracts, RFC 8187-decodes, and cleans the filename parameter
// from a Content-Disposition HTTP header according to RFC 6266 §4.3.
func ExtractSanitizedFilename(contentDispositionHeader string) string {
	return sanitize.ExtractFilename(contentDispositionHeader)
}

// FormatContentDisposition constructs an RFC 6266 compliant Content-Disposition header value (RFC 6266 Appendix D).
func FormatContentDisposition(dispType, filename string) string {
	return sanitize.FormatContentDisposition(dispType, filename)
}

// IsRFC8187AttrChar reports whether b is a valid RFC 8187 attr-char (RFC 8187 §3.2.1).
func IsRFC8187AttrChar(b byte) bool {
	return sanitize.IsRFC8187AttrChar(b)
}

// EncodeRFC8187 encodes value into RFC 8187 extended parameter value notation (RFC 8187 §3.2.1).
func EncodeRFC8187(value, language string) string {
	return sanitize.EncodeRFC8187(value, language)
}

// DecodeRFC8187 decodes an RFC 8187 extended parameter value string into charset, language, and value (RFC 8187 §3.2.1).
func DecodeRFC8187(extValue string) (charset, language, value string, err error) {
	return sanitize.DecodeRFC8187(extValue)
}

// DecodeRFC8187Value decodes an RFC 8187 extended parameter value string, returning the decoded text value (RFC 8187 §3.2.1 & §4.2).
func DecodeRFC8187Value(extValue string) string {
	return sanitize.DecodeRFC8187Value(extValue)
}

// ISO88591ToUTF8 translates ISO-8859-1 raw bytes into UTF-8 representation.
func ISO88591ToUTF8(s string) string {
	return sanitize.ISO88591ToUTF8(s)
}

// IsWindowsReservedName checks whether filename stem conflicts with Win32 legacy DOS devices (RFC 6266 §4.3).
func IsWindowsReservedName(filename string) bool {
	return sanitize.IsWindowsReservedName(filename)
}

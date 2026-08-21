// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sanitize implements Content-Disposition header parsing, RFC 8187 parameter encoding/decoding,
// and safe filename sanitization strictly conforming to RFC 6266 and RFC 8187.
package sanitize

import (
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lemon4ksan/foundation/text/encoding/htmlindex"
)

// Standard Content-Disposition disposition types per RFC 6266 §4.2 and RFC 7578.
const (
	// DispositionInline indicates default in-place processing and rendering of the response (RFC 6266 §4.2).
	DispositionInline = "inline"

	// DispositionAttachment prompts the user to save the response locally ("Save As...") (RFC 6266 §4.2).
	DispositionAttachment = "attachment"

	// DispositionFormData denotes a form field payload in multipart/form-data bodies (RFC 7578).
	DispositionFormData = "form-data"
)

// ContentDisposition represents a parsed HTTP Content-Disposition header according to RFC 6266.
type ContentDisposition struct {
	// Type is the disposition type (e.g., "inline", "attachment", or extension token) per RFC 6266 §4.2.
	Type string

	// Filename is the extracted, decoded, and sanitized filename per RFC 6266 §4.3.
	Filename string

	// Parameters holds additional disposition parameters per RFC 6266 §4.4.
	Parameters map[string]string
}

// FileName cleans a string by stripping path traversal sequences, null bytes,
// control characters, and Windows reserved device names per RFC 6266 §4.3.
func FileName(filename string) string {
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

	if IsWindowsReservedName(filename) || filename == "" || filename == "." || filename == "/" ||
		filename == "\\" {
		return "downloaded_file"
	}

	return filename
}

// ParseContentDisposition parses a Content-Disposition header string per RFC 6266 §4.1–§4.4.
// Unknown or unhandled disposition types are normalized to "attachment" (RFC 6266 §4.2).
// When both "filename*" and "filename" parameters are present, "filename*" takes precedence (RFC 6266 §4.3).
func ParseContentDisposition(contentDispositionHeader string) ContentDisposition {
	if contentDispositionHeader == "" {
		return ContentDisposition{
			Type:       DispositionAttachment,
			Filename:   "downloaded_file",
			Parameters: nil,
		}
	}

	dispType, params, err := mime.ParseMediaType(contentDispositionHeader)
	if err != nil {
		return ContentDisposition{
			Type:       DispositionAttachment,
			Filename:   "downloaded_file",
			Parameters: nil,
		}
	}

	dispType = strings.ToLower(strings.TrimSpace(dispType))
	if dispType == "" {
		dispType = DispositionAttachment
	}

	var filename string
	if extFn, ok := params["filename*"]; ok && extFn != "" {
		// RFC 6266 §4.3: Pick filename* over filename when both are present
		filename = DecodeRFC8187Value(extFn)
	} else if stdFn, ok := params["filename"]; ok {
		filename = stdFn
	}

	sanitized := "downloaded_file"
	if filename != "" {
		sanitized = FileName(filename)
	}

	return ContentDisposition{
		Type:       dispType,
		Filename:   sanitized,
		Parameters: params,
	}
}

// ExtractFilename extracts, RFC 8187-decodes, and cleans the filename parameter
// from a Content-Disposition HTTP header according to RFC 6266 §4.3.
func ExtractFilename(contentDispositionHeader string) string {
	cd := ParseContentDisposition(contentDispositionHeader)
	return cd.Filename
}

// FormatContentDisposition constructs an RFC 6266 compliant Content-Disposition header value
// using the recommendations in RFC 6266 Appendix D (providing ASCII fallback and RFC 8187 filename*).
func FormatContentDisposition(dispType, filename string) string {
	dispType = strings.TrimSpace(dispType)
	if dispType == "" {
		dispType = DispositionAttachment
	}

	if filename == "" {
		return dispType
	}

	isPureASCII := true
	for i := 0; i < len(filename); i++ {
		if filename[i] > 127 || filename[i] == '"' || filename[i] == '\\' {
			isPureASCII = false
			break
		}
	}

	if isPureASCII {
		// RFC 6266 §4.1: token or quoted-string form
		return dispType + `; filename="` + filename + `"`
	}

	// RFC 6266 Appendix D & RFC 8187 §3.2.1: When filename* is sent, also generate ASCII fallback "filename" first
	var asciiFallback strings.Builder
	asciiFallback.Grow(len(filename))

	for i := 0; i < len(filename); i++ {
		b := filename[i]
		if b >= 32 && b <= 126 && b != '"' && b != '\\' && b != '%' {
			asciiFallback.WriteByte(b)
		} else {
			asciiFallback.WriteByte('_')
		}
	}

	return dispType + `; filename="` + asciiFallback.String() + `"; filename*=` + EncodeRFC8187(filename, "")
}

const hexTableUpper = "0123456789ABCDEF"

// IsRFC8187AttrChar reports whether b is a valid RFC 8187 attr-char (RFC 8187 §3.2.1).
func IsRFC8187AttrChar(b byte) bool {
	if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
		return true
	}

	switch b {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

// EncodeRFC8187 encodes value into RFC 8187 extended parameter value notation ("UTF-8'lang'encoded-value")
// using UTF-8 character encoding and percent-encoding for non-attr-char octets (RFC 8187 §3.2.1).
func EncodeRFC8187(value, language string) string {
	var sb strings.Builder
	sb.Grow(len("UTF-8'") + len(language) + len("'") + len(value)*3)
	sb.WriteString("UTF-8'")
	sb.WriteString(language)
	sb.WriteByte('\'')

	for i := 0; i < len(value); i++ {
		b := value[i]
		if IsRFC8187AttrChar(b) {
			sb.WriteByte(b)
		} else {
			sb.WriteByte('%')
			sb.WriteByte(hexTableUpper[b>>4])
			sb.WriteByte(hexTableUpper[b&0x0F])
		}
	}

	return sb.String()
}

// DecodeRFC8187 decodes an RFC 8187 extended parameter value string ("charset'language'value-chars")
// into its constituent charset, language, and decoded value (RFC 8187 §3.2.1).
func DecodeRFC8187(extValue string) (charset, language, value string, err error) {
	firstQuote := strings.IndexByte(extValue, '\'')
	if firstQuote == -1 {
		return "", "", extValue, nil
	}

	secondQuote := strings.IndexByte(extValue[firstQuote+1:], '\'')
	if secondQuote == -1 {
		return "", "", extValue, nil
	}

	secondQuote += firstQuote + 1

	charset = strings.ToLower(strings.TrimSpace(extValue[:firstQuote]))
	language = extValue[firstQuote+1 : secondQuote]
	rawEncoded := extValue[secondQuote+1:]

	unescaped, unescapeErr := url.PathUnescape(rawEncoded)
	if unescapeErr != nil {
		unescaped = rawEncoded
	}

	switch charset {
	case "utf-8", "utf8", "":
		if utf8.ValidString(unescaped) {
			value = unescaped
		} else {
			value = strings.ToValidUTF8(unescaped, "")
		}

	case "iso-8859-1", "latin1":
		value = ISO88591ToUTF8(unescaped)

	default:
		if enc, getErr := htmlindex.Get(charset); getErr == nil {
			if decoded, decErr := enc.NewDecoder().String(unescaped); decErr == nil {
				value = decoded
				return charset, language, value, nil
			}
		}

		if utf8.ValidString(unescaped) {
			value = unescaped
		} else {
			value = strings.ToValidUTF8(unescaped, "")
		}
	}

	return charset, language, value, nil
}

// DecodeRFC8187Value decodes an RFC 8187 extended parameter value string, returning the decoded text value
// with robust error recovery (RFC 8187 §3.2.1 & §4.2).
func DecodeRFC8187Value(extValue string) string {
	_, _, val, _ := DecodeRFC8187(extValue)
	return val
}

// ISO88591ToUTF8 translates ISO-8859-1 raw bytes into UTF-8 representation.
func ISO88591ToUTF8(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) * 2)

	for i := range s {
		buf.WriteRune(rune(s[i]))
	}

	return buf.String()
}

// IsWindowsReservedName checks whether filename stem conflicts with Win32 legacy DOS devices (RFC 6266 §4.3).
func IsWindowsReservedName(filename string) bool {
	if before, _, ok := strings.Cut(filename, "."); ok {
		filename = before
	}

	switch strings.ToUpper(strings.TrimSpace(filename)) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}

	return false
}

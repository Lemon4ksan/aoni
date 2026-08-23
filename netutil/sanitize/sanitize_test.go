// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/netutil/sanitize"
)

func TestSanitize_RFC6266_And_RFC8187(t *testing.T) {
	t.Parallel()

	// Pure ASCII filename
	asciiFormatted := sanitize.FormatContentDisposition("attachment", "document.pdf")
	assert.Equal(t, `attachment; filename="document.pdf"`, asciiFormatted)

	cdAscii := sanitize.ParseContentDisposition(asciiFormatted)
	assert.Equal(t, sanitize.DispositionAttachment, cdAscii.Type)
	assert.Equal(t, "document.pdf", cdAscii.Filename)

	// UTF-8 filename with RFC 8187 encoding
	utf8Formatted := sanitize.FormatContentDisposition("attachment", "отчет_2026.pdf")
	assert.Contains(t, utf8Formatted, `filename*=UTF-8''`)

	cdUtf8 := sanitize.ParseContentDisposition(utf8Formatted)
	assert.Equal(t, "отчет_2026.pdf", cdUtf8.Filename)

	// Path traversal protection
	assert.Equal(t, "secret.txt", sanitize.FileName("../../secret.txt"))
	assert.Equal(t, "passwords.db", sanitize.FileName("C:\\Windows\\System32\\..\\passwords.db"))
	assert.Equal(t, "downloaded_file", sanitize.FileName("../.."))
	assert.Equal(t, "downloaded_file", sanitize.FileName("CON"))
	assert.Equal(t, "downloaded_file", sanitize.FileName("NUL.txt"))

	// RFC 8187 Encode & Decode
	encoded := sanitize.EncodeRFC8187("letztes Kapitel", "de")
	assert.Equal(t, "UTF-8'de'letztes%20Kapitel", encoded)

	charset, lang, val, err := sanitize.DecodeRFC8187(encoded)
	assert.NoError(t, err)
	assert.Equal(t, "utf-8", charset)
	assert.Equal(t, "de", lang)
	assert.Equal(t, "letztes Kapitel", val)
}

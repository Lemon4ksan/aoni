// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sanitize_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/sanitize"
)

func FuzzSanitize(f *testing.F) {
	f.Add(
		"attachment; filename=\"report.pdf\"; filename*=UTF-8''%e2%82%ac%20rates.pdf",
		"../../../etc/passwd",
		"UTF-8''filename.txt",
	)
	f.Add("form-data; name=\"upload\"; filename=\"COM1.txt\"", "CON", "")
	f.Add("", "", "")

	f.Fuzz(func(t *testing.T, header, rawFilename, extValue string) {
		cd := sanitize.ParseContentDisposition(header)
		_ = cd.Type
		_ = cd.Filename

		extracted := sanitize.ExtractFilename(header)
		_ = extracted

		cleaned := sanitize.FileName(rawFilename)
		_ = cleaned
		_ = sanitize.IsWindowsReservedName(rawFilename)

		_, _, _, _ = sanitize.DecodeRFC8187(extValue)
		_ = sanitize.DecodeRFC8187Value(extValue)
		_ = sanitize.ISO88591ToUTF8(rawFilename)
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package extract

import (
	fhtml "github.com/lemon4ksan/foundation/text/htmlkit"
)

// HTMLUnescape converts HTML entities within src into their unescaped UTF-8 byte representation.
// Core implementation is located in [github.com/lemon4ksan/foundation/text/htmlkit].
func HTMLUnescape(src []byte) []byte {
	return fhtml.Unescape(src)
}

// AppendHTMLUnescape parses HTML entities in src and appends the decoded UTF-8 bytes into dst.
// Core implementation is located in [github.com/lemon4ksan/foundation/text/htmlkit].
func AppendHTMLUnescape(dst, src []byte) []byte {
	return fhtml.AppendUnescape(dst, src)
}

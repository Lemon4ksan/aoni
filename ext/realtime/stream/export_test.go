// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream

import "net/http"

// NewStream creates a Stream wrapping resp strictly for package unit and fuzz tests.
func NewStream(resp *http.Response) *Stream {
	return &Stream{resp: resp}
}

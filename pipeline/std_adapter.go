// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/http"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/std"
)

// NewStdRequestAdapter wraps stdReq into a core.Request contract adapter.
func NewStdRequestAdapter(stdReq *http.Request) core.Request {
	return std.NewRequest(stdReq)
}

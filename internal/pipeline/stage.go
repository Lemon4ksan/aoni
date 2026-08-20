// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/http"
)

// PrepStage represents an individual, modular transformation applied to an outbound HTTP request.
type PrepStage[Req, Resp any] func(p *Pipeline[Req, Resp], req *http.Request, tx *Tx) *http.Request

// PostStage represents an individual, modular transformation or validation step applied to an inbound HTTP response.
type PostStage[Req, Resp any] func(p *Pipeline[Req, Resp], stdReq *http.Request, resp *http.Response, tx *Tx) (*http.Response, error)

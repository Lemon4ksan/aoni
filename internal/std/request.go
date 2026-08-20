// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package std provides high-performance zero-allocation adapters between standard library net/http
// and the unified aoni core.Request / core.Response contracts.
package std

import (
	"net/http"

	"github.com/lemon4ksan/aoni/internal/core"
)

// Request adapts a standard net/http [*http.Request] to the unified [core.Request] contract.
type Request = core.StdRequest

// NewRequest wraps req into a pooled [Request] adapter.
func NewRequest(req *http.Request) *Request {
	return core.NewStdRequest(req)
}

// ReleaseRequest returns the request to the pool after execution.
func ReleaseRequest(r *Request) {
	core.ReleaseStdRequest(r)
}

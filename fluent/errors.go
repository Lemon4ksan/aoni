// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"errors"
	"strconv"

	"github.com/lemon4ksan/aoni"
)

var (
	// ErrDownloadFailed is returned when a stream download request yields an HTTP status code >= 400.
	ErrDownloadFailed = errors.New("aoni/fluent: download HTTP status error")

	// ErrUnexpectedStatus is returned when response status code violates [Request.ExpectStatus] criteria.
	ErrUnexpectedStatus = errors.New("aoni/fluent: response status code mismatch")

	// ErrRangeNotSatisfiable is returned when the requested byte range exceeds the remote file size (HTTP 416).
	ErrRangeNotSatisfiable = errors.New("aoni/fluent: requested byte range not satisfiable by server")
)

// Error describes an operational failure occurring during request building or execution.
type Error struct {
	// Op identifies the failed operation (e.g., "download", "expect_status").
	Op string
	// Path contains the target URL or local file path.
	Path string
	// Code represents the returned HTTP status code, if applicable.
	Code int
	// Err is the underlying causal error.
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Code > 0 {
		return "aoni/fluent: " + e.Op + " " + e.Path + " status " + strconv.Itoa(e.Code) + ": " + e.Err.Error()
	}

	if e.Path != "" {
		return "aoni/fluent: " + e.Op + " " + e.Path + ": " + e.Err.Error()
	}

	return "aoni/fluent: " + e.Op + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Category categorizes the HTTP status code into RFC 9110 status classes.
func (e *Error) Category() aoni.HTTPStatusCategory {
	if e == nil || e.Code < 100 || e.Code > 599 {
		return aoni.CategoryUnknown
	}

	return aoni.HTTPStatusCategory(e.Code / 100)
}

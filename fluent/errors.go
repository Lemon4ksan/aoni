// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"github.com/lemon4ksan/aoni"
)

var (
	// ErrNilRequest is returned when attempting to execute on a nil [Request] pointer.
	ErrNilRequest = aoni.ErrNilRequest

	// ErrDownloadFailed is returned when a stream download request yields an HTTP status code >= 400.
	ErrDownloadFailed = aoni.ErrDownloadFailed

	// ErrUnexpectedStatus is returned when response status code violates [Request.ExpectStatus] criteria.
	ErrUnexpectedStatus = aoni.ErrUnexpectedStatus

	// ErrRangeNotSatisfiable is returned when the requested byte range exceeds the remote file size (HTTP 416).
	ErrRangeNotSatisfiable = aoni.ErrRangeNotSatisfiable
)

// Error describes an operational failure occurring during request building or execution.
type Error = aoni.Error

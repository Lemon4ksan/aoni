// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"net/http"
	"time"
)

// FormatTimeout is exported strictly for package unit tests.
func FormatTimeout(d time.Duration) string {
	return formatTimeout(d)
}

// ParseGRPCStatus is exported strictly for package unit tests.
func ParseGRPCStatus(trailers http.Header) *StatusError {
	return parseGRPCStatus(trailers)
}

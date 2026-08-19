// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"
)

// MarshalFrame is exported strictly for package unit tests.
func MarshalFrame(msg proto.Message, compressed bool) ([]byte, error) {
	return marshalFrame(msg, compressed)
}

// UnmarshalFrame is exported strictly for package unit tests.
func UnmarshalFrame(r io.Reader, target proto.Message) (bool, error) {
	return unmarshalFrame(r, target)
}

// MarshalFrameCompressed is exported strictly for package unit tests.
func MarshalFrameCompressed(msg proto.Message, compress bool) ([]byte, error) {
	return marshalFrame(msg, compress)
}

// FormatTimeout is exported strictly for package unit tests.
func FormatTimeout(d time.Duration) string {
	return formatTimeout(d)
}

// ParseGRPCStatus is exported strictly for package unit tests.
func ParseGRPCStatus(trailers http.Header) *StatusError {
	return parseGRPCStatus(trailers)
}

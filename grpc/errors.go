// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import "errors"

var (
	// ErrInvalidGRPCFrame is returned when a gRPC 5-byte length header is corrupted or truncated.
	ErrInvalidGRPCFrame = errors.New("aoni grpc: invalid or truncated gRPC frame header")

	// ErrInvalidContentType is returned when the response Content-Type is not application/grpc.
	ErrInvalidContentType = errors.New("aoni grpc: invalid content-type in response (expected application/grpc)")

	// ErrMissingGRPCStatus is returned when the HTTP/2 response trailers lack the mandatory grpc-status header.
	ErrMissingGRPCStatus = errors.New("aoni grpc: missing mandatory grpc-status header in response trailers")
)

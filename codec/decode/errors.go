// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import "errors"

var (
	// ErrInvalidRawTarget is returned when RawDecoder is supplied with an output type other than *[]byte.
	ErrInvalidRawTarget = errors.New("aoni: RawDecoder requires *[]byte output type")

	// ErrInvalidProtoTarget is returned when ProtoDecoder or GRPCWebDecoder is supplied with an output type that does not satisfy proto.Message.
	ErrInvalidProtoTarget = errors.New("aoni: ProtoDecoder requires proto.Message output type")

	// ErrInvalidGRPCWebFrame is returned when a gRPC-Web frame header or payload is corrupted or truncated.
	ErrInvalidGRPCWebFrame = errors.New("aoni: invalid gRPC-Web frame format")

	// ErrGRPCWebStatusError is returned when a gRPC-Web endpoint returns a non-zero gRPC status code in its trailer frame.
	ErrGRPCWebStatusError = errors.New("aoni: gRPC-Web endpoint returned error status")
)

// Error describes a structural or unmarshaling failure during decoding.
type Error struct {
	Format string // "raw", "proto", "grpc-web", "protojson", "json", "xml"
	Target string // Target type name (e.g. "*pb.UserResponse")
	Err    error  // Underlying sentinel error or parser error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Target != "" {
		return "aoni: decode " + e.Format + " into " + e.Target + ": " + e.Err.Error()
	}

	return "aoni: decode " + e.Format + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// GRPCWebError describes a framing, status code, or stream error specific to gRPC-Web.
type GRPCWebError struct {
	Op         string // Operation context (e.g. "decompress", "read_header", "read_payload")
	StatusCode string // gRPC status code string (e.g. "16")
	StatusMsg  string // gRPC status message
	Err        error  // Underlying error
}

func (e *GRPCWebError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.StatusCode != "" {
		return "aoni: grpc-web status=" + e.StatusCode + " msg=" + e.StatusMsg + ": " + e.Err.Error()
	}

	if e.Op != "" {
		return "aoni: grpc-web " + e.Op + ": " + e.Err.Error()
	}

	return "aoni: grpc-web: " + e.Err.Error()
}

func (e *GRPCWebError) Unwrap() error { return e.Err }

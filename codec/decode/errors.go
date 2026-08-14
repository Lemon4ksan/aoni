// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import "errors"

var (
	// ErrInvalidRawTarget is returned when [RawDecoder] is supplied with a target output type other than *[]byte.
	ErrInvalidRawTarget = errors.New("aoni/decode: RawDecoder requires *[]byte output target")

	// ErrInvalidProtoTarget is returned when [ProtoDecoder] or [GRPCWebDecoder] is supplied with a target not implementing [proto.Message].
	ErrInvalidProtoTarget = errors.New("aoni/decode: ProtoDecoder requires proto.Message output target")

	// ErrInvalidGRPCWebFrame is returned when a gRPC-Web frame header or payload is corrupted or truncated.
	ErrInvalidGRPCWebFrame = errors.New("aoni/decode: invalid gRPC-Web frame format")

	// ErrGRPCWebStatusError is returned when a gRPC-Web endpoint returns a non-zero status code in its trailer frame.
	ErrGRPCWebStatusError = errors.New("aoni/decode: gRPC-Web endpoint returned error status")
)

// Error describes a structural or unmarshaling failure encountered during stream decoding.
type Error struct {
	// Format identifies the encoding format (e.g. "json", "protobuf", "xml").
	Format string
	// Target identifies the target Go type name into which decoding was attempted.
	Target string
	// Err holds the underlying unmarshaling or I/O error cause.
	Err error
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

// GRPCWebError describes a framing, status code, or stream error specific to gRPC-Web response processing.
type GRPCWebError struct {
	// Op identifies the operation stage (e.g., "header", "frame", "trailer").
	Op string
	// StatusCode records the string representation of the gRPC status code (e.g., "0", "14").
	StatusCode string
	// StatusMsg provides the error message string returned in gRPC trailers.
	StatusMsg string
	// StatusDetails contains raw binary status detail payloads.
	StatusDetails []byte
	// Err holds the underlying cause error.
	Err error
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

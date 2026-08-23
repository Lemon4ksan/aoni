// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrorCode defines standard HTTP/2 32-bit error codes specified in RFC 9113 Section 7.
type ErrorCode uint32

const (
	// NoError indicates graceful shutdown or that the condition is not an error (RFC 9113 §7: NO_ERROR, 0x0).
	NoError ErrorCode = 0x0

	// ProtocolError indicates an unspecific protocol violation (RFC 9113 §7: PROTOCOL_ERROR, 0x1).
	ProtocolError ErrorCode = 0x1

	// InternalError indicates an unexpected internal endpoint failure (RFC 9113 §7: INTERNAL_ERROR, 0x2).
	InternalError ErrorCode = 0x2

	// FlowControlError indicates a flow-control protocol violation or window overflow (RFC 9113 §7: FLOW_CONTROL_ERROR, 0x3).
	FlowControlError ErrorCode = 0x3

	// SettingsTimeoutError indicates a SETTINGS frame was not acknowledged in a timely manner (RFC 9113 §7 & §6.5.3: SETTINGS_TIMEOUT, 0x4).
	SettingsTimeoutError ErrorCode = 0x4

	// StreamClosedError indicates a frame was received on a half-closed or closed stream (RFC 9113 §7 & §5.1: STREAM_CLOSED, 0x5).
	StreamClosedError ErrorCode = 0x5

	// FrameSizeError indicates an invalid frame payload size (RFC 9113 §7 & §4.2: FRAME_SIZE_ERROR, 0x6).
	FrameSizeError ErrorCode = 0x6

	// RefusedStreamError indicates stream refusal prior to processing, permitting automatic retry (RFC 9113 §7 & §8.7: REFUSED_STREAM, 0x7).
	RefusedStreamError ErrorCode = 0x7

	// StreamCanceled indicates the stream is no longer needed (RFC 9113 §7: CANCEL, 0x8).
	StreamCanceled ErrorCode = 0x8

	// CompressionError indicates failure to maintain the HPACK field section compression context (RFC 9113 §7 & §4.3: COMPRESSION_ERROR, 0x9).
	CompressionError ErrorCode = 0x9

	// ConnectionError indicates an error on a CONNECT tunnel stream (RFC 9113 §7 & §8.5: CONNECT_ERROR, 0xa).
	ConnectionError ErrorCode = 0xa

	// EnhanceYourCalm indicates peer behavior generating excessive load / anti-DoS threshold (RFC 9113 §7 & §10.5: ENHANCE_YOUR_CALM, 0xb).
	EnhanceYourCalm ErrorCode = 0xb

	// InadequateSecurity indicates transport properties do not meet minimum security requirements (RFC 9113 §7, §9.2 & Appendix A: INADEQUATE_SECURITY, 0xc).
	InadequateSecurity ErrorCode = 0xc

	// HTTP11Required indicates that HTTP/1.1 must be used instead of HTTP/2 (RFC 9113 §7: HTTP_1_1_REQUIRED, 0xd).
	HTTP11Required ErrorCode = 0xd
)

var (
	ErrServerSupport      = errors.New("h2engine: server does not support HTTP/2")
	ErrNoAvailableStreams = errors.New("h2engine: ran out of available streams")
	ErrTimeout            = errors.New("h2engine: server is not replying to pings")
	ErrUnexpectedSize     = errors.New("h2engine: unexpected header size")
	ErrWriterClosed       = errors.New("h2engine: stream writer closed")
	ErrWrongPreface       = errors.New("h2engine: invalid connection preface (RFC 9113 §3.4)")
	ErrMalformedString    = errors.New("h2engine: malformed HPACK string data (RFC 9113 §4.3)")
	ErrGoAwayRetryable    = errors.New("h2engine: stream affected by GOAWAY frame (RFC 9113 §6.8 & §8.7)")
	ErrControlFrameFlood  = NewGoAwayError(EnhanceYourCalm, "too many consecutive control frames (RFC 9113 §10.5)")
	ErrUnknownFrameType   = NewError(ProtocolError, "unknown frame type")
	ErrMissingBytes       = NewError(ProtocolError, "missing payload bytes")
	ErrPayloadExceeds     = NewError(
		FrameSizeError,
		"frame payload exceeds negotiated maximum size (RFC 9113 §4.2)",
	)
	ErrCompression            = NewGoAwayError(CompressionError, "compression error (RFC 9113 §4.3)")
	ErrInvalidPingPayload     = NewGoAwayError(FrameSizeError, "invalid ping payload (RFC 9113 §6.7)")
	ErrStreamClosed           = NewGoAwayError(StreamClosedError, "stream has been closed (RFC 9113 §5.1)")
	ErrInvalidWindowIncrement = NewGoAwayError(ProtocolError, "window increment of zero (RFC 9113 §6.9)")
	ErrWindowAboveLimits      = NewGoAwayError(FlowControlError, "window is above limits (RFC 9113 §6.9.1)")
)

// Error encapsulates an HTTP/2 protocol failure.
type Error struct {
	code      ErrorCode
	frameType FrameType
	debug     string
}

// Is reports whether the error code matches target.
func (e Error) Is(target error) bool {
	return errors.Is(e.code, target)
}

// Code returns the protocol error code.
func (e Error) Code() ErrorCode {
	return e.code
}

// Debug returns optional text diagnostics describing the error condition.
func (e Error) Debug() string {
	return e.debug
}

// NewError constructs an Error configured for stream resets or protocol errors.
func NewError(code ErrorCode, debug string) Error {
	return Error{
		code:      code,
		debug:     debug,
		frameType: FrameResetStream,
	}
}

// NewGoAwayError constructs an Error that triggers a connection-level GOAWAY shutdown.
func NewGoAwayError(code ErrorCode, debug string) Error {
	return Error{
		code:      code,
		debug:     debug,
		frameType: FrameGoAway,
	}
}

// NewResetStreamError constructs an Error that signals a stream-level termination.
func NewResetStreamError(code ErrorCode, debug string) Error {
	return Error{
		code:      code,
		debug:     debug,
		frameType: FrameResetStream,
	}
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.debug)
}

var errorCodeNames = [...]string{
	NoError:              "NoError",
	ProtocolError:        "ProtocolError",
	InternalError:        "InternalError",
	FlowControlError:     "FlowControlError",
	SettingsTimeoutError: "SettingsTimeoutError",
	StreamClosedError:    "StreamClosedError",
	FrameSizeError:       "FrameSizeError",
	RefusedStreamError:   "RefusedStreamError",
	StreamCanceled:       "StreamCanceled",
	CompressionError:     "CompressionError",
	ConnectionError:      "ConnectionError",
	EnhanceYourCalm:      "EnhanceYourCalm",
	InadequateSecurity:   "InadequateSecurity",
	HTTP11Required:       "HTTP11Required",
}

func (e ErrorCode) String() string {
	if int(e) >= len(errorCodeNames) {
		return "Unknown"
	}

	return errorCodeNames[e]
}

func (e ErrorCode) Error() string {
	if int(e) < len(errorCodeNames) {
		return errorCodeNames[e]
	}

	return strconv.Itoa(int(e))
}

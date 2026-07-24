// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrorCode defines the standard HTTP/2 error codes specified in RFC 7540 Section 7.
type ErrorCode uint32

const (
	NoError              ErrorCode = 0x0
	ProtocolError        ErrorCode = 0x1
	InternalError        ErrorCode = 0x2
	FlowControlError     ErrorCode = 0x3
	SettingsTimeoutError ErrorCode = 0x4
	StreamClosedError    ErrorCode = 0x5
	FrameSizeError       ErrorCode = 0x6
	RefusedStreamError   ErrorCode = 0x7
	StreamCanceled       ErrorCode = 0x8
	CompressionError     ErrorCode = 0x9
	ConnectionError      ErrorCode = 0xa
	EnhanceYourCalm      ErrorCode = 0xb
	InadequateSecurity   ErrorCode = 0xc
	HTTP11Required       ErrorCode = 0xd
)

var (
	ErrServerSupport          = errors.New("aoni h2engine: server does not support HTTP/2")
	ErrNotAvailableStreams    = errors.New("aoni h2engine: ran out of available streams")
	ErrTimeout                = errors.New("aoni h2engine: server is not replying to pings")
	ErrUnexpectedSize         = errors.New("aoni h2engine: unexpected header size")
	ErrWriterClosed           = errors.New("aoni h2engine: stream writer closed")
	ErrWrongPreface           = errors.New("aoni h2engine: invalid connection preface")
	ErrMalformedString        = errors.New("aoni h2engine: malformed HPACK string data")
	ErrGoAwayRetryable        = errors.New("aoni h2engine: stream affected by GOAWAY frame")
	ErrControlFrameFlood      = NewGoAwayError(EnhanceYourCalm, "too many consecutive control frames")
	ErrUnknownFrameType       = NewError(ProtocolError, "unknown frame type")
	ErrMissingBytes           = NewError(ProtocolError, "missing payload bytes")
	ErrPayloadExceeds         = NewError(FrameSizeError, "frame payload exceeds negotiated maximum size")
	ErrCompression            = NewGoAwayError(CompressionError, "compression error")
	ErrInvalidPingPayload     = NewGoAwayError(FrameSizeError, "invalid ping payload")
	ErrStreamClosed           = NewGoAwayError(StreamClosedError, "stream has been closed")
	ErrInvalidWindowIncrement = NewGoAwayError(ProtocolError, "window increment of zero")
	ErrWindowAboveLimits      = NewGoAwayError(FlowControlError, "window is above limits")
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

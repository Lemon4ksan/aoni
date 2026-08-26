// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

var (
	// ErrBadHandshake indicates the server rejected the WebSocket handshake
	// or returned invalid upgrade response headers.
	ErrBadHandshake = errors.New("aoni/ws: bad handshake")

	// ErrNilConnection indicates the connection is nil.
	ErrNilConnection = errors.New("aoni/ws: nil connection")

	// ErrUnsupportedWSScheme indicates an invalid URI scheme was provided (expected ws:// or wss://).
	ErrUnsupportedWSScheme = errors.New("aoni/ws: unsupported scheme (expected ws or wss)")

	// ErrInvalidWellKnownSuffix indicates that an empty or invalid well-known URI suffix was provided.
	ErrInvalidWellKnownSuffix = errors.New("aoni/ws: invalid well-known URI suffix")

	// ErrPathTraversalBlocked indicates a path traversal attempt was detected in a well-known URI.
	ErrPathTraversalBlocked = errors.New("aoni/ws: path traversal blocked in well-known URI")

	// ErrSubprotocolMismatch indicates the server selected a subprotocol not requested by the client.
	ErrSubprotocolMismatch = errors.New("aoni/ws: server selected unrequested subprotocol")

	// ErrInvalidSubprotocol indicates a subprotocol token contains invalid characters according to RFC 2616.
	ErrInvalidSubprotocol = errors.New("aoni/ws: invalid subprotocol token")

	// ErrInvalidCompression indicates invalid or unsupported compression extension parameters.
	ErrInvalidCompression = errors.New("aoni/ws: invalid compression negotiation")

	// ErrFlateDecompressFailed indicates a failure while decompressing a permessage-deflate payload.
	ErrFlateDecompressFailed = errors.New("aoni/ws: flate decompression failed")

	// ErrFlateCompressFailed indicates a failure while compressing a permessage-deflate payload.
	ErrFlateCompressFailed = errors.New("aoni/ws: flate compression failed")

	// ErrH2ConnectNotSupported indicates the HTTP/2 peer does not support Extended CONNECT (RFC 8441).
	ErrH2ConnectNotSupported = errors.New("aoni/ws: http2 extended connect not supported by peer")

	// ErrH2StreamClosed indicates the HTTP/2 WebSocket stream was closed unexpectedly.
	ErrH2StreamClosed = errors.New("aoni/ws: http2 stream closed")

	// ErrH2ConnectFailed indicates the HTTP/2 Extended CONNECT handshake returned a non-200 status code.
	ErrH2ConnectFailed = errors.New("aoni/ws: http2 websocket connect failed")

	// ErrH2GoAway indicates the HTTP/2 connection received a GOAWAY frame during handshake.
	ErrH2GoAway = errors.New("aoni/ws: http2 connection closed")

	// ErrH2UnexpectedFrame indicates an unhandled frame type was received during the HTTP/2 handshake.
	ErrH2UnexpectedFrame = errors.New("aoni/ws: unexpected frame during h2 handshake")

	// ErrReservedBitsSet indicates non-zero RSV bits were received without a negotiated extension.
	ErrReservedBitsSet = errors.New("aoni/ws: reserved RSV bits set without negotiated extension")

	// ErrControlFrameTooLarge indicates a control frame payload exceeded the 125-byte RFC 6455 limit.
	ErrControlFrameTooLarge = errors.New("aoni/ws: control frame payload cannot exceed 125 bytes")

	// ErrFrameTooLarge indicates an incoming frame exceeded the maximum allowed memory buffer size.
	ErrFrameTooLarge = errors.New("aoni/ws: frame payload exceeds maximum allowed size")

	// ErrControlFrameFragmented indicates a control frame with FIN=0 was received (RFC 6455 §5.4).
	ErrControlFrameFragmented = errors.New("aoni/ws: control frames must not be fragmented")

	// ErrUnexpectedContinuationFrame indicates a continuation frame without prior data frame (RFC 6455 §5.4).
	ErrUnexpectedContinuationFrame = errors.New("aoni/ws: unexpected continuation frame")

	// ErrIncompleteFragmentation indicates a new data frame arrived before previous fragmentation finished (RFC 6455 §5.4).
	ErrIncompleteFragmentation = errors.New(
		"aoni/ws: received new data frame before completing previous fragmented message",
	)
)

// CloseError represents an RFC 6455 WebSocket close frame error containing a status code and human-readable reason (RFC 6455 §7.1.5 & §7.1.6).
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("aoni/ws: close %d: %s", e.Code, e.Reason)
	}

	return fmt.Sprintf("aoni/ws: close %d", e.Code)
}

// FormatCloseMessage constructs a standard 2-byte big-endian status code followed by an optional UTF-8 reason string (RFC 6455 §5.5.1).
func FormatCloseMessage(code int, reason string) []byte {
	if code == 0 && reason == "" {
		return nil
	}

	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf[:2], uint16(code))
	copy(buf[2:], reason)

	return buf
}

// ParseCloseMessage parses an incoming Close frame payload into a 2-byte status code and UTF-8 reason string (RFC 6455 §5.5.1 & §7.1.6).
func ParseCloseMessage(payload []byte) (code int, reason string) {
	if len(payload) < 2 {
		return StatusNoStatusRcvd, ""
	}

	code = int(binary.BigEndian.Uint16(payload[:2]))
	reason = string(payload[2:])

	return code, reason
}

// IsCloseError reports whether err is a [CloseError] matching any of the specified target status codes (RFC 6455 §7.4).
func IsCloseError(err error, codes ...int) bool {
	ce, ok := errors.AsType[*CloseError](err)
	if !ok {
		return false
	}

	if len(codes) == 0 {
		return true
	}

	return slices.Contains(codes, ce.Code)
}

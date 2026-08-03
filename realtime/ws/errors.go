// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import "errors"

var (
	// ErrBadHandshake indicates the server rejected the WebSocket handshake
	// or returned invalid upgrade response headers.
	ErrBadHandshake = errors.New("aoni ws: bad handshake")

	// ErrUnsupportedWSScheme indicates an invalid URI scheme was provided (expected ws:// or wss://).
	ErrUnsupportedWSScheme = errors.New("aoni ws: unsupported scheme (expected ws or wss)")

	// ErrH2ConnectNotSupported indicates the HTTP/2 peer does not support Extended CONNECT (RFC 8441).
	ErrH2ConnectNotSupported = errors.New("aoni ws: http2 extended connect not supported by peer")

	// ErrH2StreamClosed indicates the HTTP/2 WebSocket stream was closed unexpectedly.
	ErrH2StreamClosed = errors.New("aoni ws: http2 stream closed")

	// ErrH2ConnectFailed indicates the HTTP/2 Extended CONNECT handshake returned a non-200 status code.
	ErrH2ConnectFailed = errors.New("aoni ws: http2 websocket connect failed")

	// ErrH2GoAway indicates the HTTP/2 connection received a GOAWAY frame during handshake.
	ErrH2GoAway = errors.New("aoni ws: http2 connection closed")

	// ErrH2UnexpectedFrame indicates an unhandled frame type was received during the HTTP/2 handshake.
	ErrH2UnexpectedFrame = errors.New("aoni ws: unexpected frame during h2 handshake")

	// ErrReservedBitsSet indicates non-zero RSV bits were received without a negotiated extension.
	ErrReservedBitsSet = errors.New("aoni ws: reserved RSV bits set without negotiated extension")

	// ErrControlFrameTooLarge indicates a control frame payload exceeded the 125-byte RFC 6455 limit.
	ErrControlFrameTooLarge = errors.New("aoni ws: control frame payload cannot exceed 125 bytes")

	// ErrFrameTooLarge indicates an incoming frame exceeded the maximum allowed memory buffer size.
	ErrFrameTooLarge = errors.New("aoni ws: frame payload too large")
)

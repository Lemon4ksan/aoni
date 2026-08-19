// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socketio

import "errors"

var (
	// ErrNotConnected indicates that an event emission was attempted on a closed or uninitialized socket.
	ErrNotConnected = errors.New("aoni/socketio: connection closed or not connected")

	// ErrAckTimeout indicates that a server acknowledgment was not received within the deadline.
	ErrAckTimeout = errors.New("aoni/socketio: acknowledgment timeout")

	// ErrEmptyPacket indicates a zero-length Socket.IO payload frame was received.
	ErrEmptyPacket = errors.New("aoni/socketio: empty packet")
)

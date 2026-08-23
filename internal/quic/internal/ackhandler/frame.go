// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ackhandler

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
)

// FrameHandler handles the acknowledgement and the loss of a frame.
type FrameHandler interface {
	OnAcked(wire.Frame)
	OnLost(wire.Frame)
}

type Frame struct {
	Frame   wire.Frame // nil if the frame has already been acknowledged in another packet
	Handler FrameHandler
}

type StreamFrame struct {
	Frame   *wire.StreamFrame
	Handler FrameHandler
}

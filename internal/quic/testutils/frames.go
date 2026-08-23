// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutils

import "github.com/lemon4ksan/aoni/internal/quic/internal/wire"

type (
	Frame                   = wire.Frame
	AckFrame                = wire.AckFrame
	ConnectionCloseFrame    = wire.ConnectionCloseFrame
	CryptoFrame             = wire.CryptoFrame
	DataBlockedFrame        = wire.DataBlockedFrame
	HandshakeDoneFrame      = wire.HandshakeDoneFrame
	MaxDataFrame            = wire.MaxDataFrame
	MaxStreamDataFrame      = wire.MaxStreamDataFrame
	MaxStreamsFrame         = wire.MaxStreamsFrame
	NewConnectionIDFrame    = wire.NewConnectionIDFrame
	NewTokenFrame           = wire.NewTokenFrame
	PathChallengeFrame      = wire.PathChallengeFrame
	PathResponseFrame       = wire.PathResponseFrame
	PingFrame               = wire.PingFrame
	ResetStreamFrame        = wire.ResetStreamFrame
	RetireConnectionIDFrame = wire.RetireConnectionIDFrame
	StopSendingFrame        = wire.StopSendingFrame
	StreamDataBlockedFrame  = wire.StreamDataBlockedFrame
	StreamFrame             = wire.StreamFrame
	StreamsBlockedFrame     = wire.StreamsBlockedFrame
)

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/utils"
)

func newTestStreamFlowController(id protocol.StreamID) *streamFlowController {
	return newTestStreamFlowControllerWithSendWindow(id, 0)
}

func newTestStreamFlowControllerWithSendWindow(
	id protocol.StreamID,
	sendWindow protocol.ByteCount,
) *streamFlowController {
	return newTestStreamFlowControllerWithWindows(id, sendWindow, protocol.MaxByteCount, protocol.MaxByteCount)
}

func newTestStreamFlowControllerWithWindows(
	id protocol.StreamID,
	sendWindow protocol.ByteCount,
	streamReceiveWindow protocol.ByteCount,
	connReceiveWindow protocol.ByteCount,
) *streamFlowController {
	connFC := newConnectionFlowController(
		connReceiveWindow,
		protocol.MaxByteCount,
		nil,
		utils.NewRTTStats(),
		utils.DefaultLogger,
	)
	connFC.UpdateSendWindow(protocol.MaxByteCount)

	return newStreamFlowController(
		id,
		connFC,
		streamReceiveWindow,
		protocol.MaxByteCount,
		sendWindow,
		utils.NewRTTStats(),
		utils.DefaultLogger,
	)
}

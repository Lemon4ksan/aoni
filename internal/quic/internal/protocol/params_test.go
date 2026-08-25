// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestPacketQueueCapacities(t *testing.T) {
	// Ensure that the session can queue more packets than the 0-RTT queue
	require.Greater(t, MaxConnUnprocessedPackets, Max0RTTQueueLen)
	require.Greater(t, MaxUndecryptablePackets, Max0RTTQueueLen)
}

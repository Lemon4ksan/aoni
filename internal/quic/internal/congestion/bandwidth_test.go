// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package congestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBandwidthFromDelta(t *testing.T) {
	require.Equal(t, 1000*BytesPerSecond, BandwidthFromDelta(1, time.Millisecond))
}

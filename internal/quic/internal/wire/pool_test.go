// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestGetAndPutStreamFrames(t *testing.T) {
	f := GetStreamFrame()
	putStreamFrame(f)
}

func TestPanicOnPuttingStreamFrameWithWrongCapacity(t *testing.T) {
	f := GetStreamFrame()
	f.Data = []byte("foobar")
	require.Panics(t, func() { putStreamFrame(f) })
}

func TestAcceptStreamFramesNotFromBuffer(t *testing.T) {
	f := &StreamFrame{Data: []byte("foobar")}
	putStreamFrame(f)
	// No assertion needed as we're just checking it doesn't panic
}

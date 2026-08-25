// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package tun

import (
	"errors"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestWintunAdapter_NotLoadedOrMissing(t *testing.T) {
	t.Parallel()

	adapter, err := NewWintunAdapter("AoniTunTest", "MASQUE")
	if err != nil {
		assert.Error(t, err)
		assert.True(
			t,
			errors.Is(err, ErrWintunNotLoaded) || errors.Is(err, ErrAdapterCreationFailed) ||
				errors.Is(err, ErrSessionCreationFailed),
		)
	} else {
		require.NotNil(t, adapter)
		_ = adapter.Close()
	}
}

func TestWintunAdapter_ClosedState(t *testing.T) {
	t.Parallel()

	adapter := &WintunAdapter{}
	adapter.closed.Store(true)

	// ReceivePacket on closed adapter returns io.EOF
	pkt, err := adapter.ReceivePacket()
	assert.Nil(t, pkt)
	assert.ErrorIs(t, err, io.EOF)

	// SendPacket on closed adapter returns nil without panicking
	err = adapter.SendPacket([]byte{0x45, 0x00, 0x00, 0x14})
	assert.NoError(t, err)

	// Read on closed adapter returns io.EOF
	var buf [1500]byte

	n, err := adapter.Read(buf[:])
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF)

	// Write on closed adapter returns nil without error
	n, err = adapter.Write([]byte{0x45, 0x00, 0x00, 0x14})
	assert.Equal(t, 4, n)
	assert.NoError(t, err)
}

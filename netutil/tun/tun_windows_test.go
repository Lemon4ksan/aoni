// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package tun

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// ReleaseReceivePacket on closed or empty slice returns safely
	adapter.ReleaseReceivePacket(nil)
	adapter.ReleaseReceivePacket([]byte{})

	// Close on already closed adapter is idempotent
	err = adapter.Close()
	assert.NoError(t, err)
}

func TestWintunErrorConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "aoni tun: wintun.dll not found in application or system directory", ErrWintunNotLoaded.Error())
	assert.Equal(t, "aoni tun: failed to create wintun network adapter", ErrAdapterCreationFailed.Error())
	assert.Equal(t, "aoni tun: failed to start wintun session", ErrSessionCreationFailed.Error())
}

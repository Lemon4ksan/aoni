// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/tunnel/masque"
)

func TestIPProtocolVTable(t *testing.T) {
	t.Parallel()

	vt := masque.NewIPProtocolVTable()

	tcpHandled := false
	udpHandled := false
	icmpHandled := false

	vt.Register(6, func(packet []byte) error {
		tcpHandled = true
		return nil
	})

	vt.Register(17, func(packet []byte) error {
		udpHandled = true
		return nil
	})

	vt.Register(1, func(packet []byte) error {
		icmpHandled = true
		return nil
	})

	// Valid IPv4 TCP packet (proto = 6 at offset 9)
	ipv4TCP := make([]byte, 20)
	ipv4TCP[0] = 0x45
	ipv4TCP[9] = 6

	err := vt.DispatchIPPacket(ipv4TCP)
	require.NoError(t, err)
	assert.True(t, tcpHandled)

	// Valid IPv4 UDP packet (proto = 17 at offset 9)
	ipv4UDP := make([]byte, 20)
	ipv4UDP[0] = 0x45
	ipv4UDP[9] = 17

	err = vt.DispatchIPPacket(ipv4UDP)
	require.NoError(t, err)
	assert.True(t, udpHandled)

	// Valid IPv4 ICMP packet (proto = 1 at offset 9, cold path)
	ipv4ICMP := make([]byte, 20)
	ipv4ICMP[0] = 0x45
	ipv4ICMP[9] = 1

	err = vt.DispatchIPPacket(ipv4ICMP)
	require.NoError(t, err)
	assert.True(t, icmpHandled)

	// Truncated packet error
	err = vt.DispatchIPPacket([]byte{0x45})
	assert.ErrorIs(t, err, masque.ErrInvalidIPHeader)

	// Unhandled protocol
	ipv4RAW := make([]byte, 20)
	ipv4RAW[0] = 0x45
	ipv4RAW[9] = 255

	err = vt.DispatchIPPacket(ipv4RAW)
	assert.ErrorIs(t, err, masque.ErrUnhandledProtocol)
}
